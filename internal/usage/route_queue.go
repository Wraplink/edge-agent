package usage

import (
    "fmt"
    "net"
)

// QueueRouteDeltas converts measured per-client route intervals into the
// existing backend usage report format and durably enqueues them.
// It deliberately does not advance any counter checkpoint. Production
// integration must atomically coordinate report enqueue with counter state.
func QueueRouteDeltas(outbox *Outbox, agentID string, deltas []RouteDelta) error {
    if outbox == nil { return fmt.Errorf("route usage outbox is required") }
    if agentID == "" { return fmt.Errorf("agent ID is required") }
    for _, delta := range deltas {
        ip := net.ParseIP(delta.ClientIP)
        if ip == nil || ip.To4() == nil || !delta.EndedAt.After(delta.StartedAt) {
            return fmt.Errorf("invalid route delta interval or client IP")
        }
        if delta.UploadedBytes == 0 && delta.DownloadedBytes == 0 { continue }
        sequence, err := outbox.ReserveSequence()
        if err != nil { return fmt.Errorf("reserve route usage sequence: %w", err) }
        report := Report{
            AgentID: agentID, Sequence: sequence, ClientIP: delta.ClientIP,
            UploadedBytes: delta.UploadedBytes, DownloadedBytes: delta.DownloadedBytes,
            StartedAt: delta.StartedAt, EndedAt: delta.EndedAt,
        }
        if err := outbox.Enqueue(report); err != nil {
            return fmt.Errorf("persist route usage report %d: %w", sequence, err)
        }
    }
    return nil
}
