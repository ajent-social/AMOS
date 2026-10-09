package application

const serveMain = `package main
import (
 "context"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "os/signal"
 "strings"
 "strconv"
 "syscall"
 "github.com/google/uuid"
 "github.com/ajent-social/amos/apphost"
 delivery "github.com/ajent-social/amos/delivery/email"
 "github.com/ajent-social/amos/identity"
 "github.com/ajent-social/amos/identity/session"
 "github.com/ajent-social/amos/storage"
 workspaces "github.com/ajent-social/amos/workspace/store"
 "github.com/ajent-social/amos/examples/reference/app/business"
 "github.com/ajent-social/amos/examples/reference/ui"
)
type runtimeConfig struct {SchemaVersion int; InstallationID,ApplicationID,EnvironmentID,Origin,MaterialKey,RateKey string}
func main(){
 var err error
 switch {case len(os.Args)==2 && os.Args[1]=="preflight-database":
 if preflightDatabase()!=nil{fmt.Fprintln(os.Stderr,"app: local database port unavailable");os.Exit(3)};return
 case len(os.Args)==2 && os.Args[1]=="migrate":err=runMigration()
 case len(os.Args)==1 || (len(os.Args)==2 && os.Args[1]=="serve"):err=run()
 default:err=errors.New("command unavailable")}
 if err!=nil{fmt.Fprintln(os.Stderr,"app: startup or serving failed");os.Exit(1)}
}
func preflightDatabase()error{
 port,err:=strconv.Atoi(os.Getenv("AMOS_DB_PORT"));if err!=nil || port<1 || port>65535{return errors.New("invalid database port")}
 listener,err:=net.Listen("tcp4",net.JoinHostPort("127.0.0.1",strconv.Itoa(port)));if err!=nil{return err};return listener.Close()
}
func run()error{
 root,err:=openPrivateRuntime();if err!=nil{return err};defer func(){_ = root.Close()}()
 cfg,err:=readRuntime(root);if err!=nil{return err}
 installation,err:=uuid.Parse(cfg.InstallationID);if err!=nil{return err}
 application,err:=uuid.Parse(cfg.ApplicationID);if err!=nil{return err}
 environment,err:=uuid.Parse(cfg.EnvironmentID);if err!=nil{return err}
 material,err:=hex.DecodeString(cfg.MaterialKey);if err!=nil || len(material)!=32{return errors.New("invalid key")}
 rate,err:=hex.DecodeString(cfg.RateKey);if err!=nil || len(rate)!=32{return errors.New("invalid key")}
 origin,err:=delivery.ParseApplicationOrigin(cfg.Origin,true);if err!=nil || origin.Scheme!="http"{return errors.New("local origin unavailable")}
 config:=os.Getenv("AMOS_RUNTIME_DATABASE_URL");if config==""{return errors.New("runtime database configuration unavailable")}
 if err:=root.Mkdir("mail",0700);err!=nil && !errors.Is(err,os.ErrExist){return err}
 mail,err:=root.Lstat("mail");if err!=nil || !privateOwned(mail,0700) || !mail.IsDir(){return errors.New("private mailbox unavailable")}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer cancel()
 host,err:=apphost.NewLocal(ctx,apphost.LocalConfig{InstallationID:installation,ApplicationID:application,EnvironmentID:environment,Origin:cfg.Origin,DatabaseURL:config,MailDirectory:".amos/mail",MaterialKey:material,RateKey:rate,Business:businessRoutes});if err!=nil{return err}
 defer func(){_ = host.Close()}()
 listener,err:=net.Listen("tcp",origin.Host);if err!=nil{return err}
 defer func(){_ = listener.Close()}()
 return host.Serve(ctx,listener)
}
func privateOwned(info os.FileInfo,mode os.FileMode)bool{
 stat,ok:=info.Sys().(*syscall.Stat_t);return ok && stat.Uid==uint32(os.Geteuid()) && info.Mode().Perm()==mode && info.Mode()&os.ModeSymlink==0
}
func openPrivateRuntime()(*os.Root,error){
 before,err:=os.Lstat(".amos");if err!=nil || !before.IsDir() || !privateOwned(before,0700){return nil,errors.New("private runtime directory unavailable")}
 fd,err:=syscall.Open(".amos",syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW,0);if err!=nil{return nil,errors.New("private runtime directory unavailable")}
 pinned:=os.NewFile(uintptr(fd),"runtime-directory");defer func(){_ = pinned.Close()}()
 info,err:=pinned.Stat();if err!=nil || !os.SameFile(before,info){return nil,errors.New("private runtime directory unavailable")}
 root,err:=os.OpenRoot(".amos");if err!=nil{return nil,errors.New("private runtime directory unavailable")}
 after,err:=root.Stat(".");if err!=nil || !os.SameFile(info,after){_ = root.Close();return nil,errors.New("private runtime directory unavailable")};return root,nil
}
func readRuntime(root *os.Root)(runtimeConfig,error){
 before,err:=root.Lstat("runtime.json");if err!=nil || !before.Mode().IsRegular() || !privateOwned(before,0600) || before.Size()>4096{return runtimeConfig{},errors.New("private runtime configuration unavailable")}
 f,err:=root.Open("runtime.json");if err!=nil{return runtimeConfig{},errors.New("private runtime configuration unavailable")};defer func(){_ = f.Close()}()
 after,err:=f.Stat();if err!=nil || !os.SameFile(before,after) || !privateOwned(after,0600){return runtimeConfig{},errors.New("private runtime configuration unavailable")}
 body,err:=io.ReadAll(io.LimitReader(f,4097));if err!=nil || len(body)>4096{return runtimeConfig{},errors.New("private runtime configuration unavailable")}
 var cfg runtimeConfig;decoder:=json.NewDecoder(strings.NewReader(string(body)));decoder.DisallowUnknownFields()
 if decoder.Decode(&cfg)!=nil || decoder.Decode(new(any))!=io.EOF || cfg.SchemaVersion!=1{return runtimeConfig{},errors.New("private runtime configuration unavailable")};return cfg,nil
}
func businessRoutes(db *storage.DB,sessions *session.Service)([]apphost.Route,error){
 todos,err:=business.New(db);if err!=nil{return nil,err}
 selectWorkspace:=func(r *http.Request)(string,error){
  principal,ok:=identity.PrincipalFromContext(r.Context());if !ok{return "",errors.New("authentication unavailable")}
  var selected workspaces.Workspace
  err:=db.WithTx(r.Context(),nil,func(tx *sql.Tx)error{store,err:=workspaces.New(tx);if err!=nil{return err};selected,err=store.FindPersonalWorkspace(r.Context(),workspaces.Scope{InstallationID:principal.InstallationID(),ApplicationID:principal.ApplicationID()},principal.PersonID());return err})
  return selected.ID.String(),err
 }
 pages:=sessions.Middleware(ui.NewHandler(todos,ui.Options{CSRFToken:sessions.CSRFToken,PersonalWorkspace:selectWorkspace}))
 return []apphost.Route{{Method:"GET",Pattern:"/",Handler:pages},{Method:"GET",Pattern:"/todos",Handler:pages},{Method:"POST",Pattern:"/todos",Handler:pages},{Method:"GET",Pattern:"/todos/*",Handler:pages},{Method:"POST",Pattern:"/todos/*",Handler:pages}},nil
}
`

