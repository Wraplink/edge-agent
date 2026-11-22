package revisionfence

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
)

// FileStore persists the latest accepted revision map across process restarts.
// It is intended for a single agent process on a local filesystem. It is not
// a distributed lock and does not itself prove dataplane application.
type FileStore struct { Path string }

var ErrFenceStateMissing = errors.New("revision fence state missing; explicit bootstrap required")

// Bootstrap creates an empty state only on a deliberately provisioned new node.
// It refuses to overwrite existing state, including corrupt state.
func (s FileStore) Bootstrap() error {
 if s.Path=="" {return ErrInvalidOperation}
 f,err:=os.OpenFile(s.Path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if errors.Is(err,os.ErrExist){return fmt.Errorf("revision fence already initialized: %w",err)}
 if err!=nil{return fmt.Errorf("bootstrap revision fence: %w",err)}
 if _,err=f.Write([]byte("{}"));err!=nil{f.Close();return err}
 if err=f.Sync();err!=nil{f.Close();return err}
 if err=f.Close();err!=nil{return err}
 d,err:=os.Open(filepath.Dir(s.Path))
 if err!=nil{return err}
 defer d.Close()
 return d.Sync()
}

func (s FileStore) Load() (map[string]Operation,error) {
 if s.Path=="" {return nil,ErrInvalidOperation}
 data,err:=os.ReadFile(s.Path)
 if errors.Is(err,os.ErrNotExist){return nil,ErrFenceStateMissing}
 if err!=nil{return nil,fmt.Errorf("read revision fence: %w",err)}
 var operations map[string]Operation
 if err:=json.Unmarshal(data,&operations);err!=nil{return nil,fmt.Errorf("decode revision fence: %w",err)}
 if operations==nil{return nil,errors.New("revision fence state must be an object")}
 for id,op:=range operations {
  if id!=op.SubscriptionID||id==""||op.PublicIPID==""||op.AttemptID==""||op.Revision<1 {
   return nil,fmt.Errorf("invalid persisted revision for subscription %q",id)
  }
 }
 return operations,nil
}

func (s FileStore) Save(operations map[string]Operation) error {
 if s.Path=="" {return ErrInvalidOperation}
 if operations==nil{return ErrInvalidOperation}
 for id,op:=range operations {
  if id!=op.SubscriptionID||id==""||op.PublicIPID==""||op.AttemptID==""||op.Revision<1{return ErrInvalidOperation}
 }
 data,err:=json.Marshal(operations)
 if err!=nil{return fmt.Errorf("encode revision fence: %w",err)}
 dir:=filepath.Dir(s.Path)
 tmp,err:=os.CreateTemp(dir,".revision-fence-*")
 if err!=nil{return fmt.Errorf("create revision fence temp file: %w",err)}
 defer os.Remove(tmp.Name())
 if err:=tmp.Chmod(0600);err!=nil{tmp.Close();return err}
 if _,err=tmp.Write(data);err!=nil{tmp.Close();return err}
 if err=tmp.Sync();err!=nil{tmp.Close();return err}
 if err=tmp.Close();err!=nil{return err}
 if err=os.Rename(tmp.Name(),s.Path);err!=nil{return fmt.Errorf("commit revision fence: %w",err)}
 d,err:=os.Open(dir)
 if err!=nil{return fmt.Errorf("open revision fence directory: %w",err)}
 defer d.Close()
 if err=d.Sync();err!=nil{return fmt.Errorf("sync revision fence directory: %w",err)}
 return nil
}
