package usage

import (
 "testing"
 "time"
)

func TestRouteSamplerCountsBothDirectionsAndClients(t *testing.T) {
 s:=NewRouteSampler()
 start:=time.Date(2026,11,6,0,0,0,0,time.UTC)
 counters:=[]RouteCounter{
  {Key:"a-up",ClientIP:"192.0.2.1",Direction:"upload",Bytes:100},
  {Key:"a-down",ClientIP:"192.0.2.1",Direction:"download",Bytes:200},
  {Key:"b-up",ClientIP:"192.0.2.2",Direction:"upload",Bytes:50},
 }
 if result,err:=s.Sample(start,counters);err!=nil||len(result)!=0 {t.Fatalf("baseline: %v %v",result,err)}
 counters[0].Bytes=175; counters[1].Bytes=500; counters[2].Bytes=75
 result,err:=s.Sample(start.Add(time.Minute),counters)
 if err!=nil||len(result)!=2 {t.Fatalf("sample: %v %v",result,err)}
 if result[0].UploadedBytes!=75||result[0].DownloadedBytes!=300||result[1].UploadedBytes!=25 {t.Fatalf("wrong deltas: %+v",result)}
 result,err=s.Sample(start.Add(2*time.Minute),counters)
 if err!=nil||len(result)!=0 {t.Fatalf("repeated sample: %v %v",result,err)}
}

func TestRouteSamplerRebaselinesResets(t *testing.T) {
 s:=NewRouteSampler()
 now:=time.Now()
 a:=[]RouteCounter{{Key:"counter",ClientIP:"192.0.2.1",Direction:"upload",Bytes:100}}
 _,_=s.Sample(now,a)
 a[0].Bytes=5
 result,err:=s.Sample(now.Add(time.Second),a)
 if err!=nil||len(result)!=0 {t.Fatalf("reset: %v %v",result,err)}
 a[0].Bytes=12
 result,err=s.Sample(now.Add(2*time.Second),a)
 if err!=nil||len(result)!=1||result[0].UploadedBytes!=7 {t.Fatalf("after reset: %v %v",result,err)}
}
