package revisionfence

import (
 "errors"
 "sync"
 "fmt"
)

var (
 ErrInvalidOperation=errors.New("invalid public IP operation")
 ErrStaleRevision=errors.New("stale public IP revision")
 ErrConflictingRevision=errors.New("conflicting public IP revision")
)

// Operation identifies one requested public-IP enforcement operation.
// A node must persist its accepted revision and attempt before enforcing it.
type Operation struct {
 SubscriptionID string
 PublicIPID string
 Revision int64
 AttemptID string
}

// Fence validates revision ordering. NewPersistentFence persists accepted
// revisions before returning success. This does not itself enforce dataplane
// operations or prevent a concurrent stale operation already in flight.
type Fence struct {
 mu sync.Mutex
 accepted map[string]Operation
 store *FileStore
}

// NewPersistentFence restores accepted revisions and fails closed on invalid
// persisted data. The caller must ensure exclusive ownership of the file.
func NewPersistentFence(store FileStore)(*Fence,error){
 operations,err:=store.Load()
 if err!=nil{return nil,fmt.Errorf("load revision fence: %w",err)}
 return &Fence{accepted:operations,store:&store},nil
}

func (f *Fence) Accept(op Operation) error {
 if op.SubscriptionID==""||op.PublicIPID==""||op.AttemptID==""||op.Revision<1{return ErrInvalidOperation}
 f.mu.Lock()
 defer f.mu.Unlock()
 if f.accepted==nil{f.accepted=make(map[string]Operation)}
 previous,exists:=f.accepted[op.SubscriptionID]
 if exists {
  if op.Revision<previous.Revision{return ErrStaleRevision}
  if op.Revision==previous.Revision {
   if op.PublicIPID!=previous.PublicIPID{return ErrConflictingRevision}
   if op.AttemptID!=previous.AttemptID{return ErrConflictingRevision}
   return nil
  }
 }
 if f.store!=nil {
  next:=make(map[string]Operation,len(f.accepted)+1)
  for id,previous:=range f.accepted {next[id]=previous}
  next[op.SubscriptionID]=op
  if err:=f.store.Save(next);err!=nil{return fmt.Errorf("persist revision fence: %w",err)}
 }
 f.accepted[op.SubscriptionID]=op
 return nil
}

func (f *Fence) Current(subscriptionID string)(Operation,bool){
 f.mu.Lock()
 defer f.mu.Unlock()
 op,ok:=f.accepted[subscriptionID]
 return op,ok
}
