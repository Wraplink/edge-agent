package origindns

import (
 "bytes"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "golang.org/x/net/dns/dnsmessage"
)

func TestOriginDNSRejectsUnauthenticatedRequests(t *testing.T) {
 handler:=Handler{Token:strings.Repeat("a",32),Upstreams:[]string{"1.1.1.1:53"}}
 req:=httptest.NewRequest(http.MethodPost,"/api/v1/dns/resolve",bytes.NewReader([]byte("invalid")))
 req.Header.Set("Content-Type","application/dns-message")
 recorder:=httptest.NewRecorder()
 handler.ServeHTTP(recorder,req)
 if recorder.Code!=http.StatusUnauthorized{t.Fatalf("status=%d",recorder.Code)}
}

func TestOriginDNSRejectsInvalidWireMessage(t *testing.T) {
 handler:=Handler{Token:strings.Repeat("a",32),Upstreams:[]string{"1.1.1.1:53"}}
 req:=httptest.NewRequest(http.MethodPost,"/api/v1/dns/resolve",bytes.NewReader([]byte("invalid")))
 req.Header.Set("Content-Type","application/dns-message")
 req.Header.Set("Authorization","Bearer "+handler.Token)
 recorder:=httptest.NewRecorder()
 handler.ServeHTTP(recorder,req)
 if recorder.Code!=http.StatusBadRequest{t.Fatalf("status=%d",recorder.Code)}
}

func TestOriginDNSQuestionValidation(t *testing.T) {
 builder:=dnsmessage.NewBuilder(nil,dnsmessage.Header{ID:123,RecursionDesired:true})
 if err:=builder.StartQuestions();err!=nil{t.Fatal(err)}
 name,err:=dnsmessage.NewName("example.com.")
 if err!=nil{t.Fatal(err)}
 question:=dnsmessage.Question{Name:name,Type:dnsmessage.TypeA,Class:dnsmessage.ClassINET}
 if err:=builder.Question(question);err!=nil{t.Fatal(err)}
 wire,err:=builder.Finish()
 if err!=nil{t.Fatal(err)}
 parsed,id,err:=parseQuestion(wire,false)
 if err!=nil || id!=123 || parsed!=question{t.Fatalf("parsed=%v id=%d err=%v",parsed,id,err)}
 if _,_,err:=parseQuestion(wire,true);err==nil{t.Fatal("query accepted as response")}
}

func TestValidateUpstreams(t *testing.T) {
 for _,upstream:=range []string{"127.0.0.1:53","0.0.0.0:53","bad","1.1.1.1:5353"}{
  if err:=ValidateUpstreams([]string{upstream});err==nil{t.Errorf("accepted %s",upstream)}
 }
 if err:=ValidateUpstreams([]string{"1.1.1.1:53"});err!=nil{t.Fatal(err)}
}
