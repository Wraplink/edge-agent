package revisionfence

import (
 "errors"
 "sync"
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

// Fence is an in-memory revision guard for validating the ordering contract.
// It is NOT durable: production callers must restore/persist state atomically
// and prevent stale operations from executing after a newer one is accepted.
type Fence struct {
 mu sync.Mutex
 accepted map[string]Operation
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
 f.accepted[op.SubscriptionID]=op
 return nil
}

func (f *Fence) Current(subscriptionID string)(Operation,bool){
 f.mu.Lock()
 defer f.mu.Unlock()
 op,ok:=f.accepted[subscriptionID]
 return op,ok
}
