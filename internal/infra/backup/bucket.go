package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	ErrInvalidBucketArgs = errors.New("invalid backup bucket configuration")
	backupRoleARNPattern = regexp.MustCompile(`^arn:(aws|aws-us-gov|aws-cn):iam::([0-9]{12}):role/(?:[A-Za-z0-9+=,.@_/-]+)$`)
	backupKMSARNPattern  = regexp.MustCompile(`^arn:(aws|aws-us-gov|aws-cn):kms:([a-z0-9-]+):([0-9]{12}):key/[A-Za-z0-9-]+$`)
)

// BucketArgs requires an owner-selected retention period. This component
// defines resources only; callers still authorize provider configuration and
// deployment separately.
type BucketArgs struct {
	Name           string
	Prefix         string
	KMSKeyARN      string
	RuntimeRoleARN string
	RestoreRoleARN string
	RetentionDays  int
	NoncurrentDays int
}

type Bucket struct {
	pulumi.ResourceState
	Name pulumi.StringOutput
	ARN  pulumi.StringOutput
}

func NewBucket(ctx *pulumi.Context, name string, args BucketArgs, opts ...pulumi.ResourceOption) (*Bucket, error) {
	if ctx == nil || strings.TrimSpace(name) == "" {
		return nil, ErrInvalidBucketArgs
	}
	if err := validateBucketArgs(args); err != nil {
		return nil, err
	}
	component := &Bucket{}
	if err := ctx.RegisterComponentResource("amos:infra:backup:Bucket", name, component, opts...); err != nil {
		return nil, fmt.Errorf("register backup bucket component: %w", err)
	}
	childOpts := []pulumi.ResourceOption{pulumi.Parent(component)}
	bucket, err := s3.NewBucket(ctx, name+"-objects", &s3.BucketArgs{
		ForceDestroy: pulumi.Bool(false),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create backup bucket: %w", err)
	}
	if _, err := s3.NewBucketPublicAccessBlock(ctx, name+"-public-access", &s3.BucketPublicAccessBlockArgs{
		Bucket: bucket.ID(), BlockPublicAcls: pulumi.Bool(true), BlockPublicPolicy: pulumi.Bool(true),
		IgnorePublicAcls: pulumi.Bool(true), RestrictPublicBuckets: pulumi.Bool(true),
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("block public backup access: %w", err)
	}
	if _, err := s3.NewBucketOwnershipControls(ctx, name+"-ownership", &s3.BucketOwnershipControlsArgs{
		Bucket: bucket.ID(),
		Rule:   &s3.BucketOwnershipControlsRuleArgs{ObjectOwnership: pulumi.String("BucketOwnerEnforced")},
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("enforce bucket ownership: %w", err)
	}
	if _, err := s3.NewBucketVersioningV2(ctx, name+"-versioning", &s3.BucketVersioningV2Args{
		Bucket:                  bucket.ID(),
		VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{Status: pulumi.String("Enabled")},
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("enable backup object versioning: %w", err)
	}
	if _, err := s3.NewBucketServerSideEncryptionConfigurationV2(ctx, name+"-encryption", &s3.BucketServerSideEncryptionConfigurationV2Args{
		Bucket: bucket.ID(),
		Rules: s3.BucketServerSideEncryptionConfigurationV2RuleArray{
			&s3.BucketServerSideEncryptionConfigurationV2RuleArgs{
				BucketKeyEnabled: pulumi.Bool(false),
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("aws:kms"), KmsMasterKeyId: pulumi.String(args.KMSKeyARN),
				},
			},
		},
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("configure backup encryption: %w", err)
	}
	lifecycleRule := &s3.BucketLifecycleConfigurationV2RuleArgs{
		Id:         pulumi.String("backup-retention"),
		Status:     pulumi.String("Enabled"),
		Filter:     &s3.BucketLifecycleConfigurationV2RuleFilterArgs{Prefix: pulumi.String(args.Prefix + "/")},
		Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{Days: pulumi.IntPtr(args.RetentionDays)},
		NoncurrentVersionExpiration: &s3.BucketLifecycleConfigurationV2RuleNoncurrentVersionExpirationArgs{
			NoncurrentDays: pulumi.Int(args.NoncurrentDays),
		},
		AbortIncompleteMultipartUpload: &s3.BucketLifecycleConfigurationV2RuleAbortIncompleteMultipartUploadArgs{
			DaysAfterInitiation: pulumi.IntPtr(7),
		},
	}
	if _, err := s3.NewBucketLifecycleConfigurationV2(ctx, name+"-retention", &s3.BucketLifecycleConfigurationV2Args{
		Bucket: bucket.ID(), Rules: s3.BucketLifecycleConfigurationV2RuleArray{lifecycleRule},
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("configure backup retention: %w", err)
	}
	if err := attachBackupRoles(ctx, name, args, bucket, childOpts); err != nil {
		return nil, err
	}
	if _, err := s3.NewBucketPolicy(ctx, name+"-policy", &s3.BucketPolicyArgs{
		Bucket: bucket.ID(),
		Policy: bucket.Arn.ApplyT(func(bucketARN string) (string, error) {
			return bucketPolicyJSON(bucketARN, bucketARN, args)
		}).(pulumi.StringOutput),
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("configure backup bucket policy: %w", err)
	}
	component.Name = bucket.Bucket
	component.ARN = bucket.Arn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"name": component.Name, "arn": component.ARN}); err != nil {
		return nil, fmt.Errorf("register backup bucket outputs: %w", err)
	}
	return component, nil
}

func validateBucketArgs(args BucketArgs) error {
	kms := backupKMSARNPattern.FindStringSubmatch(args.KMSKeyARN)
	runtimeRole := backupRoleARNPattern.FindStringSubmatch(args.RuntimeRoleARN)
	restoreRole := backupRoleARNPattern.FindStringSubmatch(args.RestoreRoleARN)
	if strings.TrimSpace(args.Name) == "" || !validPrefix(args.Prefix) || kms == nil ||
		runtimeRole == nil || restoreRole == nil || args.RuntimeRoleARN == args.RestoreRoleARN ||
		kms[1] != runtimeRole[1] || kms[1] != restoreRole[1] || kms[3] != runtimeRole[2] || kms[3] != restoreRole[2] ||
		args.RetentionDays < 1 || args.NoncurrentDays < 1 {
		return ErrInvalidBucketArgs
	}
	return nil
}

func validPrefix(prefix string) bool {
	if prefix == "" || strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "*?#") {
		return false
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func attachBackupRoles(ctx *pulumi.Context, name string, args BucketArgs, bucket *s3.Bucket, opts []pulumi.ResourceOption) error {
	bucketArn := bucket.Arn
	objectArn := bucket.Arn.ApplyT(func(arn string) string { return arn + "/" + args.Prefix + "/*" }).(pulumi.StringOutput)
	runtimePolicy := pulumi.All(objectArn).ApplyT(func(values []interface{}) (string, error) {
		objectARN := values[0].(string)
		encoded, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []any{
			map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"}, "Resource": objectARN},
			map[string]any{"Effect": "Allow", "Action": []string{"kms:GenerateDataKey"}, "Resource": args.KMSKeyARN,
				"Condition": map[string]any{
					"StringEquals": map[string]string{"kms:ViaService": s3Endpoint(args.KMSKeyARN)},
					"StringLike":   map[string]string{"kms:EncryptionContext:aws:s3:arn": objectARN},
				}},
		}})
		return string(encoded), err
	}).(pulumi.StringOutput)
	if _, err := iam.NewRolePolicy(ctx, name+"-runtime-write", &iam.RolePolicyArgs{
		Role: pulumi.String(roleName(args.RuntimeRoleARN)), Policy: runtimePolicy,
	}, opts...); err != nil {
		return fmt.Errorf("scope runtime backup writes: %w", err)
	}
	restorePolicy := pulumi.All(bucketArn, objectArn).ApplyT(func(values []interface{}) (string, error) {
		bucketARN := values[0].(string)
		objectARN := values[1].(string)
		encoded, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []any{
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion"}, "Resource": objectARN},
			map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": bucketARN, "Condition": map[string]any{"StringLike": map[string]any{"s3:prefix": []string{args.Prefix, args.Prefix + "/*"}}}},
			map[string]any{"Effect": "Allow", "Action": []string{"kms:Decrypt"}, "Resource": args.KMSKeyARN, "Condition": map[string]any{"StringEquals": map[string]string{"kms:ViaService": s3Endpoint(args.KMSKeyARN)}, "StringLike": map[string]string{"kms:EncryptionContext:aws:s3:arn": objectARN}}},
		}})
		return string(encoded), err
	}).(pulumi.StringOutput)
	if _, err := iam.NewRolePolicy(ctx, name+"-restore-read", &iam.RolePolicyArgs{
		Role: pulumi.String(roleName(args.RestoreRoleARN)), Policy: restorePolicy,
	}, opts...); err != nil {
		return fmt.Errorf("scope restore backup reads: %w", err)
	}
	return nil
}

func bucketPolicyJSON(bucketARN, bucketName string, args BucketArgs) (string, error) {
	objectARN := bucketARN + "/" + args.Prefix + "/*"
	doc := map[string]any{
		"Version": "2012-10-17",
		"Statement": []any{
			map[string]any{"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*",
				"Resource": []string{bucketARN, objectARN}, "Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "false"}}},
			map[string]any{"Sid": "RequireKMSEncryption", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": objectARN,
				"Condition": map[string]any{"StringNotEquals": map[string]string{"s3:x-amz-server-side-encryption": "aws:kms"}}},
			map[string]any{"Sid": "RequireSelectedKMSKey", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": objectARN,
				"Condition": map[string]any{"StringNotEquals": map[string]string{"s3:x-amz-server-side-encryption-aws-kms-key-id": args.KMSKeyARN}}},
			map[string]any{"Sid": "RuntimeCannotReadOrDelete", "Effect": "Deny", "Principal": map[string]string{"AWS": args.RuntimeRoleARN},
				"Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:DeleteObject", "s3:DeleteObjectVersion"}, "Resource": objectARN},
		},
	}
	if bucketName == "" {
		return "", ErrInvalidBucketArgs
	}
	data, err := json.Marshal(doc)
	return string(data), err
}

func s3Endpoint(kmsARN string) string {
	parts := strings.Split(kmsARN, ":")
	if len(parts) < 4 {
		return ""
	}
	suffix := "amazonaws.com"
	if parts[1] == "aws-cn" {
		suffix = "amazonaws.com.cn"
	}
	return "s3." + parts[3] + "." + suffix
}

func roleName(arn string) string {
	parts := strings.SplitN(arn, ":role/", 2)
	if len(parts) != 2 {
		return ""
	}
	name := strings.Split(parts[1], "/")
	return name[len(name)-1]
}
