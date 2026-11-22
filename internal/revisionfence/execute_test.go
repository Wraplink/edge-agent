package revisionfence

import (
 "context"
 "errors"
 "path/filepath"
 "sync"
 "testing"
)

func TestExecuteRejectsStaleDataplaneWork(t *testing.T) {
 store:=FileStore{Path:filepath.Join(t.TempDir(),"fence.json")}
 if err:=store.Bootstrap();err!=nil{t.Fatal(err)}
 f,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 newer:=Operation{SubscriptionID:"sub",PublicIPID:"ip-new",Revision:2,AttemptID:"attempt-new"}
 older:=Operation{SubscriptionID:"sub",PublicIPID:"ip-old",Revision:1,AttemptID:"attempt-old"}
 started:=make(chan struct{})
 release:=make(chan struct{})
 var wg sync.WaitGroup
 wg.Add(1)
 go func(){
  defer wg.Done()
  if err:=f.Execute(context.Background(),newer,func(context.Context)error{close(started);<-release;return nil});err!=nil{t.Errorf("new operation: %v",err)}
 }()
 <-started
 called:=false
 staleResult:=make(chan error,1)
 go func(){staleResult<-f.Execute(context.Background(),older,func(context.Context)error{called=true;return nil})}()
 close(release)
 wg.Wait()
 if err:=<-staleResult;!errors.Is(err,ErrStaleRevision){t.Fatalf("stale result: %v",err)}
 if called{t.Fatal("stale dataplane callback executed")}
}

func TestExecutePersistsBeforeApplying(t *testing.T){
 store:=FileStore{Path:filepath.Join(t.TempDir(),"fence.json")}
 if err:=store.Bootstrap();err!=nil{t.Fatal(err)}
 f,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 op:=Operation{SubscriptionID:"sub",PublicIPID:"ip",Revision:1,AttemptID:"attempt"}
 err=f.Execute(context.Background(),op,func(context.Context)error{
  loaded,err:=store.Load()
  if err!=nil{return err}
  if loaded["sub"]!=op{t.Fatal("dataplane ran before revision was persisted")}
  return nil
 })
 if err!=nil{t.Fatal(err)}
}

func TestExecuteErrorDoesNotEraseFence(t *testing.T){
 f:=&Fence{}
 op:=Operation{SubscriptionID:"sub",PublicIPID:"ip",Revision:3,AttemptID:"attempt"}
 failure:=errors.New("dataplane failed")
 if err:=f.Execute(context.Background(),op,func(context.Context)error{return failure});!errors.Is(err,failure){t.Fatalf("apply failure: %v",err)}
 stale:=op;stale.Revision=2
 if err:=f.Execute(context.Background(),stale,func(context.Context)error{t.Fatal("stale apply");return nil});!errors.Is(err,ErrStaleRevision){t.Fatalf("stale accepted: %v",err)}
}
