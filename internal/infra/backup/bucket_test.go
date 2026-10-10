package backup

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type bucketResource struct {
	token  string
	inputs resource.PropertyMap
}

type bucketMocks struct {
	mu        sync.Mutex
	resources []bucketResource
}

func (m *bucketMocks) Call(pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func (m *bucketMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, bucketResource{token: args.TypeToken, inputs: args.Inputs})
	outputs := args.Inputs.Copy()
	if args.TypeToken == "aws:s3/bucket:Bucket" {
		outputs[resource.PropertyKey("bucket")] = resource.NewStringProperty("amos-test-bucket")
		outputs[resource.PropertyKey("arn")] = resource.NewStringProperty("arn:aws:s3:::amos-test-bucket")
	}
	return args.Name + "-id", outputs, nil
}

func (m *bucketMocks) all() []bucketResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bucketResource(nil), m.resources...)
}

func validTestBucketArgs() BucketArgs {
	return BucketArgs{
		Name: "amos-test", Prefix: "installations/test", KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/abcd",
		RuntimeRoleARN: "arn:aws:iam::123456789012:role/amos-runtime",
		RestoreRoleARN: "arn:aws:iam::123456789012:role/amos-restore",
		RetentionDays:  90, NoncurrentDays: 90,
	}
}

func TestBucketRequiresExplicitRetentionAndDistinctScopedRoles(t *testing.T) {
	base := validTestBucketArgs()
	if err := validateBucketArgs(base); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*BucketArgs)
	}{
		{"no current retention", func(a *BucketArgs) { a.RetentionDays = 0 }},
		{"no noncurrent retention", func(a *BucketArgs) { a.NoncurrentDays = 0 }},
		{"same runtime and restore identity", func(a *BucketArgs) { a.RestoreRoleARN = a.RuntimeRoleARN }},
		{"unscoped prefix", func(a *BucketArgs) { a.Prefix = "installations/../all" }},
		{"wildcard prefix", func(a *BucketArgs) { a.Prefix = "installations/*" }},
		{"cross-account restore role", func(a *BucketArgs) { a.RestoreRoleARN = "arn:aws:iam::999999999999:role/amos-restore" }},
		{"invalid key", func(a *BucketArgs) { a.KMSKeyARN = "arn:aws:kms:us-east-1:123456789012:key/*" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			tc.change(&args)
			if err := validateBucketArgs(args); err == nil {
				t.Fatal("accepted invalid bucket policy")
			}
		})
	}
}

func TestBucketPulumiMocksSetPrivateVersionedEncryptedRetention(t *testing.T) {
	mocks := &bucketMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewBucket(ctx, "backup", validTestBucketArgs())
		return err
	}, pulumi.WithMocks("amos", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	resources := mocks.all()
	find := func(token string) (resource.PropertyMap, bool) {
		for _, r := range resources {
			if r.token == token {
				return r.inputs, true
			}
		}
		return nil, false
	}
	bucket, ok := find("aws:s3/bucket:Bucket")
	if !ok || bucket["forceDestroy"].BoolValue() {
		t.Fatal("bucket must be retained on destroy unless explicitly emptied")
	}
	public, ok := find("aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock")
	if !ok || !public["blockPublicAcls"].BoolValue() || !public["blockPublicPolicy"].BoolValue() ||
		!public["ignorePublicAcls"].BoolValue() || !public["restrictPublicBuckets"].BoolValue() {
		t.Fatal("bucket public access is not fully blocked")
	}
	versioning, ok := find("aws:s3/bucketVersioningV2:BucketVersioningV2")
	if !ok || versioning["versioningConfiguration"].ObjectValue()["status"].StringValue() != "Enabled" {
		t.Fatal("bucket versioning is not enabled")
	}
	encryption, ok := find("aws:s3/bucketServerSideEncryptionConfigurationV2:BucketServerSideEncryptionConfigurationV2")
	if !ok {
		t.Fatal("KMS encryption configuration missing")
	}
	rules := encryption["rules"].ArrayValue()
	if len(rules) != 1 {
		t.Fatalf("got %d encryption rules", len(rules))
	}
	encryptionRule := rules[0].ObjectValue()
	if encryptionRule["bucketKeyEnabled"].BoolValue() || encryptionRule["applyServerSideEncryptionByDefault"].ObjectValue()["sseAlgorithm"].StringValue() != "aws:kms" ||
		encryptionRule["applyServerSideEncryptionByDefault"].ObjectValue()["kmsMasterKeyId"].StringValue() != validTestBucketArgs().KMSKeyARN {
		t.Fatal("KMS encryption or per-object encryption context is not enforced")
	}
	lifecycle, ok := find("aws:s3/bucketLifecycleConfigurationV2:BucketLifecycleConfigurationV2")
	if !ok {
		t.Fatal("explicit lifecycle configuration missing")
	}
	lifecycleRules := lifecycle["rules"].ArrayValue()
	if len(lifecycleRules) != 1 {
		t.Fatalf("got %d lifecycle rules", len(lifecycleRules))
	}
	lifecycleRule := lifecycleRules[0].ObjectValue()
	if lifecycleRule["status"].StringValue() != "Enabled" ||
		lifecycleRule["filter"].ObjectValue()["prefix"].StringValue() != "installations/test/" ||
		lifecycleRule["expiration"].ObjectValue()["days"].NumberValue() != 90 ||
		lifecycleRule["noncurrentVersionExpiration"].ObjectValue()["noncurrentDays"].NumberValue() != 90 {
		t.Fatalf("lifecycle retention is not explicit and prefix scoped: %#v", lifecycleRule)
	}
}

