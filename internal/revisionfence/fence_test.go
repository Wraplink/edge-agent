package revisionfence

import (
 "errors"
 "testing"
)

func TestFenceOrdering(t *testing.T){
 f:=&Fence{}
 base:=Operation{SubscriptionID:"subscription",PublicIPID:"ip-a",Revision:4,AttemptID:"attempt-a"}
 if err:=f.Accept(base);err!=nil{t.Fatal(err)}
 if err:=f.Accept(base);err!=nil{t.Fatalf("idempotent replay: %v",err)}
 stale:=base;stale.Revision=3
 if err:=f.Accept(stale);!errors.Is(err,ErrStaleRevision){t.Fatalf("stale: %v",err)}
 conflict:=base;conflict.PublicIPID="ip-b"
 if err:=f.Accept(conflict);!errors.Is(err,ErrConflictingRevision){t.Fatalf("conflict: %v",err)}
 conflict=base;conflict.AttemptID="attempt-b"
 if err:=f.Accept(conflict);!errors.Is(err,ErrConflictingRevision){t.Fatalf("attempt conflict: %v",err)}
 next:=Operation{SubscriptionID:"subscription",PublicIPID:"ip-b",Revision:5,AttemptID:"attempt-b"}
 if err:=f.Accept(next);err!=nil{t.Fatal(err)}
 if err:=f.Accept(base);!errors.Is(err,ErrStaleRevision){t.Fatalf("replay: %v",err)}
 got,ok:=f.Current(base.SubscriptionID)
 if !ok||got!=next{t.Fatalf("current=%+v exists=%v",got,ok)}
}

func TestFenceInvalidOperation(t *testing.T){
 f:=&Fence{}
 for _,op:=range []Operation{
  {},{SubscriptionID:"s",PublicIPID:"p",Revision:1},
  {SubscriptionID:"s",AttemptID:"a",Revision:1},
  {PublicIPID:"p",AttemptID:"a",Revision:1},
  {SubscriptionID:"s",PublicIPID:"p",AttemptID:"a"},
 }{
  if err:=f.Accept(op);!errors.Is(err,ErrInvalidOperation){t.Fatalf("operation=%+v err=%v",op,err)}
 }
}
