package businessgen

// The fixture has real typed owner business logic, but deliberately no working
// execution adapters. Registration must not touch these fixture codecs or the
// resolver, and no successful invocation is claimed by this test.
const consumerImplementation = `package operations
import (
 "context"
 "errors"
 gen "example.test/business-demo/internal/amosgen/business"
 "github.com/ajent-social/amos/app/operation"
)
var Calls int
func Create(_ context.Context, _ operation.InvocationContext, req gen.OpWidgetsCreateRequest) (operation.Result[gen.OpWidgetsCreateResponse],error) {
 Calls++
 if req.Body.Priority < 0 { return operation.Result[gen.OpWidgetsCreateResponse]{},errors.New("negative priority") }
 return operation.Result[gen.OpWidgetsCreateResponse]{Kind:operation.ResultCreated,Value:gen.OpWidgetsCreateResponse{Id:gen.ModelWidgetId("created")}},nil
}
`

const consumerTest = `package consumer_test
import (
 "crypto/sha256"
 "errors"
 "testing"
 gen "example.test/business-demo/internal/amosgen/business"
 owner "example.test/business-demo/business/operations"
 "github.com/ajent-social/amos/app/operation"
 "github.com/ajent-social/amos/identity"
 "github.com/ajent-social/amos/policy"
 workspacecontext "github.com/ajent-social/amos/workspace/context"
)
type inputCodec struct{}
func (*inputCodec) SchemaDigest() [32]byte { return sha256.Sum256([]byte("synthetic input schema")) }
func (*inputCodec) DecodeCanonical(operation.RawInput)(gen.OpWidgetsCreateRequest,[]byte,error){ panic("registration decoded input") }
type outputCodec struct{}
func (*outputCodec) SchemaDigest() [32]byte { return sha256.Sum256([]byte("synthetic output schema")) }
func (*outputCodec) EncodeCanonical(gen.OpWidgetsCreateResponse)([]byte,error){ panic("registration encoded output") }
func (*outputCodec) ValidateCanonical([]byte)error{panic("registration validated output")}
type resolver struct{}
func (*resolver) Resolve(identity.Principal,workspacecontext.Selection,gen.OpWidgetsCreateRequest)(operation.ResolvedResources,error){panic("registration resolved authority")}
func bindings() gen.Bindings {
 return gen.Bindings{OpWidgetsCreate:gen.Binding[gen.OpWidgetsCreateRequest,gen.OpWidgetsCreateResponse]{
 Metadata:gen.BindingMetadata{Revision:"synthetic-v1",InputSchemaDigest:(&inputCodec{}).SchemaDigest(),OutputSchemaDigest:(&outputCodec{}).SchemaDigest(),Features:[]string{"typed-json"},OutputClass:operation.OutputReplaySafe,MaxReplayBytes:4096},
 Input:&inputCodec{},Resolver:&resolver{},Output:&outputCodec{},Handler:owner.Create,
 }}
}
func TestRealGeneratedRegistry(t *testing.T){
 registry,err:=gen.BuildRegistry(bindings());if err!=nil{t.Fatal(err)}
 definition,ok:=registry.Lookup("widgets.create");if !ok{t.Fatal("generated operation missing")}
 m:=definition.Metadata()
 if m.Method!="POST" || m.Path!="/widgets" || len(m.Requirements.Permissions)!=1 || m.Requirements.Permissions[0]!="widgets:create" || m.Requirements.Entitlements==nil || m.Requirements.Assurance!=identity.AAL2 || m.Requirements.MCPExposure!=policy.MCPChallenge || m.SideEffect!=operation.SideEffectWrite || m.Idempotency!=operation.IdempotencyRequired {t.Fatalf("source policy metadata changed: %#v",m)}
 if owner.Calls!=0{t.Fatal("registration executed business logic")}
 m.Requirements.Permissions[0]="weakened"
 if definition.Metadata().Requirements.Permissions[0]!="widgets:create"{t.Fatal("registry metadata mutable")}
}
func TestMissingImplementationCannotRegister(t *testing.T){
 b:=bindings();b.OpWidgetsCreate.Handler=nil
 if r,err:=gen.BuildRegistry(b);r!=nil || !errors.Is(err,operation.ErrInvalidDefinition){t.Fatalf("missing handler registered: %v",err)}
 if r,err:=gen.BuildRegistry(gen.Bindings{});r!=nil || !errors.Is(err,operation.ErrInvalidDefinition){t.Fatalf("empty bindings registered: %v",err)}
}
func TestMissingTypedDependencies(t *testing.T){
 for _,which:=range []string{"input","output","resolver"}{t.Run(which,func(t *testing.T){
  b:=bindings()
  switch which{case "input":var missing *inputCodec;b.OpWidgetsCreate.Input=missing;case "output":var missing *outputCodec;b.OpWidgetsCreate.Output=missing;case "resolver":var missing *resolver;b.OpWidgetsCreate.Resolver=missing}
  if r,err:=gen.BuildRegistry(b);r!=nil || !errors.Is(err,operation.ErrInvalidDefinition){t.Fatalf("typed-nil dependency registered: %v",err)}
 })}
}
func TestMismatchedSchemaDigests(t *testing.T){
 for _,which:=range []string{"input","output"}{t.Run(which,func(t *testing.T){
  b:=bindings();if which=="input"{b.OpWidgetsCreate.Metadata.InputSchemaDigest=sha256.Sum256([]byte("wrong"))}else{b.OpWidgetsCreate.Metadata.OutputSchemaDigest=sha256.Sum256([]byte("wrong"))}
  if r,err:=gen.BuildRegistry(b);r!=nil || !errors.Is(err,operation.ErrSchemaDigestMismatch){t.Fatalf("mismatched digest registered: %v",err)}
 })}
}
func TestMissingRuntimeMetadata(t *testing.T){
 b:=bindings();b.OpWidgetsCreate.Metadata.Features=nil
 if r,err:=gen.BuildRegistry(b);r!=nil || !errors.Is(err,operation.ErrInvalidDefinition){t.Fatalf("missing features registered: %v",err)}
}
`
