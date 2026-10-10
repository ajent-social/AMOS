package state

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type recordedResource struct {
	token   string
	name    string
	inputs  resource.PropertyMap
	protect bool
}

type bootstrapMocks struct {
	mu        sync.Mutex
	resources []recordedResource
}

func (m *bootstrapMocks) Call(pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func (m *bootstrapMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, recordedResource{token: args.TypeToken, name: args.Name, inputs: args.Inputs, protect: args.RegisterRPC.GetProtect()})
	outputs := args.Inputs.Copy()
	if args.TypeToken == "aws:s3/bucketV2:BucketV2" {
		outputs[resource.PropertyKey("arn")] = resource.NewStringProperty("arn:aws:s3:::amos-state")
		outputs[resource.PropertyKey("bucket")] = resource.NewStringProperty("amos-state")
	}
	if args.TypeToken == "aws:kms/key:Key" {
		outputs[resource.PropertyKey("arn")] = resource.NewStringProperty("arn:aws:kms:us-east-1:123456789012:key/key-id")
		outputs[resource.PropertyKey("keyId")] = resource.NewStringProperty("key-id")
	}
	if args.TypeToken == "aws:iam/role:Role" {
		outputs[resource.PropertyKey("arn")] = resource.NewStringProperty("arn:aws:iam::123456789012:role/" + args.Name)
		outputs[resource.PropertyKey("name")] = resource.NewStringProperty(args.Name)
	}
	return args.Name + "-id", outputs, nil
}

func (m *bootstrapMocks) all() []recordedResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedResource(nil), m.resources...)
}

func validArgs() BootstrapArgs {
	return BootstrapArgs{
		Name: "state-foundation", BucketName: "amos-state-example",
		Environment: "staging", InstallationID: "01890f3e-7c00-7000-8000-000000000001",
		StatePrefix:          "amos/staging/01890f3e-7c00-7000-8000-000000000001",
		StateWriterPrincipal: "arn:aws:iam::123456789012:role/state-assumer",
		RecoveryPrincipal:    "arn:aws:iam::123456789012:role/recovery-assumer",
		KeyAdministrator:     "arn:aws:iam::123456789012:role/key-admin", Region: "us-east-1",
	}
}

func TestBootstrapRequiresExplicitIsolatedInputs(t *testing.T) {
	base := validArgs()
	cases := []struct {
		name string
		edit func(*BootstrapArgs)
	}{
		{"missing bucket", func(a *BootstrapArgs) { a.BucketName = "" }},
		{"invalid region", func(a *BootstrapArgs) { a.Region = "not-a-region" }},
		{"invalid bucket name", func(a *BootstrapArgs) { a.BucketName = "BadBucket" }},
		{"invalid bucket label", func(a *BootstrapArgs) { a.BucketName = "valid-.bucket" }},
		{"ambiguous prefix", func(a *BootstrapArgs) { a.StatePrefix = "amos//state" }},
		{"missing region", func(a *BootstrapArgs) { a.Region = "" }},
		{"noncanonical owner", func(a *BootstrapArgs) { a.InstallationID = "01890f3e-7c00-6000-8000-000000000001" }},
		{"path traversal", func(a *BootstrapArgs) { a.StatePrefix = "amos/../shared" }},
		{"query prefix", func(a *BootstrapArgs) { a.StatePrefix = "amos/state?redirect=other" }},
		{"same writer and recovery", func(a *BootstrapArgs) { a.RecoveryPrincipal = a.StateWriterPrincipal }},
		{"wildcard administrator", func(a *BootstrapArgs) { a.KeyAdministrator = "arn:aws:iam::123456789012:role/*" }},
		{"wrong partition principal", func(a *BootstrapArgs) { a.KeyAdministrator = "arn:aws-cn:iam::123456789012:role/key-admin" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			tc.edit(&args)
			if err := validateBootstrapArgs(args); err == nil {
				t.Fatal("invalid bootstrap input was accepted")
			}
		})
	}
}