const migrateMain = `package main
import (
 "context"
 "errors"
 "os"
 "time"
 "github.com/ajent-social/amos/migrations"
 "github.com/ajent-social/amos/identity/mfa"
 "github.com/ajent-social/amos/identity/federation"
 "github.com/ajent-social/amos/billing/reconcile"
 "github.com/ajent-social/amos/storage"
 reference "github.com/ajent-social/amos/examples/reference/migrations"
)
func runMigration()error{
 config:=os.Getenv("AMOS_MIGRATION_DATABASE_URL");if config==""{return errors.New("migration database configuration unavailable")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=storage.Open(ctx,config);if err!=nil{return err};defer func(){_ = db.Close()}()
 ingress,err:=migrations.BillingWebhookIngress(9);if err!=nil{return err}
 binding,err:=migrations.RuntimeBinding(10);if err!=nil{return err}
 magic,err:=migrations.MagicBrowserBinding(11);if err!=nil{return err}
 assurance,err:=migrations.SessionAssurance(12);if err!=nil{return err}
 factors,err:=mfa.Fragment(13);if err!=nil{return err}
 limits,err:=migrations.MFAProtection(14);if err!=nil{return err}
 reconciliation,err:=reconcile.Schema(15);if err!=nil{return err}
 federationFlow,err:=federation.Fragment(16);if err!=nil{return err}
 invocations,err:=migrations.OperationInvocations(17);if err!=nil{return err}
 registry,err:=migrations.Core(reference.Fragment(),ingress,binding,magic,assurance,factors,limits,reconciliation,federationFlow,invocations);if err!=nil{return err}
 return storage.Migrate(ctx,db,registry)
}
`

