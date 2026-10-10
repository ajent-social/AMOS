package state

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	aws "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	awsiam "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awskms "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/kms"
	awss3 "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// BootstrapArgs describes the separately managed Pulumi state foundation. The
// trusted principals are installation-owned IAM role ARNs, never application
// task or host identities.
type BootstrapArgs struct {
	Name                 string
	BucketName           string
	Environment          string
	InstallationID       string
	StatePrefix          string
	StateWriterPrincipal string
	RecoveryPrincipal    string
	KeyAdministrator     string
	Region               string
}

// Bootstrap exposes resource identifiers and role ARNs only; no state object,
// key material, or decrypted secret is returned or logged.
type Bootstrap struct {
	pulumi.ResourceState
	BucketName      pulumi.StringOutput `pulumi:"bucketName"`
	BucketARN       pulumi.StringOutput `pulumi:"bucketArn"`
	KeyARN          pulumi.StringOutput `pulumi:"keyArn"`
	SecretsKeyARN   pulumi.StringOutput `pulumi:"secretsKeyArn"`
	SecretsProvider pulumi.StringOutput `pulumi:"secretsProvider"`
	StateRoleARN    pulumi.StringOutput `pulumi:"stateRoleArn"`
	RecoveryRoleARN pulumi.StringOutput `pulumi:"recoveryRoleArn"`
	BackendURL      pulumi.StringOutput `pulumi:"backendUrl"`
}

