package usage

import (
 "fmt"
 "time"
)

// SampleAndQueue samples cumulative route counters and durably writes every
// nonzero interval to the existing outbox before committing the new in-memory
// baseline. On enqueue failure the caller MUST stop collection: retrying the
// same interval after a partial enqueue can duplicate usage. A production
// collector needs a transactional checkpoint/outbox batch for crash recovery.
func (s *RouteSampler) SampleAndQueue(now time.Time, counters []RouteCounter, outbox *Outbox, agentID string) ([]RouteDelta,error) {
 if outbox==nil {return nil,fmt.Errorf("route usage outbox is required")}
 if agentID=="" {return nil,fmt.Errorf("agent ID is required")}
 candidate:=&RouteSampler{last:s.last,previous:make(map[string]RouteCounter,len(s.previous))}
 for k,v:=range s.previous {candidate.previous[k]=v}
 deltas,err:=candidate.Sample(now,counters)
 if err!=nil {return nil,err}
 if err:=QueueRouteDeltas(outbox,agentID,deltas);err!=nil {return nil,err}
 s.previous=candidate.previous
 s.last=candidate.last
 return deltas,nil
}