func decodePolicy(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var doc struct {
		Statement []map[string]any `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode IAM policy: %v", err)
	}
	return doc.Statement
}

func statementForAction(t *testing.T, statements []map[string]any, action string) map[string]any {
	t.Helper()
	for _, statement := range statements {
		actions, ok := statement["Action"].([]any)
		if !ok {
			continue
		}
		for _, candidate := range actions {
			if candidate == action {
				return statement
			}
		}
	}
	t.Fatalf("no IAM statement grants %s", action)
	return nil
}

func assertKMSGrant(t *testing.T, statements []map[string]any, action, keyARN, endpoint, objectARN string) {
	t.Helper()
	statement := statementForAction(t, statements, action)
	actions := statement["Action"].([]any)
	if !reflect.DeepEqual(actions, []any{action}) || statement["Effect"] != "Allow" || statement["Resource"] != keyARN {
		t.Fatalf("%s grant is broader or targets the wrong key: %#v", action, statement)
	}
	condition, ok := statement["Condition"].(map[string]any)
	if !ok {
		t.Fatalf("%s grant has no scoped condition: %#v", action, statement)
	}
	viaService, ok := condition["StringEquals"].(map[string]any)
	if !ok || !reflect.DeepEqual(viaService, map[string]any{"kms:ViaService": endpoint}) {
		t.Fatalf("%s grant has wrong ViaService condition: %#v", action, condition)
	}
	context, ok := condition["StringLike"].(map[string]any)
	if !ok || !reflect.DeepEqual(context, map[string]any{"kms:EncryptionContext:aws:s3:arn": objectARN}) {
		t.Fatalf("%s grant has wrong encryption-context condition: %#v", action, condition)
	}
}

func TestBucketPoliciesSeparateRuntimeWriteAndRestoreRead(t *testing.T) {
	args := validTestBucketArgs()
	policy, err := bucketPolicyJSON("arn:aws:s3:::amos-test", "amos-test", args)
	if err != nil {
		t.Fatal(err)
	}
	var bucketDoc map[string]any
	if err := json.Unmarshal([]byte(policy), &bucketDoc); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(bucketDoc)
	for _, want := range []string{"DenyInsecureTransport", "RequireKMSEncryption", "RequireSelectedKMSKey", "RuntimeCannotReadOrDelete", "s3:DeleteObjectVersion"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("bucket policy omitted %q", want)
		}
	}
	mocks := &bucketMocks{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewBucket(ctx, "backup", args)
		return err
	}, pulumi.WithMocks("amos", "test", mocks)); err != nil {
		t.Fatal(err)
	}
	var runtime, restore string
	for _, item := range mocks.all() {
		if item.token != "aws:iam/rolePolicy:RolePolicy" {
			continue
		}
		role := item.inputs["role"].StringValue()
		switch role {
		case "amos-runtime":
			runtime = item.inputs["policy"].StringValue()
		case "amos-restore":
			restore = item.inputs["policy"].StringValue()
		}
	}
	if runtime == "" || restore == "" {
		t.Fatalf("expected distinct runtime and restore policies; runtime=%q restore=%q", runtime, restore)
	}
	args = validTestBucketArgs()
	objectARN := "arn:aws:s3:::amos-test-bucket/" + args.Prefix + "/*"
	runtimeStatements := decodePolicy(t, runtime)
	restoreStatements := decodePolicy(t, restore)
	runtimeS3 := statementForAction(t, runtimeStatements, "s3:PutObject")
	if runtimeS3["Resource"] != objectARN {
		t.Fatalf("runtime S3 write escaped its prefix: %#v", runtimeS3)
	}
	if strings.Contains(runtime, "s3:GetObject") || strings.Contains(runtime, "s3:DeleteObject") ||
		strings.Contains(restore, "s3:DeleteObject") {
		t.Fatalf("runtime/restore permissions cross authority boundary: runtime=%s restore=%s", runtime, restore)
	}
	assertKMSGrant(t, runtimeStatements, "kms:GenerateDataKey", args.KMSKeyARN, "s3.us-east-1.amazonaws.com", objectARN)
	assertKMSGrant(t, restoreStatements, "kms:Decrypt", args.KMSKeyARN, "s3.us-east-1.amazonaws.com", objectARN)
	if strings.Contains(runtime, "kms:Decrypt") || strings.Contains(restore, "kms:GenerateDataKey") {
		t.Fatalf("runtime and restore KMS authority crossed: runtime=%s restore=%s", runtime, restore)
	}
}