// NewBootstrap creates a protected, versioned S3 state backend, an S3-only
// encryption key, and a separate context-scoped Pulumi secrets key. The
// SecretsProvider output supplies the awskms URL and required installation and
// environment encryption context. It never provisions cloud resources itself;
// Pulumi preview/apply remains an owner gate.
//
// Initialize this foundation with local state only. With Pulumi CLI 3.254 or
// later, log in to the target S3 BackendURL, then migrate each existing stack
// with `pulumi stack migrate file://~ <stack>` before changing its default
// backend. Use the SecretsProvider output when initializing or changing the
// stack's secrets provider; preserve its context query parameters. Verify the
// target stack and version history through the authenticated S3 backend; do not
// print state to logs or copy secret values. Pulumi's backend lock is
// authoritative for concurrent writers. Stale-lock recovery is manual: an
// owner first verifies no writer remains, inspects the affected stack, then
// removes only the exact stale backend lock object.
func NewBootstrap(ctx *pulumi.Context, name string, args BootstrapArgs, opts ...pulumi.ResourceOption) (*Bootstrap, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(args.Name) == "" {
		return nil, fmt.Errorf("bootstrap name is required")
	}
	if err := validateBootstrapArgs(args); err != nil {
		return nil, err
	}
	component := &Bootstrap{}
	if err := ctx.RegisterComponentResource("amos:infra:state:Bootstrap", name, component, opts...); err != nil {
		return nil, fmt.Errorf("register state bootstrap component: %w", err)
	}
	provider, err := aws.NewProvider(ctx, name+"-provider", &aws.ProviderArgs{Region: pulumi.String(args.Region)}, pulumi.Parent(component))
	if err != nil {
		return nil, fmt.Errorf("create region-pinned AWS provider: %w", err)
	}
	child := []pulumi.ResourceOption{pulumi.Parent(component), pulumi.Provider(provider), pulumi.Protect(true)}

	stateRole, err := awsiam.NewRole(ctx, name+"-state-writer", &awsiam.RoleArgs{
		AssumeRolePolicy: pulumi.String(roleTrustPolicy(args.StateWriterPrincipal)),
		Description:      pulumi.String("Owner-assumed access to the isolated Pulumi state backend"),
		Tags:             stateTags(args),
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create state-writer role: %w", err)
	}
	recoveryRole, err := awsiam.NewRole(ctx, name+"-recovery", &awsiam.RoleArgs{
		AssumeRolePolicy: pulumi.String(roleTrustPolicy(args.RecoveryPrincipal)),
		Description:      pulumi.String("Separately controlled state recovery access"),
		Tags:             stateTags(args),
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create recovery role: %w", err)
	}

	keyPolicy := pulumi.All(stateRole.Arn, recoveryRole.Arn).ApplyT(func(values []any) (string, error) {
		return keyPolicyJSON(args.KeyAdministrator, values[0].(string), values[1].(string), args.BucketName, args.Region)
	}).(pulumi.StringOutput)
	key, err := awskms.NewKey(ctx, name+"-key", &awskms.KeyArgs{
		Description:          pulumi.String("Encrypted infrastructure state and separately controlled recovery"),
		EnableKeyRotation:    pulumi.Bool(true),
		DeletionWindowInDays: pulumi.Int(30),
		Policy:               keyPolicy,
		Tags:                 stateTags(args),
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create state encryption key: %w", err)
	}
	secretsKeyPolicy := pulumi.All(stateRole.Arn, recoveryRole.Arn).ApplyT(func(values []any) (string, error) {
		return pulumiSecretsKeyPolicyJSON(args.KeyAdministrator, values[0].(string), values[1].(string), args.InstallationID, args.Environment)
	}).(pulumi.StringOutput)
	secretsKey, err := awskms.NewKey(ctx, name+"-pulumi-secrets-key", &awskms.KeyArgs{
		Description:          pulumi.String("Pulumi configuration secret encryption for this installation"),
		EnableKeyRotation:    pulumi.Bool(true),
		DeletionWindowInDays: pulumi.Int(30),
		Policy:               secretsKeyPolicy,
		Tags:                 stateTags(args),
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create Pulumi secrets key: %w", err)
	}
	secretsAliasName := "alias/amos/" + args.Environment + "/" + args.InstallationID + "/pulumi-secrets"
	if _, err = awskms.NewAlias(ctx, name+"-pulumi-secrets-key-alias", &awskms.AliasArgs{
		Name:        pulumi.String(secretsAliasName),
		TargetKeyId: secretsKey.KeyId,
	}, child...); err != nil {
		return nil, fmt.Errorf("create Pulumi secrets key alias: %w", err)
	}
	_, err = awskms.NewAlias(ctx, name+"-key-alias", &awskms.AliasArgs{
		Name:        pulumi.String("alias/amos/" + args.Environment + "/" + args.InstallationID + "/state"),
		TargetKeyId: key.KeyId,
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create state key alias: %w", err)
	}

	bucket, err := awss3.NewBucketV2(ctx, name+"-bucket", &awss3.BucketV2Args{
		Bucket:       pulumi.String(args.BucketName),
		ForceDestroy: pulumi.Bool(false),
		Tags:         stateTags(args),
	}, child...)
	if err != nil {
		return nil, fmt.Errorf("create state bucket: %w", err)
	}
	if _, err = awss3.NewBucketVersioningV2(ctx, name+"-versioning", &awss3.BucketVersioningV2Args{
		Bucket:                  bucket.ID().ToStringOutput(),
		VersioningConfiguration: &awss3.BucketVersioningV2VersioningConfigurationArgs{Status: pulumi.String("Enabled")},
	}, child...); err != nil {
		return nil, fmt.Errorf("enable state versioning: %w", err)
	}
	if _, err = awss3.NewBucketServerSideEncryptionConfigurationV2(ctx, name+"-encryption", &awss3.BucketServerSideEncryptionConfigurationV2Args{
		Bucket: bucket.ID().ToStringOutput(),
		Rules: awss3.BucketServerSideEncryptionConfigurationV2RuleArray{
			awss3.BucketServerSideEncryptionConfigurationV2RuleArgs{
				ApplyServerSideEncryptionByDefault: awss3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm:   pulumi.String("aws:kms"),
					KmsMasterKeyId: key.Arn,
				},
				BucketKeyEnabled: pulumi.Bool(true),
			},
		},
	}, child...); err != nil {
		return nil, fmt.Errorf("configure state bucket encryption: %w", err)
	}
	if _, err = awss3.NewBucketPublicAccessBlock(ctx, name+"-public-access-block", &awss3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID().ToStringOutput(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(true),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(true),
	}, child...); err != nil {
		return nil, fmt.Errorf("block public state-bucket access: %w", err)
	}
	if _, err = awss3.NewBucketPolicy(ctx, name+"-policy", &awss3.BucketPolicyArgs{
		Bucket: bucket.ID().ToStringOutput(),
		Policy: pulumi.All(bucket.Arn, stateRole.Arn, recoveryRole.Arn).ApplyT(func(values []any) (string, error) {
			return bucketPolicy(values[0].(string), values[1].(string), values[2].(string), args.StatePrefix)
		}).(pulumi.StringOutput),
	}, child...); err != nil {
		return nil, fmt.Errorf("restrict state bucket policy: %w", err)
	}
	statePolicy := pulumi.All(bucket.Arn, key.Arn, secretsKey.Arn).ApplyT(func(values []any) (string, error) {
		return writerPolicyJSON(values[0].(string), values[1].(string), values[2].(string), args.StatePrefix, args.BucketName, args.Region, args.InstallationID, args.Environment)
	}).(pulumi.StringOutput)
	if _, err = awsiam.NewRolePolicy(ctx, name+"-state-writer-policy", &awsiam.RolePolicyArgs{Role: stateRole.Name, Policy: statePolicy}, child...); err != nil {
		return nil, fmt.Errorf("attach state-writer policy: %w", err)
	}
	recoveryPolicy := pulumi.All(bucket.Arn, key.Arn, secretsKey.Arn).ApplyT(func(values []any) (string, error) {
		return recoveryPolicyJSON(values[0].(string), values[1].(string), values[2].(string), args.StatePrefix, args.BucketName, args.Region, args.InstallationID, args.Environment)
	}).(pulumi.StringOutput)
	if _, err = awsiam.NewRolePolicy(ctx, name+"-recovery-policy", &awsiam.RolePolicyArgs{Role: recoveryRole.Name, Policy: recoveryPolicy}, child...); err != nil {
		return nil, fmt.Errorf("attach recovery policy: %w", err)
	}

	component.BucketName = bucket.Bucket
	component.BucketARN = bucket.Arn
	component.KeyARN = key.Arn
	component.SecretsKeyARN = secretsKey.Arn
	component.SecretsProvider = pulumi.String(secretsProviderURL(secretsAliasName, args.Region, args.InstallationID, args.Environment)).ToStringOutput()
	component.StateRoleARN = stateRole.Arn
	component.RecoveryRoleARN = recoveryRole.Arn
	component.BackendURL = bucket.Bucket.ApplyT(func(bucketName string) string {
		return (&url.URL{Scheme: "s3", Host: bucketName, Path: "/" + args.StatePrefix, RawQuery: "region=" + url.QueryEscape(args.Region) + "&awssdk=v2"}).String()
	}).(pulumi.StringOutput)
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"bucketName": component.BucketName, "bucketArn": component.BucketARN,
		"keyArn": component.KeyARN, "secretsKeyArn": component.SecretsKeyARN,
		"secretsProvider": component.SecretsProvider, "stateRoleArn": component.StateRoleARN,
		"recoveryRoleArn": component.RecoveryRoleARN, "backendUrl": component.BackendURL,
	}); err != nil {
		return nil, fmt.Errorf("register state bootstrap outputs: %w", err)
	}
	return component, nil
}

func validateBootstrapArgs(args BootstrapArgs) error {
	if !validS3BucketName(args.BucketName) {
		return fmt.Errorf("state bucket name must be a valid DNS-compatible S3 name")
	}
	if strings.TrimSpace(args.StatePrefix) == "" {
		return fmt.Errorf("state prefix is required")
	}
	if !validStatePrefix(args.StatePrefix) {
		return fmt.Errorf("state prefix must be a canonical relative object prefix")
	}
	if !validAWSRegion(args.Region) {
		return fmt.Errorf("state backend region must be an explicit AWS region")
	}
	if args.Environment != "development" && args.Environment != "staging" && args.Environment != "production" {
		return fmt.Errorf("environment must be development, staging, or production")
	}
	if !validInstallationID(args.InstallationID) {
		return fmt.Errorf("installation ID must be a canonical lowercase UUIDv7")
	}
	partition := awsPartitionForRegion(args.Region)
	for name, arn := range map[string]string{"state writer principal": args.StateWriterPrincipal, "recovery principal": args.RecoveryPrincipal, "key administrator": args.KeyAdministrator} {
		if !validRoleARN(arn) || strings.Split(arn, ":")[1] != partition {
			return fmt.Errorf("%s must be an explicit IAM role ARN", name)
		}
	}
	if args.StateWriterPrincipal == args.RecoveryPrincipal {
		return fmt.Errorf("state writer and recovery principals must be distinct")
	}
	if args.KeyAdministrator == args.StateWriterPrincipal || args.KeyAdministrator == args.RecoveryPrincipal {
		return fmt.Errorf("key administrator must be distinct from state writer and recovery principals")
	}
	return nil
}

func validS3BucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 || !isASCIIAlphanumeric(name[0]) || !isASCIIAlphanumeric(name[len(name)-1]) || strings.Contains(name, "..") {
		return false
	}
	for _, ch := range name {
		if !isASCIIAlphanumeric(byte(ch)) && ch != '.' && ch != '-' {
			return false
		}
	}
	parts := strings.Split(name, ".")
	for _, part := range parts {
		if len(part) == 0 || !isASCIIAlphanumeric(part[0]) || !isASCIIAlphanumeric(part[len(part)-1]) {
			return false
		}
	}
	if len(parts) == 4 {
		ipv4 := true
		for _, part := range parts {
			if part == "" || len(part) > 3 {
				ipv4 = false
				break
			}
			for _, ch := range part {
				if ch < '0' || ch > '9' {
					ipv4 = false
					break
				}
			}
		}
		if ipv4 {
			return false
		}
	}
	return true
}

func isASCIIAlphanumeric(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}

func validAWSRegion(region string) bool {
	parts := strings.Split(region, "-")
	if len(parts) == 4 && parts[0] == "us" && parts[1] == "gov" {
		parts = []string{parts[0], parts[2], parts[3]}
	}
	if len(parts) != 3 || len(parts[0]) != 2 || parts[1] == "" || parts[2] == "" {
		return false
	}
	for _, ch := range parts[0] + parts[1] {
		if ch < 'a' || ch > 'z' {
			return false
		}
	}
	for _, ch := range parts[2] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func validRoleARN(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 6 || parts[0] != "arn" || (parts[1] != "aws" && parts[1] != "aws-us-gov" && parts[1] != "aws-cn") || parts[2] != "iam" || parts[3] != "" || len(parts[4]) != 12 || !strings.HasPrefix(parts[5], "role/") || len(parts[5]) <= len("role/") || strings.HasSuffix(parts[5], "/") || strings.Contains(parts[5], "//") {
		return false
	}
	for _, ch := range parts[4] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return !strings.ContainsAny(value, "*? \t\r\n")
}

func validStatePrefix(prefix string) bool {
	if prefix == "" || strings.Trim(prefix, "/") != prefix || strings.Contains(prefix, "..") || strings.Contains(prefix, "//") {
		return false
	}
	for _, ch := range prefix {
		valid := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '/' || ch == '-' || ch == '_'
		if !valid {
			return false
		}
	}
	return true
}

func validInstallationID(id string) bool {
	if len(id) != 36 || strings.ToLower(id) != id || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || id[14] != '7' {
		return false
	}
	if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
		return false
	}
	for i, ch := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		valid := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !valid {
			return false
		}
	}
	return true
}

