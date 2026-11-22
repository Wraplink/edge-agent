package revisionfence

import (
 "context"
 "errors"
 "fmt"
)

// ErrUnverifiedApplication indicates the dataplane could not be independently
// inspected after the requested change.
var ErrUnverifiedApplication=errors.New("dataplane application was not verified")

// DataplaneOperation executes the requested change and verifies the resulting
// dataplane state. Verify MUST inspect the real installed rules, not merely
// the requested or cached desired state.
type DataplaneOperation interface {
 Apply(context.Context,Operation) error
 Verify(context.Context,Operation) (bool,error)
}

// ApplyVerified serializes revision acceptance, dataplane application and
// observation. A successful return means the verifier observed the exact
// requested state. The caller may then mark process-local applied state.
// A failed apply/verify leaves the accepted revision fenced for reconciliation.
func (f *Fence) ApplyVerified(ctx context.Context,op Operation,dataplane DataplaneOperation,tracker *AppliedTracker)error{
 if dataplane==nil||tracker==nil{return ErrInvalidOperation}
 err:=f.Execute(ctx,op,func(ctx context.Context)error{
  if err:=dataplane.Apply(ctx,op);err!=nil{return fmt.Errorf("apply public IP revision: %w",err)}
  observed,err:=dataplane.Verify(ctx,op)
  if err!=nil{return fmt.Errorf("verify public IP revision: %w",err)}
  if !observed{return ErrUnverifiedApplication}
  return nil
 })
 if err!=nil{return err}
 return tracker.MarkApplied(ctx,f,op)
}
