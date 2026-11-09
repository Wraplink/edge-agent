package inventory

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "strings"
 "time"
)

// AgentHealthClient sends independent Backend heartbeats while the legacy
// DNS Control lease protocol remains active.
type AgentHealthClient struct {
 URL string
 Key string
 HTTPClient *http.Client
}

func (c AgentHealthClient) post(ctx context.Context, path string, payload any) error {
 data,err:=json.Marshal(payload)
 if err!=nil{return err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,strings.TrimRight(c.URL,"/")+path,bytes.NewReader(data))
 if err!=nil{return err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("X-Network-Control-Key",c.Key)
 httpClient:=c.HTTPClient
 if httpClient==nil {httpClient=&http.Client{Timeout:10*time.Second}}
 resp,err:=httpClient.Do(req)
 if err!=nil{return err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK {
  body,_:=io.ReadAll(io.LimitReader(resp.Body,1024))
  return fmt.Errorf("backend agent health %s: HTTP %d: %s",path,resp.StatusCode,string(body))
 }
 return nil
}

func (c AgentHealthClient) Register(ctx context.Context,id,region string,sni,route int) error {
 return c.post(ctx,"/api/v1/internal/network/agents/register",map[string]any{
  "agent_id":id,"region":region,"capacity":map[string]int{"sni":sni,"route":route},
 })
}

func (c AgentHealthClient) Heartbeat(ctx context.Context,id string) error {
 return c.post(ctx,"/api/v1/internal/network/agents/heartbeat",map[string]string{"agent_id":id})
}