func roleTrustPolicy(principal string) string {
	value, _ := marshalPolicy(map[string]any{"Version": "2012-10-17", "Statement": []map[string]any{{"Effect": "Allow", "Principal": map[string]any{"AWS": principal}, "Action": "sts:AssumeRole"}}})
	return value
}

func keyPolicyJSON(admin, stateRole, recoveryRole, bucket, region string) (string, error) {
	bucketARN, viaService := s3KeyContext(bucket, region)
	condition := map[string]any{"StringEquals": map[string]string{"kms:ViaService": viaService, "kms:EncryptionContext:aws:s3:arn": bucketARN}}
	return marshalPolicy(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": map[string]any{"AWS": admin}, "Action": []string{"kms:DescribeKey", "kms:EnableKey", "kms:DisableKey", "kms:PutKeyPolicy", "kms:ScheduleKeyDeletion", "kms:CancelKeyDeletion", "kms:EnableKeyRotation", "kms:DisableKeyRotation", "kms:ListResourceTags", "kms:TagResource", "kms:UntagResource"}, "Resource": "*"},
			{"Sid": "StateWriterS3CryptographicUse", "Effect": "Allow", "Principal": map[string]any{"AWS": stateRole}, "Action": []string{"kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey"}, "Resource": "*", "Condition": condition},
			{"Sid": "StateWriterDescribeKey", "Effect": "Allow", "Principal": map[string]any{"AWS": stateRole}, "Action": "kms:DescribeKey", "Resource": "*"},
			{"Sid": "RecoveryS3DecryptOnly", "Effect": "Allow", "Principal": map[string]any{"AWS": recoveryRole}, "Action": "kms:Decrypt", "Resource": "*", "Condition": condition},
			{"Sid": "RecoveryDescribeKey", "Effect": "Allow", "Principal": map[string]any{"AWS": recoveryRole}, "Action": "kms:DescribeKey", "Resource": "*"},
		},
	})
}

