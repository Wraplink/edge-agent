package revisionfence

import (
 "errors"
 "os"
 "path/filepath"
 "testing"
)

func TestPersistentFenceSurvivesRestart(t *testing.T){
 store:=FileStore{Path:filepath.Join(t.TempDir(),"fence.json")}
 if err:=store.Bootstrap();err!=nil{t.Fatal(err)}
 first,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 old:=Operation{SubscriptionID:"sub",PublicIPID:"ip-a",Revision:5,AttemptID:"attempt-a"}
 if err:=first.Accept(old);err!=nil{t.Fatal(err)}
 restarted,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 if current,ok:=restarted.Current("sub");!ok||current!=old{t.Fatalf("restored=%+v ok=%v",current,ok)}
 stale:=old;stale.Revision=4
 if err:=restarted.Accept(stale);!errors.Is(err,ErrStaleRevision){t.Fatalf("stale accepted after restart: %v",err)}
 next:=Operation{SubscriptionID:"sub",PublicIPID:"ip-b",Revision:6,AttemptID:"attempt-b"}
 if err:=restarted.Accept(next);err!=nil{t.Fatal(err)}
 third,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 if current,ok:=third.Current("sub");!ok||current!=next{t.Fatalf("restored=%+v ok=%v",current,ok)}
}

func TestPersistentFenceRejectsWriteFailureWithoutAccepting(t *testing.T){
 dir:=t.TempDir()
 store:=FileStore{Path:filepath.Join(dir,"fence.json")}
 if err:=store.Bootstrap();err!=nil{t.Fatal(err)}
 fence,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 op:=Operation{SubscriptionID:"sub",PublicIPID:"ip",Revision:1,AttemptID:"attempt"}
 if err:=os.RemoveAll(dir);err!=nil{t.Fatal(err)}
 if err:=fence.Accept(op);err==nil{t.Fatal("accepted without durable persistence")}
 if _,ok:=fence.Current("sub");ok{t.Fatal("in-memory revision advanced despite storage failure")}
}

func TestPersistentFenceRejectsCorruptStartupState(t *testing.T){
 path:=filepath.Join(t.TempDir(),"fence.json")
 if err:=os.WriteFile(path,[]byte("not json"),0600);err!=nil{t.Fatal(err)}
 if _,err:=NewPersistentFence(FileStore{Path:path});err==nil{t.Fatal("corrupt state accepted")}
}

func TestPersistentFenceRejectsLostState(t *testing.T){
 store:=FileStore{Path:filepath.Join(t.TempDir(),"fence.json")}
 if _,err:=NewPersistentFence(store);!errors.Is(err,ErrFenceStateMissing){t.Fatalf("missing initial state: %v",err)}
 if err:=store.Bootstrap();err!=nil{t.Fatal(err)}
 fence,err:=NewPersistentFence(store)
 if err!=nil{t.Fatal(err)}
 if err:=fence.Accept(Operation{SubscriptionID:"sub",PublicIPID:"ip",Revision:8,AttemptID:"attempt"});err!=nil{t.Fatal(err)}
 if err:=os.Remove(store.Path);err!=nil{t.Fatal(err)}
 if _,err:=NewPersistentFence(store);!errors.Is(err,ErrFenceStateMissing){t.Fatalf("lost state silently reset: %v",err)}
}
