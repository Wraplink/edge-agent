package sniproxy

import (
 "net"
 "testing"
 "time"
)

func TestDisconnectSessionIsTargeted(t *testing.T) {
 first,firstPeer:=net.Pipe()
 defer first.Close()
 defer firstPeer.Close()
 second,secondPeer:=net.Pipe()
 defer second.Close()
 defer secondPeer.Close()

 p:=&Proxy{sessionIDs:map[string]net.Conn{"first":first,"second":second}}
 if p.DisconnectSession("unknown") {t.Fatal("unknown session was reported disconnected")}
 if !p.DisconnectSession("first") {t.Fatal("known session not found")}
 if _,err:=firstPeer.Write([]byte("x"));err==nil {t.Fatal("disconnected peer still writable")}
 // A targeted disconnect must not terminate unrelated sessions.
 if err:=second.SetWriteDeadline(time.Now().Add(time.Second));err!=nil {t.Fatal(err)}
 received:=make(chan error,1)
 go func(){buf:=make([]byte,1);_,err:=secondPeer.Read(buf);received<-err}()
 if _,err:=second.Write([]byte("x"));err!=nil {t.Fatalf("unrelated session closed: %v",err)}
 if err:=<-received;err!=nil {t.Fatalf("unrelated session read failed: %v",err)}
}
