package sessioncontrol

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

type fakeTerminator struct {seen []string;found bool}
func (f *fakeTerminator) DisconnectSession(id string)bool {f.seen=append(f.seen,id);return f.found}

func TestPollOnceAuthenticatesAndAcknowledges(t *testing.T){
 for _,found:=range []bool{true,false}{
  t.Run(map[bool]string{true:"found",false:"missing"}[found],func(t *testing.T){
   term:=&fakeTerminator{found:found}
   var acknowledged string
   server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    if r.Header.Get("Authorization")!="Bearer secret" || r.Header.Get("X-Network-Agent-ID")!="agent-01" {t.Error("missing agent authentication");w.WriteHeader(http.StatusUnauthorized);return}
    if r.Method==http.MethodGet {
     w.Header().Set("Content-Type","application/json")
     _,_=w.Write([]byte(`{"commands":[{"commandId":"11111111-1111-4111-8111-111111111111","agentSessionId":"session-1"}]}`))
     return
    }
    if r.URL.Path!="/api/v1/internal/network/sessions/commands/11111111-1111-4111-8111-111111111111/ack" {t.Errorf("wrong ack path %s",r.URL.Path)}
    var body struct{Status string `json:"status"`;ErrorCode string `json:"errorCode"`}
    if err:=json.NewDecoder(r.Body).Decode(&body);err!=nil{t.Error(err)}
    acknowledged=body.Status
    if found && body.ErrorCode!="" {t.Error("success must not include error code")}
    if !found && body.ErrorCode!="session_not_found" {t.Error("missing session must be reported")}
    _,_=w.Write([]byte(`{}`))
   }))
   defer server.Close()
   c:=&Client{BaseURL:server.URL,AgentID:"agent-01",Token:"secret",HTTP:server.Client(),Terminator:term}
   if err:=c.PollOnce(context.Background());err!=nil{t.Fatal(err)}
   expected:="failed";if found {expected="succeeded"}
   if acknowledged!=expected {t.Fatalf("acknowledged %q, expected %q",acknowledged,expected)}
   if len(term.seen)!=1 || term.seen[0]!="session-1" {t.Fatalf("wrong termination: %v",term.seen)}
  })
 }
}

func TestPollOnceRejectsNonHTTPS(t *testing.T){
 c:=&Client{BaseURL:"http://example.com",AgentID:"agent",Token:"token",Terminator:&fakeTerminator{}}
 if err:=c.PollOnce(context.Background());err==nil || !strings.Contains(err.Error(),"HTTPS"){t.Fatalf("expected HTTPS rejection, got %v",err)}
}