func TestBootstrapPulumiMocksEnforceRetentionAndSeparation(t *testing.T) {
	mocks := &bootstrapMocks{}
	backendURL := make(chan string, 1)
	secretsProvider := make(chan string, 1)
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		component, err := NewBootstrap(ctx, "foundation", validArgs())
		if err != nil {
			return err
		}
		component.BackendURL.ApplyT(func(value string) bool {
			backendURL <- value
			return true
		})
		component.SecretsProvider.ApplyT(func(value string) bool {
			secretsProvider <- value
			return true
		})
		return nil
	}, pulumi.WithMocks("amos", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-backendURL:
		if !strings.Contains(value, "s3://amos-state/amos/staging/") || !strings.Contains(value, "region=us-east-1") {
			t.Fatalf("backend URL omitted bucket, prefix, or region: %q", value)
		}
	default:
		t.Fatal("backend URL did not resolve in Pulumi mocks")
	}
	select {
	case value := <-secretsProvider:
		if !strings.HasPrefix(value, "awskms://alias/amos/staging/") || !strings.Contains(value, "context_amosInstallationID=01890f3e") || !strings.Contains(value, "context_amosEnvironment=staging") {
			t.Fatalf("secrets-provider URL omitted scoped KMS context: %q", value)
		}
	default:
		t.Fatal("secrets-provider URL did not resolve in Pulumi mocks")
	}
	resources := mocks.all()
	providerRegion := ""
	for _, r := range resources {
		if r.token == "pulumi:providers:aws" {
			providerRegion = r.inputs["region"].StringValue()
		}
	}
	if providerRegion != "us-east-1" {
		t.Fatalf("AWS provider region = %q, want explicitly selected region", providerRegion)
	}
	find := func(token string) (resource.PropertyMap, bool) {
		for _, r := range resources {
			if r.token == token {
				return r.inputs, true
			}
		}
		return nil, false
	}
	bucket, ok := find("aws:s3/bucketV2:BucketV2")
	if !ok || bucket["forceDestroy"].BoolValue() {
		t.Fatal("state bucket missing or configured for deletion")
	}
	protectedBucket, protectedKeys := false, 0
	for _, r := range resources {
		if r.token == "aws:s3/bucketV2:BucketV2" {
			protectedBucket = r.protect
		}
		if r.token == "aws:kms/key:Key" {
			if r.protect {
				protectedKeys++
			}
		}
	}
	if !protectedBucket || protectedKeys != 2 {
		t.Fatal("state bucket and both KMS keys must be protected from stack teardown")
	}
	versioning, ok := find("aws:s3/bucketVersioningV2:BucketVersioningV2")
	if !ok || versioning["versioningConfiguration"].ObjectValue()["status"].StringValue() != "Enabled" {
		t.Fatal("state bucket versioning is not enabled")
	}
	access, ok := find("aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock")
	if !ok || !access["blockPublicAcls"].BoolValue() || !access["blockPublicPolicy"].BoolValue() || !access["ignorePublicAcls"].BoolValue() || !access["restrictPublicBuckets"].BoolValue() {
		t.Fatal("state bucket public access block is incomplete")
	}
	encryption, ok := find("aws:s3/bucketServerSideEncryptionConfigurationV2:BucketServerSideEncryptionConfigurationV2")
	if !ok {
		t.Fatal("default state encryption is missing")
	}
	rules := encryption["rules"].ArrayValue()
	if len(rules) != 1 || rules[0].ObjectValue()["applyServerSideEncryptionByDefault"].ObjectValue()["sseAlgorithm"].StringValue() != "aws:kms" {
		t.Fatal("state bucket does not default to KMS encryption")
	}
	key, ok := find("aws:kms/key:Key")
	if !ok || !key["enableKeyRotation"].BoolValue() {
		t.Fatal("state key is not configured for automatic rotation")
	}
	secretKeys := 0
	for _, r := range resources {
		if r.token == "aws:kms/key:Key" && strings.Contains(r.name, "pulumi-secrets-key") {
			secretKeys++
			if !r.inputs["enableKeyRotation"].BoolValue() {
				t.Fatal("Pulumi secrets key rotation is disabled")
			}
		}
	}
	if secretKeys != 1 {
		t.Fatalf("Pulumi secrets keys = %d, want one separate key", secretKeys)
	}
	_, stateOK := find("aws:iam/role:Role")
	recoveryRoles := 0
	for _, r := range resources {
		if r.token == "aws:iam/role:Role" && strings.Contains(r.name, "recovery") {
			recoveryRoles++
		}
	}
	stateWriterRoles := 0
	for _, r := range resources {
		if r.token == "aws:iam/role:Role" && strings.Contains(r.name, "state-writer") {
			stateWriterRoles++
		}
	}
	if !stateOK || stateWriterRoles != 1 || recoveryRoles != 1 {
		t.Fatal("writer and recovery roles are not distinct")
	}
}

