package revisionfence

import (
 "context"
 "errors"
 "fmt"
 "sync"
)

// ErrNotApplied means the operation has been accepted but has not been
// independently confirmed as installed in the dataplane.
var ErrNotApplied=errors.New("public IP operation is not confirmed applied")

// AppliedTracker separates accepted revisions from successfully enforced ones.
// It deliberately does not infer success from Fence.Accept or Fence.Execute.
// This tracker is process-local and is NOT a durable network acknowledgement.
type AppliedTracker struct {
 mu sync.RWMutex
 applied map[string]Operation
}

func (t *AppliedTracker) MarkApplied(ctx context.Context, f *Fence, op Operation) error {
 if f==nil{return ErrInvalidOperation}
 if err:=ctx.Err();err!=nil{return err}
 f.mu.Lock()
 defer f.mu.Unlock()
 if err:=ctx.Err();err!=nil{return err}
 accepted,ok:=f.accepted[op.SubscriptionID]
 if !ok||accepted!=op{return fmt.Errorf("accepted revision changed: %w",ErrNotApplied)}
 t.mu.Lock()
 defer t.mu.Unlock()
 if t.applied==nil{t.applied=make(map[string]Operation)}
 t.applied[op.SubscriptionID]=op
 return nil
}

func (t *AppliedTracker) IsApplied(op Operation) bool {
 t.mu.RLock()
 defer t.mu.RUnlock()
 applied,ok:=t.applied[op.SubscriptionID]
 return ok&&applied==op
}

// Reset discards all process-local confirmations after dataplane reset.
func (t *AppliedTracker) Reset(){
 t.mu.Lock()
 defer t.mu.Unlock()
 t.applied=nil
}

// Confirmed returns the exact process-local confirmation for a subscription.
// The caller must additionally verify authenticated node identity and durable
// dataplane state before treating this as a network acknowledgement.
func (t *AppliedTracker) Confirmed(subscriptionID string)(Operation,bool){
 t.mu.RLock()
 defer t.mu.RUnlock()
 op,ok:=t.applied[subscriptionID]
 return op,ok
}

// IsCurrentApplied checks both the local observation and the current accepted
// fence revision. Never use IsApplied alone to authorize a network ack.
func (t *AppliedTracker) IsCurrentApplied(f *Fence,op Operation) bool {
 if f==nil{return false}
 f.mu.Lock()
 defer f.mu.Unlock()
 current,ok:=f.accepted[op.SubscriptionID]
 if !ok||current!=op{return false}
 return t.IsApplied(op)
}
