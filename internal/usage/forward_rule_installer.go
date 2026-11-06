package usage

import (
 "bytes"
 "github.com/wraplink/edge-agent/internal/client"
 "context"
 "fmt"
 "net"
 "os/exec"
 "sort"
 "strings"
)

// ForwardRuleInstaller owns only the dedicated route_accounting chain.
// It requires the WrapLink IPv4 table to exist and must be invoked only
// after entitlement and route leases have been reconciled. This is not yet
// wired into the agent lifecycle.
type ForwardRuleInstaller struct {
 Command string
 Table string
 Chain string
}

// RenderForwardRules produces an nft -f batch. Conntrack's original
// destination matches the allocated route IP even after DNAT, so unrelated
// direct traffic to the same origin is excluded. Download rules also match
// the original source client to avoid attributing other forwarded flows.
func RenderForwardRules(leases []client.Lease, clientIPs []string, table, chain string) (string,error) {
 if table=="" {table="wraplink"}
 if chain=="" {chain="route_accounting"}
 if !safeNFTIdentifier(table)||!safeNFTIdentifier(chain) {return "",fmt.Errorf("invalid nftables identifier")}
 clients:=map[string]bool{}
 for _,s:=range clientIPs {
  ip:=net.ParseIP(s)
  if ip==nil||ip.To4()==nil {return "",fmt.Errorf("invalid client IPv4 %q",s)}
  clients[ip.To4().String()]=true
 }
 type route struct { public,origin,proto string }
 unique:=map[route]bool{}
 for _,lease:=range leases {
  if lease.Type!="route" {continue}
  public:=net.ParseIP(lease.Address)
  origin:=net.ParseIP(lease.DestinationIP)
  if public==nil||public.To4()==nil||origin==nil||origin.To4()==nil {return "",fmt.Errorf("invalid route address in lease %q",lease.ID)}
  protocols:=[]string{lease.Protocol}
  if lease.Protocol=="tcp_udp" {protocols=[]string{"tcp","udp"}}
  for _,proto:=range protocols {
   if proto!="tcp"&&proto!="udp" {return "",fmt.Errorf("invalid route protocol %q",proto)}
   unique[route{public.To4().String(),origin.To4().String(),proto}]=true
  }
 }
 var routes []route
 for r:=range unique {routes=append(routes,r)}
 sort.Slice(routes,func(i,j int)bool {
  a,b:=routes[i],routes[j]
  return a.public+a.origin+a.proto < b.public+b.origin+b.proto
 })
 var addresses []string
 for ip:=range clients {addresses=append(addresses,ip)}
 sort.Strings(addresses)
 var script strings.Builder
 // The chain is a filter base chain on FORWARD. Flush/rebuild in one
 // nft transaction to avoid partially applied accounting rules.
 fmt.Fprintf(&script,"flush chain ip %s %s\n",table,chain)
 for _,r:=range routes {
  for _,ip:=range addresses {
   id:=strings.ReplaceAll(r.public+"-"+r.origin+"-"+r.proto,".","_")
   for _,direction:=range []string{"upload","download"} {
    tag:="wraplink:route-usage:"+ip+":"+direction+":"+id
    if direction=="upload" {
     fmt.Fprintf(&script,"add rule ip %s %s ip saddr %s ip daddr %s ct direction original ct original daddr %s meta l4proto %s counter comment %q\n",table,chain,ip,r.origin,r.public,r.proto,tag)
    } else {
     fmt.Fprintf(&script,"add rule ip %s %s ip saddr %s ip daddr %s ct direction reply ct original saddr %s ct original daddr %s meta l4proto %s counter comment %q\n",table,chain,r.origin,ip,ip,r.public,r.proto,tag)
    }
   }
  }
 }
 return script.String(),nil
}

func safeNFTIdentifier(s string) bool {
 if s=="" {return false}
 for _,c:=range s {
  if c!='_' && c!='-' && (c<'a'||c>'z') && (c<'A'||c>'Z') && (c<'0'||c>'9') {return false}
 }
 return true
}

func (i ForwardRuleInstaller) Apply(ctx context.Context, leases []client.Lease, clientIPs []string) error {
 table,chain:=i.Table,i.Chain
 if table=="" {table="wraplink"}
 if chain=="" {chain="route_accounting"}
 script,err:=RenderForwardRules(leases,clientIPs,table,chain)
 if err!=nil {return err}
 command:=i.Command
 if command=="" {command="nft"}
 cmd:=exec.CommandContext(ctx,command,"-f","-")
 cmd.Stdin=strings.NewReader(script)
 output,err:=cmd.CombinedOutput()
 if err!=nil {return fmt.Errorf("install route counters: %w: %s",err,bytes.TrimSpace(output))}
 return nil
}
