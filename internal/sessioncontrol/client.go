package sessioncontrol

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// Terminator closes one agent-local connection; it must not revoke other sessions.
type Terminator interface { DisconnectSession(string) bool }

type Command struct {
 CommandID string `json:"commandId"`
 AgentSessionID string `json:"agentSessionId"`
}
type commandResponse struct { Commands []Command `json:"commands"` }

// Client polls a trusted backend endpoint using a separately provisioned token.
// It is not enabled automatically: agent registration and credentials must first
// be bound to a verified backend identity.
type Client struct {
 BaseURL string
 AgentID string
 Token string
 HTTP *http.Client
 Terminator Terminator
}

func (c *Client) request(ctx context.Context,method,path string,body string)(*http.Response,error){
 if c.Token=="" || c.AgentID=="" || c.Terminator==nil {return nil,errors.New("session control is not configured")}
 u,err:=url.Parse(c.BaseURL)
 if err!=nil || u.Scheme!="https" || u.Host=="" || u.User!=nil || u.RawQuery!="" || u.Fragment!="" {return nil,errors.New("session control requires an HTTPS backend URL")}
 endpoint:=strings.TrimRight(u.String(),"/")+path
 req,err:=http.NewRequestWithContext(ctx,method,endpoint,strings.NewReader(body))
 if err!=nil{return nil,err}
 req.Header.Set("Authorization","Bearer "+c.Token)
 req.Header.Set("X-Network-Agent-ID",c.AgentID)
 if body!="" {req.Header.Set("Content-Type","application/json")}
 client:=c.HTTP
 if client==nil {client=&http.Client{Timeout:10*time.Second}}
 return client.Do(req)
}

// PollOnce processes only instructions returned by the authenticated backend.
// An absent local session is acknowledged as failed, never as a success.
func (c *Client) PollOnce(ctx context.Context)error{
 resp,err:=c.request(ctx,http.MethodGet,"/api/v1/internal/network/sessions/commands","")
 if err!=nil{return err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return fmt.Errorf("poll commands: HTTP %d",resp.StatusCode)}
 var payload commandResponse
 decoder:=json.NewDecoder(io.LimitReader(resp.Body,128<<10))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&payload);err!=nil{return err}
 if len(payload.Commands)>100{return errors.New("too many disconnect commands")}
 for _,command:=range payload.Commands{
  if command.CommandID=="" || command.AgentSessionID=="" {return errors.New("invalid disconnect instruction")}
  status:="failed"
  errorCode:="session_not_found"
  if c.Terminator.DisconnectSession(command.AgentSessionID) {status="succeeded";errorCode=""}
  body,_:=json.Marshal(struct{
   Status string `json:"status"`
   ErrorCode string `json:"errorCode,omitempty"`
  }{Status:status,ErrorCode:errorCode})
  endpoint:="/api/v1/internal/network/sessions/commands/"+url.PathEscape(command.CommandID)+"/ack"
  ack,err:=c.request(ctx,http.MethodPost,endpoint,string(body))
  if err!=nil{return err}
  ack.Body.Close()
  if ack.StatusCode!=http.StatusOK{return fmt.Errorf("acknowledge disconnect: HTTP %d",ack.StatusCode)}
 }
 return nil
}
