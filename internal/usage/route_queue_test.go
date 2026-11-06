package usage

import (
 "testing"
 "time"
)

func TestQueueRouteDeltas(t *testing.T) {
 outbox,err:=OpenOutbox(t.TempDir())
 if err!=nil {t.Fatal(err)}
 start:=time.Date(2026,11,6,1,0,0,0,time.UTC)
 deltas:=[]RouteDelta{
  {ClientIP:"192.0.2.10",UploadedBytes:1024,DownloadedBytes:2048,StartedAt:start,EndedAt:start.Add(time.Minute)},
  {ClientIP:"192.0.2.11",UploadedBytes:0,DownloadedBytes:0,StartedAt:start,EndedAt:start.Add(time.Minute)},
 }
 if err:=QueueRouteDeltas(outbox,"edge-01",deltas);err!=nil {t.Fatal(err)}
 pending,err:=outbox.Pending()
 if err!=nil {t.Fatal(err)}
 if len(pending)!=1||pending[0].AgentID!="edge-01"||pending[0].ClientIP!="192.0.2.10"||pending[0].UploadedBytes!=1024||pending[0].DownloadedBytes!=2048 {
  t.Fatalf("unexpected queued reports: %+v",pending)
 }
}

func TestQueueRouteDeltasRejectsInvalid(t *testing.T) {
 outbox,err:=OpenOutbox(t.TempDir())
 if err!=nil {t.Fatal(err)}
 now:=time.Now()
 if err:=QueueRouteDeltas(outbox,"edge-01",[]RouteDelta{{ClientIP:"bad",UploadedBytes:1,StartedAt:now,EndedAt:now.Add(time.Second)}});err==nil {t.Fatal("invalid client IP accepted")}
 pending,err:=outbox.Pending()
 if err!=nil||len(pending)!=0 {t.Fatalf("invalid report queued: %+v %v",pending,err)}
}
