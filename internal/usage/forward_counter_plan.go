package usage

import (
 "fmt"
 "net"
 "sort"
 "strings"

 "github.com/wraplink/edge-agent/internal/client"
)

// ForwardCounterSpec describes an accounting rule in the FORWARD hook.
// The post-DNAT destination is the origin, not the route's public IP.
// Rules must be reconciled as a complete non-overlapping set before install.
type ForwardCounterSpec struct {
 ClientIP string
 OriginIP string
 Protocol string
 Direction string
 Tag string
}

// PlanForwardCounters generates distinct upload and download match rules.
// It refuses ambiguous overlapping origin/protocol pairs because these would
// count one packet more than once. Caller must install only when access
// enforcement is active and must constrain matches to the route dataplane.
func PlanForwardCounters(leases []client.Lease, clientIPs []string) ([]ForwardCounterSpec,error) {
 clients:=make(map[string]bool)
 for _,ip:=range clientIPs {
  parsed:=net.ParseIP(ip)
  if parsed==nil||parsed.To4()==nil {return nil,fmt.Errorf("invalid client IPv4 %q",ip)}
  clients[parsed.To4().String()]=true
 }
 unique:=make(map[string]bool)
 var result []ForwardCounterSpec
 for _,lease:=range leases {
  if lease.Type!="route" {continue}
  origin:=net.ParseIP(lease.DestinationIP)
  if origin==nil||origin.To4()==nil {return nil,fmt.Errorf("invalid route origin %q",lease.DestinationIP)}
  protocols:=[]string{lease.Protocol}
  if lease.Protocol=="tcp_udp" {protocols=[]string{"tcp","udp"}}
  for _,proto:=range protocols {
   if proto!="tcp"&&proto!="udp" {return nil,fmt.Errorf("invalid route protocol %q",proto)}
   for ip:=range clients {
    key:=strings.Join([]string{ip,origin.To4().String(),proto},":")
    if unique[key] {continue} // Multiple leases share the same routed flow; count once.
    unique[key]=true
    for _,direction:=range []string{"upload","download"} {
     result=append(result,ForwardCounterSpec{
      ClientIP:ip,OriginIP:origin.To4().String(),Protocol:proto,
      Direction:direction,
      Tag:"wraplink:route-usage:"+ip+":"+direction+":"+strings.ReplaceAll(key,":","-"),
     })
    }
   }
  }
 }
 sort.Slice(result,func(i,j int)bool{return result[i].Tag<result[j].Tag})
 return result,nil
}
