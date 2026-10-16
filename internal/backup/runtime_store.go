package backup

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"
)

var ErrRuntimeStore = errors.New("backup runtime identity or storage unavailable")

// runtimeSnapshot never refreshes or re-resolves the verified credential set.
type runtimeSnapshot struct{ credentials aws.Credentials }

func (p runtimeSnapshot) Retrieve(ctx context.Context) (aws.Credentials, error) {
	if ctx == nil || ctx.Err() != nil || !p.credentials.Expires.After(time.Now()) {
		return aws.Credentials{}, ErrRuntimeStore
	}
	return p.credentials, nil
}

type runtimeWriter struct {
	config   ScheduleAWSConfig
	expires  time.Time
	client   *s3.Client
	maxBytes int64
}

func (w *runtimeWriter) validateScheduleRuntime(cfg ScheduleAWSConfig, lifetime time.Duration) error {
	if w == nil || w.client == nil || cfg != w.config || lifetime < 0 || time.Until(w.expires) < lifetime {
		return ErrRuntimeStore
	}
	return nil
}

// NewRuntimeStore selects only the configured platform runtime source. It never
// uses the deployment loader, environment credentials or a refreshing cache.
func NewRuntimeStore(ctx context.Context, cfg ScheduleAWSConfig, maxBytes int64, requiredLifetime time.Duration) (*Store, error) {
	if ctx == nil || ValidateScheduleAWSConfig(cfg) != nil || strings.HasSuffix(cfg.Bucket, "--x-s3") || maxBytes < 1 || maxBytes > 5<<30 || requiredLifetime <= 0 || requiredLifetime > time.Hour+15*time.Second || rejectRuntimeEnvironment(cfg.CredentialKind) != nil {
		return nil, ErrRuntimeStore
	}
	source, err := runtimeMetadataSource(cfg.CredentialKind)
	if err != nil {
		return nil, ErrRuntimeStore
	}
	identityHTTP, err := runtimeHTTPClient(2*time.Second, false)
	if err != nil {
		return nil, ErrRuntimeStore
	}
	objectHTTP, err := runtimeHTTPClient(0, false)
	if err != nil {
		return nil, ErrRuntimeStore
	}
	return newRuntimeStoreWithClients(ctx, cfg, maxBytes, requiredLifetime, source, sts.Options{Region: cfg.Region, HTTPClient: identityHTTP}, s3.Options{Region: cfg.Region, HTTPClient: objectHTTP})
}

