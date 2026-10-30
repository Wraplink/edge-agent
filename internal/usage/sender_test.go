package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSenderRetainsFailedReportAndRetries(t *testing.T) {
	outbox, err := OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC()
	report := Report{AgentID: "edge-1", Sequence: 9, ClientIP: "198.51.100.10", UploadedBytes: 123, StartedAt: start, EndedAt: start.Add(time.Minute)}
	if err := outbox.Enqueue(report); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Header.Get("Idempotency-Key") != "edge-1:9" {
			t.Errorf("unexpected idempotency key: %s", r.Header.Get("Idempotency-Key"))
		}
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sender := Sender{Outbox: outbox, Client: Client{URL: server.URL, Key: "secret"}}
	if sent, err := sender.Flush(context.Background()); err == nil || sent != 0 {
		t.Fatalf("expected failed first attempt: sent=%d err=%v", sent, err)
	}
	pending, err := outbox.Pending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("failed report lost: %+v %v", pending, err)
	}
	if sent, err := sender.Flush(context.Background()); err != nil || sent != 1 {
		t.Fatalf("retry failed: sent=%d err=%v", sent, err)
	}
	pending, err = outbox.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("acknowledged report remains: %+v %v", pending, err)
	}
}