func TestBootstrapWriterAndRecoveryPoliciesAreSeparated(t *testing.T) {
	writer, err := writerPolicyJSON("arn:aws:s3:::state", "arn:aws:kms:us-east-1:123456789012:key/state-key", "arn:aws:kms:us-east-1:123456789012:key/secrets-key", "amos/staging/installation", "state", "us-east-1", "01890f3e-7c00-7000-8000-000000000001", "staging")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := recoveryPolicyJSON("arn:aws:s3:::state", "arn:aws:kms:us-east-1:123456789012:key/state-key", "arn:aws:kms:us-east-1:123456789012:key/secrets-key", "amos/staging/installation", "state", "us-east-1", "01890f3e-7c00-7000-8000-000000000001", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writer, "s3:PutObject") || !strings.Contains(writer, "amos/staging/installation/.pulumi/locks/*") || !strings.Contains(writer, "kms:EncryptionContext:amosInstallationID") || !strings.Contains(writer, "key/secrets-key") {
		t.Fatal("state writer cannot update its prefix, locks, and scoped Pulumi secret material")
	}
	if strings.Contains(recovery, "s3:PutObject") || strings.Contains(recovery, "s3:DeleteObject") || !strings.Contains(recovery, "s3:GetObjectVersion") || !strings.Contains(recovery, "s3:ListBucketVersions") || !strings.Contains(recovery, "kms:Decrypt") || !strings.Contains(recovery, "key/secrets-key") || strings.Contains(recovery, `"kms:Encrypt"`) {
		t.Fatal("recovery grants exceed read-and-decrypt access")
	}
	stateKeyPolicy, err := keyPolicyJSON("arn:aws:iam::123456789012:role/key-admin", "arn:aws:iam::123456789012:role/state", "arn:aws:iam::123456789012:role/recovery", "state", "us-east-1")
	if err != nil || !strings.Contains(stateKeyPolicy, "kms:ViaService") || !strings.Contains(stateKeyPolicy, "kms:EncryptionContext:aws:s3:arn") {
		t.Fatalf("S3 encryption key lost its S3-only conditions: %v", err)
	}
	secretKeyPolicy, err := pulumiSecretsKeyPolicyJSON("arn:aws:iam::123456789012:role/key-admin", "arn:aws:iam::123456789012:role/state", "arn:aws:iam::123456789012:role/recovery", "01890f3e-7c00-7000-8000-000000000001", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(secretKeyPolicy, "kms:ViaService") || !strings.Contains(secretKeyPolicy, "kms:EncryptionContext:amosInstallationID") || !strings.Contains(secretKeyPolicy, "kms:EncryptionContext:amosEnvironment") {
		t.Fatal("Pulumi secrets key must use direct, installation-scoped encryption context without the S3 ViaService condition")
	}
	var secretPolicy map[string]any
	if err := json.Unmarshal([]byte(secretKeyPolicy), &secretPolicy); err != nil {
		t.Fatal(err)
	}
	secretStatements := secretPolicy["Statement"].([]any)
	writerActions := secretStatements[1].(map[string]any)["Action"].([]any)
	if len(writerActions) != 3 {
		t.Fatalf("writer direct-KMS actions = %#v", writerActions)
	}
	recoveryStatement := secretStatements[3].(map[string]any)
	if recoveryStatement["Action"] != "kms:Decrypt" {
		t.Fatalf("recovery direct-KMS action = %#v, want decrypt only", recoveryStatement["Action"])
	}
	writerCondition := secretStatements[1].(map[string]any)["Condition"].(map[string]any)["StringEquals"].(map[string]any)
	for _, tc := range []struct {
		name    string
		id, env string
		want    bool
	}{
		{"correct context", "01890f3e-7c00-7000-8000-000000000001", "staging", true},
		{"other installation", "01890f3e-7c00-7000-8000-000000000002", "staging", false},
		{"other environment", "01890f3e-7c00-7000-8000-000000000001", "production", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := writerCondition["kms:EncryptionContext:amosInstallationID"] == tc.id && writerCondition["kms:EncryptionContext:amosEnvironment"] == tc.env
			if got != tc.want {
				t.Fatalf("context accepted = %t, want %t", got, tc.want)
			}
		})
	}
	bucketPolicyText, err := bucketPolicy("arn:aws:s3:::state", "arn:aws:iam::123456789012:role/state", "arn:aws:iam::123456789012:role/recovery", "amos/staging/installation")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(bucketPolicyText), &parsed); err != nil {
		t.Fatal(err)
	}
	statements, ok := parsed["Statement"].([]any)
	if !ok {
		t.Fatal("bucket policy has no statements")
	}
	checked := 0
	for _, raw := range statements {
		statement, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("bucket policy statement has an invalid shape")
		}
		if statement["Sid"] != "DenyListingByOtherPrincipals" && statement["Sid"] != "DenyStateObjectsByOtherPrincipals" {
			continue
		}
		checked++
		if statement["Principal"] != "*" {
			t.Fatalf("deny statement principal = %#v, want wildcard with condition", statement["Principal"])
		}
		if _, hasNotPrincipal := statement["NotPrincipal"]; hasNotPrincipal {
			t.Fatal("bucket deny must avoid NotPrincipal with permissions boundaries")
		}
		condition := statement["Condition"].(map[string]any)["ArnNotEquals"].(map[string]any)
		allowed, ok := condition["aws:PrincipalArn"].([]any)
		if !ok || len(allowed) != 2 {
			t.Fatalf("allowed PrincipalArn set = %#v", condition["aws:PrincipalArn"])
		}
		isDenied := func(arn string) bool { return arn != allowed[0].(string) && arn != allowed[1].(string) }
		for _, tc := range []struct {
			name, arn string
			want      bool
		}{
			{"state writer, including boundary-bearing role", "arn:aws:iam::123456789012:role/state", false},
			{"recovery role", "arn:aws:iam::123456789012:role/recovery", false},
			{"unlisted role", "arn:aws:iam::123456789012:role/other", true},
		} {
			if got := isDenied(tc.arn); got != tc.want {
				t.Errorf("%s denied = %t, want %t", tc.name, got, tc.want)
			}
		}
	}
	if checked != 2 {
		t.Fatalf("PrincipalArn conditioned deny statements = %d, want 2", checked)
	}
}
