package revisionfence

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnverifiedApplication indicates the dataplane could not be independently
// inspected after the requested change.
var ErrUnverifiedApplication = errors.New("dataplane application was not verified")

// DataplaneOperation executes the requested change and verifies the resulting
// dataplane state. Verify MUST inspect the real installed rules, not merely
// the requested or cached desired state.
type DataplaneOperation interface {
	Apply(context.Context, Operation) error
	Verify(context.Context, Operation) (bool, error)
}

// ApplyVerified serializes revision acceptance, dataplane application and
// observation. A successful return means the verifier observed the exact
// requested state. The caller may then mark process-local applied state.
// A failed apply/verify leaves the accepted revision fenced for reconciliation.
func (f *Fence) ApplyVerified(ctx context.Context, op Operation, dataplane DataplaneOperation, tracker *AppliedTracker) error {
	if dataplane == nil || tracker == nil {
		return ErrInvalidOperation
	}
	err := f.Execute(ctx, op, func(ctx context.Context) error {
		// An identical accepted attempt may be replayed after a lost response.
		// Inspect effective state first to avoid repeating non-idempotent writes.
		alreadyApplied, preflightErr := dataplane.Verify(ctx, op)
		if preflightErr != nil {
			return fmt.Errorf("preflight verify public IP revision: %w", preflightErr)
		}
		if alreadyApplied {
			return nil
		}
		if err := dataplane.Apply(ctx, op); err != nil {
			return fmt.Errorf("apply public IP revision: %w", err)
		}
		observed, err := dataplane.Verify(ctx, op)
		if err != nil {
			return fmt.Errorf("verify public IP revision: %w", err)
		}
		if !observed {
			return ErrUnverifiedApplication
		}
		return nil
	})
	if err != nil {
		return err
	}
	// The caller context may be canceled immediately after the dataplane was
	// verified. Never lose an observed success solely due to that cancellation;
	// still require the exact accepted revision to be current.
	recordCtx := context.WithoutCancel(ctx)
	return tracker.MarkApplied(recordCtx, f, op)
}
