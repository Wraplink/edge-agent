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
	if s.Outbox == nil {
		return 0, fmt.Errorf("usage outbox is required")
	}
	pending, err := s.Outbox.Pending()
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, report := range pending {
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