// This private composition seam permits real TLS SDK fixtures. Production owns
// every supplied client option above; no CLI mode or public caller selects it.
func newRuntimeStoreWithClients(ctx context.Context, cfg ScheduleAWSConfig, maxBytes int64, requiredLifetime time.Duration, source aws.CredentialsProvider, identityOptions sts.Options, objectOptions s3.Options) (*Store, error) {
	if ctx == nil || source == nil || ValidateScheduleAWSConfig(cfg) != nil || strings.HasSuffix(cfg.Bucket, "--x-s3") || maxBytes < 1 || maxBytes > 5<<30 || requiredLifetime <= 0 || requiredLifetime > time.Hour+15*time.Second {
		return nil, ErrRuntimeStore
	}
	metadataContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	snapshot, err := source.Retrieve(metadataContext)
	cancel()
	if err != nil || len(snapshot.AccessKeyID) == 0 || len(snapshot.SecretAccessKey) == 0 || len(snapshot.SessionToken) == 0 || !snapshot.CanExpire || time.Until(snapshot.Expires) < requiredLifetime {
		return nil, ErrRuntimeStore
	}
	private := runtimeSnapshot{credentials: snapshot}
	identityOptions.Logger = logging.Nop{}
	identityOptions.Region = cfg.Region
	identityOptions.Credentials = private
	identityOptions.Retryer = aws.NopRetryer{}
	identityOptions.RetryMaxAttempts = 1
	identityOptions.DisableClockSkewCorrection = true
	identity := sts.New(identityOptions)
	identityContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	result, err := identity.GetCallerIdentity(identityContext, &sts.GetCallerIdentityInput{})
	cancel()
	if err != nil || result == nil || aws.ToString(result.Account) != cfg.AccountID || !runtimeAssumedRole(cfg.RuntimeRoleARN, aws.ToString(result.Arn)) || time.Until(snapshot.Expires) < requiredLifetime {
		return nil, ErrRuntimeStore
	}
	objectOptions.Logger = logging.Nop{}
	objectOptions.DisableS3ExpressSessionAuth = aws.Bool(true)
	objectOptions.ExpressCredentials = nil
	objectOptions.Region = cfg.Region
	objectOptions.Credentials = private
	objectOptions.Retryer = aws.NopRetryer{}
	objectOptions.RetryMaxAttempts = 1
	objectOptions.DisableClockSkewCorrection = true
	objectOptions.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	objectOptions.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	writer := &runtimeWriter{config: cfg, expires: snapshot.Expires, client: s3.New(objectOptions), maxBytes: maxBytes}
	store, err := NewStore(writer, cfg.Bucket, cfg.Prefix, cfg.KMSKeyARN, maxBytes)
	if err != nil {
		return nil, ErrRuntimeStore
	}
	return store, nil
}
func runtimeAssumedRole(expected, actual string) bool {
	want := strings.Split(expected, ":")
	got := strings.Split(actual, ":")
	if len(want) != 6 || len(got) != 6 || want[0] != "arn" || got[0] != "arn" || want[1] != got[1] || want[2] != "iam" || got[2] != "sts" || want[3] != "" || got[3] != "" || want[4] != got[4] {
		return false
	}
	role := strings.Split(want[5], "/")
	assumed := strings.Split(got[5], "/")
	return len(role) == 2 && role[0] == "role" && len(assumed) == 3 && assumed[0] == "assumed-role" && assumed[1] == role[1] && assumed[2] != "" && len(assumed[2]) <= 64
}
func (w *runtimeWriter) PutObject(ctx context.Context, input PutObjectInput) (PutObjectOutput, error) {
	if ctx == nil || w == nil || ctx.Err() != nil || w.validateScheduleRuntime(w.config, 0) != nil || input.Bucket != w.config.Bucket || input.Body == nil || input.ContentLength <= 0 || input.IfNoneMatch != "*" || input.ServerSideEncrypt != "aws:kms" || input.KMSKeyID != w.config.KMSKeyARN || input.BucketKeyEnabled {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	prefix := strings.Trim(w.config.Prefix, "/") + "/"
	if !strings.HasPrefix(input.Key, prefix) {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	parts := strings.Split(strings.TrimPrefix(input.Key, prefix), "/")
	if len(parts) != 2 || !storeIDPattern.MatchString(parts[0]) || (parts[1] != "database.dump" && parts[1] != "manifest.json") {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	checksum, err := base64.StdEncoding.DecodeString(input.ChecksumSHA256)
	if err != nil || len(checksum) != 32 || base64.StdEncoding.EncodeToString(checksum) != input.ChecksumSHA256 {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	limit := w.maxBytes
	if parts[1] == "manifest.json" {
		limit = 2 << 20
	}
	if input.ContentLength > limit || input.Metadata["backup-id"] != parts[0] || len(input.Metadata) != 2 {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	if parts[1] == "database.dump" {
		if !sha256Pattern.MatchString(input.Metadata["sha256"]) || input.Metadata["sha256"] != hex.EncodeToString(checksum) {
			return PutObjectOutput{}, ErrRuntimeStore
		}
	} else if input.Metadata["manifest"] != "complete" {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	uploadContext, cancel := context.WithDeadline(ctx, w.expires)
	defer cancel()
	result, err := w.client.PutObject(uploadContext, &s3.PutObjectInput{Bucket: aws.String(input.Bucket), Key: aws.String(input.Key), Body: input.Body, ContentLength: aws.Int64(input.ContentLength), ChecksumSHA256: aws.String(input.ChecksumSHA256), IfNoneMatch: aws.String(input.IfNoneMatch), Metadata: input.Metadata, ServerSideEncryption: s3types.ServerSideEncryptionAwsKms, SSEKMSKeyId: aws.String(input.KMSKeyID), BucketKeyEnabled: aws.Bool(false), ExpectedBucketOwner: aws.String(w.config.BucketAccountID)})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "PreconditionFailed" || api.ErrorCode() == "ConditionalRequestConflict") {
			return PutObjectOutput{}, ErrDuplicateBackup
		}
		return PutObjectOutput{}, ErrRuntimeStore
	}
	if result == nil || aws.ToString(result.ChecksumSHA256) != input.ChecksumSHA256 || aws.ToString(result.VersionId) == "" || aws.ToString(result.VersionId) == "null" || len(aws.ToString(result.VersionId)) > 1024 || strings.ContainsAny(aws.ToString(result.VersionId), "\r\n\x00") {
		return PutObjectOutput{}, ErrRuntimeStore
	}
	return PutObjectOutput{ChecksumSHA256: aws.ToString(result.ChecksumSHA256), VersionID: aws.ToString(result.VersionId)}, nil
}
