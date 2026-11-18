package usage

import (
	"context"
	"fmt"
)

// Sender transmits queued reports in sequence order. An unsuccessful
// submission leaves the report on disk for the next retry.
type Sender struct {
	Outbox *Outbox
	Client Client
}

func (s Sender) Flush(ctx context.Context) (int, error) {
	return s.FlushThrough(ctx, ^uint64(0))
}

// FlushThrough confirms backend ingestion of all pending reports up to the
// specified sequence. A transition must use the last sequence from its
// checkpoint, and must fence writes until this call and promotion complete.
// Successful Submit means backend accepted the report; it is not a substitute
// for verifying entitlement generation at the subscription boundary.
func (s Sender) FlushThrough(ctx context.Context, sequence uint64) (int, error) {
	if s.Outbox == nil {
		return 0, fmt.Errorf("usage outbox is required")
	}
	pending, err := s.Outbox.Pending()
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, report := range pending {
		if report.Sequence > sequence {
			break
		}
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		if err := s.Client.Submit(ctx, report); err != nil {
			return sent, err
		}
		if err := s.Outbox.Acknowledge(report.Sequence); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}
