package aws

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	awsiam "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// IdentityArgs restricts the host to one installation's runtime inputs and
// backup prefix. ARNs are explicit owner-selected references, never secrets.
type IdentityArgs struct {
	Name               string
	Environment        string
	InstallationID     string
	ManagementProfile  string // only ManagementProfileSessionManager is supported
	Region             string
	LogGroupARN        string   // exact pre-created host log group
	RepositoryARNs     []string // exact ECR repositories from which this host may pull
	SecretARNs         []string // exact tagged runtime secrets for this installation
	BackupBucketARN    string   // general-purpose bucket ARN; syntax only, existence/namespace/ownership are provider gates
	BackupObjectPrefix string   // must equal installations/<InstallationID>/<Environment>
	BackupKMSKeyARN    string   // exact customer-managed key for installation backups
	SecretKMSKeyARN    string   // optional exact customer-managed key for runtime secrets
}

// Identity is the EC2 instance role/profile. It has no deployment, state,
// role-assumption, backup-delete, or KMS-administration permissions.
type Identity struct {
	pulumi.ResourceState
	RoleARN         pulumi.StringOutput `pulumi:"roleArn"`
	InstanceProfile pulumi.StringOutput `pulumi:"instanceProfile"`
}

// NewIdentity creates the tagged EC2 host role, least-privilege policy and profile.
func NewIdentity(ctx *pulumi.Context, name string, args IdentityArgs, opts ...pulumi.ResourceOption) (*Identity, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(args.Name) == "" {
		return nil, fmt.Errorf("identity name is required")
	}
	if err := validateOwnerScope(args.Environment, args.InstallationID); err != nil {
		return nil, err
	}
	if args.ManagementProfile != ManagementProfileSessionManager {
		return nil, fmt.Errorf("unsupported host management profile")
	}
	prefix, err := validateIdentityArgs(args)
	if err != nil {
		return nil, err
	}

	component := &Identity{}
	if err := ctx.RegisterComponentResource("amos:infra:aws:Identity", name, component, opts...); err != nil {
		return nil, fmt.Errorf("register identity component: %w", err)
	}
	childOpts := []pulumi.ResourceOption{pulumi.Parent(component)}
	trust, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []any{map[string]any{
			"Effect": "Allow", "Principal": map[string]string{"Service": "ec2.amazonaws.com"},
			"Action": "sts:AssumeRole",
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode EC2 trust policy: %w", err)
	}
	role, err := awsiam.NewRole(ctx, name+"-host", &awsiam.RoleArgs{
		AssumeRolePolicy: pulumi.String(string(trust)),
		Tags:             ownerTags(args.Name+"-host", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create host role: %w", err)
	}

	policyBytes, err := hostPolicy(args, prefix)
	if err != nil {
		return nil, err
	}
	if _, err := awsiam.NewRolePolicy(ctx, name+"-runtime-access", &awsiam.RolePolicyArgs{
		Role: role.Name, Policy: pulumi.String(string(policyBytes)),
	}, childOpts...); err != nil {
		return nil, fmt.Errorf("create host runtime policy: %w", err)
	}

	profile, err := awsiam.NewInstanceProfile(ctx, name+"-profile", &awsiam.InstanceProfileArgs{
		Role: role.Name, Tags: ownerTags(args.Name+"-profile", args.Environment, args.InstallationID),
	}, childOpts...)
	if err != nil {
		return nil, fmt.Errorf("create host instance profile: %w", err)
	}
	component.RoleARN = role.Arn
	component.InstanceProfile = profile.Name
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"roleArn": component.RoleARN, "instanceProfile": component.InstanceProfile}); err != nil {
		return nil, fmt.Errorf("register identity outputs: %w", err)
	}
	return component, nil
}

func validateIdentityArgs(args IdentityArgs) (string, error) {
	if err := validateOwnerScope(args.Environment, args.InstallationID); err != nil {
		return "", err
	}
	if args.Region == "" || args.LogGroupARN == "" || args.BackupBucketARN == "" || args.BackupObjectPrefix == "" || args.BackupKMSKeyARN == "" || args.ManagementProfile != ManagementProfileSessionManager {
		return "", fmt.Errorf("region, log group, backup bucket, backup prefix, backup KMS key, and supported management profile are required")
	}
	if len(args.RepositoryARNs) == 0 {
		return "", fmt.Errorf("at least one image repository ARN is required")
	}
	if len(args.SecretARNs) == 0 {
		return "", fmt.Errorf("at least one runtime secret ARN is required")
	}
	if !strings.HasPrefix(args.BackupBucketARN, "arn:") || !strings.HasPrefix(args.BackupKMSKeyARN, "arn:") {
		return "", fmt.Errorf("backup bucket and KMS references must be ARNs")
	}
	if !validARN(args.BackupBucketARN, "s3", "") || !validARN(args.BackupKMSKeyARN, "kms", args.Region) || !validARN(args.LogGroupARN, "logs", args.Region) {
		return "", fmt.Errorf("log group, backup bucket and KMS references must be valid AWS ARNs for the selected region")
	}
	if !strings.Contains(args.BackupKMSKeyARN, ":key/") || !strings.Contains(args.LogGroupARN, ":log-group:") {
		return "", fmt.Errorf("KMS key and CloudWatch log group must use resource ARNs")
	}
	if args.SecretKMSKeyARN != "" && (!validARN(args.SecretKMSKeyARN, "kms", args.Region) || !strings.Contains(args.SecretKMSKeyARN, ":key/")) {
		return "", fmt.Errorf("secret KMS reference must be a customer-managed key ARN in the selected region")
	}
	logResource := strings.TrimPrefix(strings.SplitN(args.LogGroupARN, ":", 6)[5], "log-group:")
	if logResource == "" || strings.Contains(logResource, ":") {
		return "", fmt.Errorf("log group reference must identify the group, not a stream")
	}
	for _, arn := range args.RepositoryARNs {
		if !validARN(arn, "ecr", args.Region) || !strings.Contains(arn, ":repository/") {
			return "", fmt.Errorf("image reference must be a literal ECR repository ARN")
		}
	}
	for _, arn := range args.SecretARNs {
		if !validARN(arn, "secretsmanager", args.Region) || !strings.Contains(arn, ":secret:") {
			return "", fmt.Errorf("runtime secret reference must be a literal Secrets Manager ARN")
		}
	}
	kmsParts := strings.SplitN(args.BackupKMSKeyARN, ":", 6)
	if !validAWSRegion(args.Region, kmsParts[1]) {
		return "", fmt.Errorf("region is invalid for the selected AWS partition")
	}
	bucketParts := strings.SplitN(args.BackupBucketARN, ":", 6)
	if bucketParts[1] != kmsParts[1] {
		return "", fmt.Errorf("backup bucket and KMS key must share an AWS partition")
	}
	allARNS := append(append([]string{args.LogGroupARN}, args.RepositoryARNs...), args.SecretARNs...)
	if args.SecretKMSKeyARN != "" {
		allARNS = append(allARNS, args.SecretKMSKeyARN)
	}
	for _, arn := range allARNS {
		parts := strings.SplitN(arn, ":", 6)
		if parts[1] != kmsParts[1] || parts[4] != kmsParts[4] {
			return "", fmt.Errorf("host resource ARNs must share the selected KMS account and partition")
		}
	}
	prefix := strings.Trim(args.BackupObjectPrefix, "/")
	if prefix == "" || strings.Contains(prefix, "..") || strings.ContainsAny(prefix, "*?#") {
		return "", fmt.Errorf("backup object prefix must be a non-empty literal path")
	}
	wantPrefix := "installations/" + args.InstallationID + "/" + args.Environment
	if prefix != wantPrefix {
		return "", fmt.Errorf("backup object prefix must bind to the selected installation and environment")
	}
	return prefix, nil
}

func validARN(value, service, region string) bool {
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || !supportedPartition(parts[1]) || parts[2] != service || parts[5] == "" || strings.ContainsAny(value, "*?#") {
		return false
	}
	if region != "" && parts[3] != region {
		return false
	}
	if service == "s3" {
		// This validates only general-purpose bucket name syntax. Bucket
		// existence, namespace type, ownership and region are provider gates.
		return supportedPartition(parts[1]) && parts[3] == "" && parts[4] == "" && validGeneralPurposeBucketName(parts[5])
	}
	if parts[3] == "" || !accountIDPattern.MatchString(parts[4]) {
		return false
	}
	switch service {
	case "ecr":
		return ecrRepositoryResourcePattern.MatchString(parts[5])
	case "secretsmanager":
		return secretResourcePattern.MatchString(parts[5])
	case "kms":
		return kmsKeyResourcePattern.MatchString(parts[5])
	case "logs":
		return strings.HasPrefix(parts[5], "log-group:") && len(strings.TrimPrefix(parts[5], "log-group:")) > 0
	default:
		return parts[5] != ""
	}
}

var s3BucketNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{1,61}[a-z0-9])$`)
var s3AccountRegionalBucketPattern = regexp.MustCompile(`^.+-[0-9]{12}-[a-z]+(?:-[a-z]+)+-[0-9]+-an$`)

func validGeneralPurposeBucketName(name string) bool {
	if !s3BucketNamePattern.MatchString(name) || strings.Contains(name, "..") || net.ParseIP(name) != nil {
		return false
	}
	if strings.HasSuffix(name, "-an") && !s3AccountRegionalBucketPattern.MatchString(name) {
		return false
	}
	for _, prefix := range []string{"xn--", "sthree-", "amzn-s3-demo-"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	for _, suffix := range []string{"-s3alias", "--ol-s3", ".mrap", "--x-s3", "--table-s3"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

var accountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)
var ecrRepositoryResourcePattern = regexp.MustCompile(`^repository/[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
var secretResourcePattern = regexp.MustCompile(`^secret:[A-Za-z0-9/_+=.@!-]+$`)
var kmsKeyResourcePattern = regexp.MustCompile(`^key/[A-Za-z0-9-]{1,128}$`)

func hostPolicy(args IdentityArgs, prefix string) ([]byte, error) {
	objectARN := strings.TrimSuffix(args.BackupBucketARN, "/") + "/" + prefix + "/*"
	endpoint, err := s3Endpoint(args.BackupBucketARN, args.Region)
	if err != nil {
		return nil, err
	}
	statements := []map[string]any{
		// Mirrors AWS's custom Session Manager host-channel policy. Broader SSM
		// permissions (Parameter Store, Run Command, inventory and patching) are
		// deliberately excluded; unsupported management profiles fail closed.
		{"Sid": "SessionManagerHostChannels", "Effect": "Allow", "Action": []string{"ssm:UpdateInstanceInformation", "ssmmessages:CreateControlChannel", "ssmmessages:CreateDataChannel", "ssmmessages:OpenControlChannel", "ssmmessages:OpenDataChannel"}, "Resource": "*"},
		{"Sid": "AuthenticateImagePull", "Effect": "Allow", "Action": []string{"ecr:GetAuthorizationToken"}, "Resource": "*"},
		{"Sid": "PullSelectedImages", "Effect": "Allow", "Action": []string{"ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"}, "Resource": args.RepositoryARNs},
		{"Sid": "ReadAssignedRuntimeSecrets", "Effect": "Allow", "Action": []string{"secretsmanager:DescribeSecret", "secretsmanager:GetSecretValue"}, "Resource": args.SecretARNs, "Condition": map[string]any{"StringEquals": map[string]string{"aws:ResourceTag/amos:environment": args.Environment, "aws:ResourceTag/amos:installation": args.InstallationID}}},
		{"Sid": "WriteInstallationLogs", "Effect": "Allow", "Action": []string{"logs:CreateLogStream", "logs:PutLogEvents"}, "Resource": strings.TrimSuffix(args.LogGroupARN, ":") + ":log-stream:*"},
		{"Sid": "WriteInstallationBackups", "Effect": "Allow", "Action": []string{"s3:AbortMultipartUpload", "s3:PutObject", "s3:ListMultipartUploadParts"}, "Resource": objectARN},
		// The storage adapter must keep S3 Bucket Keys disabled so KMS sees the
		// per-object ARN context and this installation/environment prefix applies.
		{"Sid": "EncryptInstallationBackups", "Effect": "Allow", "Action": []string{"kms:Decrypt", "kms:GenerateDataKey"}, "Resource": args.BackupKMSKeyARN,
			"Condition": map[string]any{"StringEquals": map[string]string{"kms:ViaService": endpoint}, "StringLike": map[string]string{"kms:EncryptionContext:aws:s3:arn": objectARN}}},
	}
	if args.SecretKMSKeyARN != "" {
		secretEndpoint, err := secretsManagerEndpoint(args.SecretKMSKeyARN, args.Region)
		if err != nil {
			return nil, err
		}
		statements = append(statements, map[string]any{"Sid": "DecryptAssignedRuntimeSecrets", "Effect": "Allow", "Action": []string{"kms:Decrypt"}, "Resource": args.SecretKMSKeyARN,
			"Condition": map[string]any{"StringEquals": map[string]any{"kms:ViaService": secretEndpoint, "kms:EncryptionContext:SecretARN": args.SecretARNs}}})
	}
	encoded, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": statements})
	if err != nil {
		return nil, err
	}
	if len(encoded) > 10240 {
		return nil, fmt.Errorf("host inline IAM policy exceeds the AWS role policy size limit")
	}
	return encoded, nil
}

func supportedPartition(partition string) bool {
	return partition == "aws" || partition == "aws-us-gov" || partition == "aws-cn"
}

func secretsManagerEndpoint(kmsARN, region string) (string, error) {
	partition := strings.SplitN(kmsARN, ":", 3)[1]
	switch partition {
	case "aws", "aws-us-gov":
		return "secretsmanager." + region + ".amazonaws.com", nil
	case "aws-cn":
		return "secretsmanager." + region + ".amazonaws.com.cn", nil
	default:
		return "", fmt.Errorf("unsupported AWS partition for Secrets Manager KMS service condition")
	}
}

func s3Endpoint(bucketARN, region string) (string, error) {
	partition := strings.SplitN(bucketARN, ":", 3)[1]
	switch partition {
	case "aws", "aws-us-gov":
		return "s3." + region + ".amazonaws.com", nil
	case "aws-cn":
		return "s3." + region + ".amazonaws.com.cn", nil
	default:
		return "", fmt.Errorf("unsupported AWS partition for S3 KMS service condition")
	}
}
