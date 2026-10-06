package aws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type policyStatement struct {
	Sid       string                    `json:"Sid"`
	Action    []string                  `json:"Action"`
	Resource  any                       `json:"Resource"`
	Condition map[string]map[string]any `json:"Condition"`
}

func TestIdentityPulumiMocksScopeHostPermissions(t *testing.T) {
	mocks := &awsMocks{}
	args := IdentityArgs{
		Name: "example", Environment: "staging", InstallationID: "01890f3e-7c00-7000-8000-000000000001", ManagementProfile: ManagementProfileSessionManager,
		Region: "us-east-1", LogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:amos-host",
		RepositoryARNs:  []string{"arn:aws:ecr:us-east-1:123456789012:repository/amos"},
		SecretARNs:      []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:amos/runtime-a"},
		BackupBucketARN: "arn:aws:s3:::amos-backup-example", BackupObjectPrefix: "installations/01890f3e-7c00-7000-8000-000000000001/staging",
		BackupKMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555",
		SecretKMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { _, err := NewIdentity(ctx, "reference", args); return err }, pulumi.WithMocks("amos", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	resources := mocks.all()
	role, ok := findResource(resources, "aws:iam/role:Role")
	if !ok {
		t.Fatal("host role missing")
	}
	var trust map[string]any
	if err := json.Unmarshal([]byte(role["assumeRolePolicy"].StringValue()), &trust); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(role["assumeRolePolicy"].StringValue()), "iam:PassRole") {
		t.Fatal("trust policy grants role passing")
	}
	profile, ok := findResource(resources, "aws:iam/instanceProfile:InstanceProfile")
	if !ok {
		t.Fatal("instance profile missing")
	}
	assertOwnerTags(t, profile["tags"].ObjectValue(), args.Environment, args.InstallationID)
	policy, ok := findResource(resources, "aws:iam/rolePolicy:RolePolicy")
	if !ok {
		t.Fatal("inline host policy missing")
	}
	var document struct {
		Statement []policyStatement `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(policy["policy"].StringValue()), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Statement) != 8 {
		t.Fatalf("got %d policy statements", len(document.Statement))
	}
	session := statementBySID(t, document.Statement, "SessionManagerHostChannels")
	wantSessionActions := []string{"ssm:UpdateInstanceInformation", "ssmmessages:CreateControlChannel", "ssmmessages:CreateDataChannel", "ssmmessages:OpenControlChannel", "ssmmessages:OpenDataChannel"}
	if strings.Join(session.Action, ",") != strings.Join(wantSessionActions, ",") || resourceText(session.Resource) != "*" {
		t.Fatalf("Session Manager policy is not the documented bounded host-channel set: %+v", session)
	}
	joined := strings.ToLower(string(policy["policy"].StringValue()))
	for _, forbidden := range []string{"s3:deleteobject", "iam:passrole", "iam:createrole", "kms:putkeypolicy", "kms:schedulekeydeletion", "ssm:getparameter", "ssm:sendcommand", "pulumi"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("host policy contains forbidden permission %q", forbidden)
		}
	}
	backup := statementBySID(t, document.Statement, "WriteInstallationBackups")
	if !strings.Contains(strings.TrimSpace(resourceText(backup.Resource)), "installations/01890f3e-7c00-7000-8000-000000000001/staging/*") {
		t.Fatalf("backup policy is not prefix scoped: %v", backup.Resource)
	}
	kms := statementBySID(t, document.Statement, "EncryptInstallationBackups")
	if kms.Condition["StringEquals"]["kms:ViaService"] != "s3.us-east-1.amazonaws.com" {
		t.Fatalf("KMS service condition missing: %v", kms.Condition)
	}
	if kms.Condition["StringLike"]["kms:EncryptionContext:aws:s3:arn"] != "arn:aws:s3:::amos-backup-example/installations/01890f3e-7c00-7000-8000-000000000001/staging/*" {
		t.Fatalf("KMS context condition not prefix-scoped: %v", kms.Condition)
	}
	secret := statementBySID(t, document.Statement, "ReadAssignedRuntimeSecrets")
	if secret.Condition["StringEquals"]["aws:ResourceTag/amos:environment"] != args.Environment || secret.Condition["StringEquals"]["aws:ResourceTag/amos:installation"] != args.InstallationID {
		t.Fatalf("secret access lacks owner tags: %v", secret.Condition)
	}
	secretKMS := statementBySID(t, document.Statement, "DecryptAssignedRuntimeSecrets")
	if conditionString(secretKMS, "StringEquals", "kms:ViaService") != "secretsmanager.us-east-1.amazonaws.com" {
		t.Fatalf("secret KMS service condition missing: %v", secretKMS.Condition)
	}
	if !strings.Contains(resourceText(secretKMS.Condition["StringEquals"]["kms:EncryptionContext:SecretARN"]), "amos/runtime-a") {
		t.Fatalf("secret KMS encryption context missing exact resource: %v", secretKMS.Condition)
	}
	assertOwnerTags(t, role["tags"].ObjectValue(), args.Environment, args.InstallationID)
}

func TestIdentityRejectsWildcardAndMissingAuthorityReferences(t *testing.T) {
	cases := []struct {
		name   string
		change func(*IdentityArgs)
	}{
		{"wildcard repository", func(a *IdentityArgs) { a.RepositoryARNs = []string{"arn:aws:ecr:us-east-1:123456789012:repository/*"} }},
		{"wildcard secret", func(a *IdentityArgs) {
			a.SecretARNs = []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:*"}
		}},
		{"missing backup key", func(a *IdentityArgs) { a.BackupKMSKeyARN = "" }},
		{"missing backup prefix", func(a *IdentityArgs) { a.BackupObjectPrefix = "" }},
		{"foreign backup prefix", func(a *IdentityArgs) { a.BackupObjectPrefix = "installations/other/staging" }},
		{"malformed repository", func(a *IdentityArgs) { a.RepositoryARNs = []string{"bad"} }},
		{"unsupported management profile", func(a *IdentityArgs) { a.ManagementProfile = "ssm-core" }},
		{"cross-account secret KMS key", func(a *IdentityArgs) { a.SecretKMSKeyARN = "arn:aws:kms:us-east-1:999999999999:key/secret-key" }},
		{"wrong-region secret KMS key", func(a *IdentityArgs) { a.SecretKMSKeyARN = "arn:aws:kms:us-west-2:123456789012:key/secret-key" }},
	}
	base := IdentityArgs{Name: "x", Environment: "staging", InstallationID: "01890f3e-7c00-7000-8000-000000000001", ManagementProfile: ManagementProfileSessionManager, Region: "us-east-1", LogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:amos-host", RepositoryARNs: []string{"arn:aws:ecr:us-east-1:123456789012:repository/amos"}, SecretARNs: []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:amos/runtime"}, BackupBucketARN: "arn:aws:s3:::amos-backup", BackupObjectPrefix: "installations/01890f3e-7c00-7000-8000-000000000001/staging", BackupKMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/key-id"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			tc.change(&args)
			if _, err := validateIdentityArgs(args); err == nil {
				t.Fatal("accepted invalid identity references")
			}
		})
	}
}

func TestIdentityRejectsStructurallyInvalidLiteralARNs(t *testing.T) {
	cases := []string{
		"arn:aws:ecr:us-east-1:not-an-account:repository/amos",
		"arn:aws:ecr:us-east-1:123456789012:repository/",
		"arn:aws:secretsmanager:us-east-1:123456789012:secret:",
		"arn:aws:kms:us-east-1:123456789012:key/",
		"arn:aws:logs:us-east-1:123456789012:log-group:",
	}
	for _, arn := range cases {
		t.Run(arn, func(t *testing.T) {
			if validARN(arn, strings.Split(arn, ":")[2], "us-east-1") {
				t.Fatalf("accepted structurally invalid ARN %q", arn)
			}
		})
	}
}

func TestIdentityRegionMustMatchARNPartition(t *testing.T) {
	if validAWSRegion("cn-north-1", "aws") {
		t.Fatal("accepted China region in aws partition")
	}
	if validAWSRegion("us-gov-west-1", "aws") {
		t.Fatal("accepted GovCloud region in aws partition")
	}
	if validAWSRegion("zz-east-1", "aws") {
		t.Fatal("accepted an unknown AWS region prefix")
	}
	if !validAWSRegion("cn-north-1", "aws-cn") || !validAWSRegion("us-gov-west-1", "aws-us-gov") {
		t.Fatal("rejected region for its matching partition")
	}
}

func TestIdentityDoesNotGrantCustomSecretKMSWithoutExplicitKey(t *testing.T) {
	args := IdentityArgs{Name: "x", Environment: "staging", InstallationID: "01890f3e-7c00-7000-8000-000000000001", ManagementProfile: ManagementProfileSessionManager, Region: "us-east-1", LogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:amos-host", RepositoryARNs: []string{"arn:aws:ecr:us-east-1:123456789012:repository/amos"}, SecretARNs: []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:amos/runtime"}, BackupBucketARN: "arn:aws:s3:::amos-backup", BackupObjectPrefix: "installations/01890f3e-7c00-7000-8000-000000000001/staging", BackupKMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/key-id"}
	prefix, err := validateIdentityArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hostPolicy(args, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Statement []policyStatement `json:"Statement"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, statement := range document.Statement {
		if statement.Sid == "DecryptAssignedRuntimeSecrets" {
			t.Fatal("granted custom-key decryption without an explicit secret key")
		}
	}
}

func conditionString(statement policyStatement, operator, key string) string {
	value, _ := statement.Condition[operator][key].(string)
	return value
}

func findResource(resources []recordedResource, token string) (resource.PropertyMap, bool) {
	for _, r := range resources {
		if r.token == token {
			return r.inputs, true
		}
	}
	return nil, false
}
func statementBySID(t *testing.T, statements []policyStatement, sid string) policyStatement {
	t.Helper()
	for _, statement := range statements {
		if statement.Sid == sid {
			return statement
		}
	}
	t.Fatalf("statement %q missing", sid)
	return policyStatement{}
}
func resourceText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