func s3KeyContext(bucket, region string) (bucketARN, viaService string) {
	partition := awsPartitionForRegion(region)
	domain := "amazonaws.com"
	if partition == "aws-cn" {
		domain += ".cn"
	}
	return "arn:" + partition + ":s3:::" + bucket, "s3." + region + "." + domain
}

func awsPartitionForRegion(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return "aws-cn"
	}
	if strings.HasPrefix(region, "us-gov-") {
		return "aws-us-gov"
	}
	return "aws"
}

func pulumiSecretsKeyPolicyJSON(admin, stateRole, recoveryRole, installationID, environment string) (string, error) {
	condition := pulumiSecretsContext(installationID, environment)
	return marshalPolicy(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": map[string]any{"AWS": admin}, "Action": []string{"kms:DescribeKey", "kms:EnableKey", "kms:DisableKey", "kms:PutKeyPolicy", "kms:ScheduleKeyDeletion", "kms:CancelKeyDeletion", "kms:EnableKeyRotation", "kms:DisableKeyRotation", "kms:ListResourceTags", "kms:TagResource", "kms:UntagResource"}, "Resource": "*"},
			{"Sid": "StateWriterPulumiSecrets", "Effect": "Allow", "Principal": map[string]any{"AWS": stateRole}, "Action": []string{"kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey"}, "Resource": "*", "Condition": condition},
			{"Sid": "StateWriterDescribePulumiSecretsKey", "Effect": "Allow", "Principal": map[string]any{"AWS": stateRole}, "Action": "kms:DescribeKey", "Resource": "*"},
			{"Sid": "RecoveryPulumiSecretsDecryptOnly", "Effect": "Allow", "Principal": map[string]any{"AWS": recoveryRole}, "Action": "kms:Decrypt", "Resource": "*", "Condition": condition},
			{"Sid": "RecoveryDescribePulumiSecretsKey", "Effect": "Allow", "Principal": map[string]any{"AWS": recoveryRole}, "Action": "kms:DescribeKey", "Resource": "*"},
		},
	})
}
func pulumiSecretsContext(installationID, environment string) map[string]any {
	return map[string]any{"StringEquals": map[string]string{"kms:EncryptionContext:amosInstallationID": installationID, "kms:EncryptionContext:amosEnvironment": environment}}
}
func secretsProviderURL(alias, region, installationID, environment string) string {
	query := url.Values{}
	query.Set("region", region)
	query.Set("awssdk", "v2")
	query.Set("context_amosInstallationID", installationID)
	query.Set("context_amosEnvironment", environment)
	return (&url.URL{Scheme: "awskms", Host: "alias", Path: "/" + strings.TrimPrefix(alias, "alias/"), RawQuery: query.Encode()}).String()
}
func writerPolicyJSON(bucketARN, keyARN, secretsKeyARN, prefix, bucket, region, installationID, environment string) (string, error) {
	objectARN := bucketARN + "/" + prefix + "/*"
	lockARN := bucketARN + "/" + prefix + "/.pulumi/locks/*"
	s3BucketARN, viaService := s3KeyContext(bucket, region)
	keyCondition := map[string]any{"StringEquals": map[string]string{"kms:ViaService": viaService, "kms:EncryptionContext:aws:s3:arn": s3BucketARN}}
	return marshalPolicy(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Sid": "ListOwnStatePrefix", "Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": bucketARN, "Condition": map[string]any{"StringLike": map[string]any{"s3:prefix": []string{prefix, prefix + "/*"}}}},
			{"Sid": "ReadWriteOwnState", "Effect": "Allow", "Action": []string{"s3:GetObject", "s3:PutObject"}, "Resource": objectARN},
			{"Sid": "ManageBackendLocks", "Effect": "Allow", "Action": []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}, "Resource": lockARN},
			{"Sid": "UseStateKeyThroughS3", "Effect": "Allow", "Action": []string{"kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey"}, "Resource": keyARN, "Condition": keyCondition},
			{"Sid": "DescribeStateKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": keyARN},
			{"Sid": "UsePulumiSecretsKey", "Effect": "Allow", "Action": []string{"kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey"}, "Resource": secretsKeyARN, "Condition": pulumiSecretsContext(installationID, environment)},
			{"Sid": "DescribePulumiSecretsKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": secretsKeyARN},
		},
	})
}
func recoveryPolicyJSON(bucketARN, keyARN, secretsKeyARN, prefix, bucket, region, installationID, environment string) (string, error) {
	s3BucketARN, viaService := s3KeyContext(bucket, region)
	keyCondition := map[string]any{"StringEquals": map[string]string{"kms:ViaService": viaService, "kms:EncryptionContext:aws:s3:arn": s3BucketARN}}
	return marshalPolicy(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Sid": "ListStateForRecovery", "Effect": "Allow", "Action": []string{"s3:ListBucket", "s3:ListBucketVersions"}, "Resource": bucketARN, "Condition": map[string]any{"StringLike": map[string]any{"s3:prefix": []string{prefix, prefix + "/*"}}}},
			{"Sid": "ReadStateForRecovery", "Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion"}, "Resource": bucketARN + "/" + prefix + "/*"},
			{"Sid": "DecryptStateThroughS3", "Effect": "Allow", "Action": []string{"kms:Decrypt"}, "Resource": keyARN, "Condition": keyCondition},
			{"Sid": "DescribeStateKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": keyARN},
			{"Sid": "DecryptPulumiSecrets", "Effect": "Allow", "Action": "kms:Decrypt", "Resource": secretsKeyARN, "Condition": pulumiSecretsContext(installationID, environment)},
			{"Sid": "DescribePulumiSecretsKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": secretsKeyARN},
		},
	})
}
func bucketPolicy(bucketARN, stateRoleARN, recoveryRoleARN, prefix string) (string, error) {
	allowedRoles := []string{stateRoleARN, recoveryRoleARN}
	outsideRoles := map[string]any{"ArnNotEquals": map[string]any{"aws:PrincipalArn": allowedRoles}}
	return marshalPolicy(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*", "Resource": []string{bucketARN, bucketARN + "/*"}, "Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "false"}}},
			{"Sid": "DenyListingByOtherPrincipals", "Effect": "Deny", "Principal": "*", "Action": []string{"s3:ListBucket", "s3:ListBucketVersions"}, "Resource": bucketARN, "Condition": outsideRoles},
			{"Sid": "DenyStateObjectsByOtherPrincipals", "Effect": "Deny", "Principal": "*", "Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:PutObject", "s3:DeleteObject", "s3:DeleteObjectVersion"}, "Resource": bucketARN + "/" + prefix + "/*", "Condition": outsideRoles},
		},
	})
}
func stateTags(args BootstrapArgs) pulumi.StringMap {
	return pulumi.StringMap{"Name": pulumi.String(args.Name), "amos:profile": pulumi.String("state"), "amos:environment": pulumi.String(args.Environment), "amos:installation": pulumi.String(args.InstallationID)}
}

func marshalPolicy(value any) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}