const runtimeTests = `package main
import("os";"path/filepath";"testing";"net";"strconv")
func TestDatabasePortPreflight(t *testing.T){
 listener,err:=net.Listen("tcp4","127.0.0.1:0");if err!=nil{t.Fatal(err)};defer func(){_ = listener.Close()}()
 t.Setenv("AMOS_DB_PORT",strconv.Itoa(listener.Addr().(*net.TCPAddr).Port));if preflightDatabase()==nil{t.Fatal("occupied port accepted")}
 for _,port:=range []string{"","0","65536","invalid"}{t.Setenv("AMOS_DB_PORT",port);if preflightDatabase()==nil{t.Fatal("invalid port accepted")}}
}
func TestPrivateRuntimeDirectory(t *testing.T){
 old,err:=os.Getwd();if err!=nil{t.Fatal("working directory unavailable")}
 temp:=t.TempDir();if err:=os.Chdir(temp);err!=nil{t.Fatal("change directory")};t.Cleanup(func(){if err:=os.Chdir(old);err!=nil{t.Error("restore working directory")}})
 if err:=os.Mkdir(".amos",0700);err!=nil{t.Fatal("create private directory")}
 if err:=os.WriteFile(".amos/runtime.json",[]byte("{\"SchemaVersion\":1}"),0600);err!=nil{t.Fatal("write private config")}
 root,err:=openPrivateRuntime();if err!=nil{t.Fatal("valid private directory rejected")}
 if _,err:=readRuntime(root);err!=nil{t.Fatal("valid private config rejected")}
 if err:=root.Close();err!=nil{t.Fatal("close private directory")}
 if err:=os.Chmod(".amos",0755);err!=nil{t.Fatal("change directory mode")}
 if root,err:=openPrivateRuntime();err==nil{_ = root.Close();t.Fatal("public runtime directory accepted")}
 if err:=os.Chmod(".amos",0700);err!=nil{t.Fatal("restore directory mode")}
 if err:=os.Rename(".amos","saved-runtime");err!=nil{t.Fatal("rename directory")}
 if err:=os.Symlink(filepath.Join(temp,"saved-runtime"),".amos");err!=nil{t.Fatal("create directory symlink")}
 if root,err:=openPrivateRuntime();err==nil{_ = root.Close();t.Fatal("symlink runtime directory accepted")}
 if err:=os.Remove(".amos");err!=nil{t.Fatal("remove owned symlink")}
 if err:=os.Rename("saved-runtime",".amos");err!=nil{t.Fatal("restore directory")}
 root,err=openPrivateRuntime();if err!=nil{t.Fatal("reopen private directory")};defer func(){_ = root.Close()}()
 if err:=os.Rename(".amos/runtime.json",".amos/saved.json");err!=nil{t.Fatal("rename private config")}
 if err:=os.Symlink("saved.json",".amos/runtime.json");err!=nil{t.Fatal("create config symlink")}
 if _,err:=readRuntime(root);err==nil{t.Fatal("symlink runtime configuration accepted")}
}
`
