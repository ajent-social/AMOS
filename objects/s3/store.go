// Package s3 implements the private object store using the AWS SDK for Go v2.
package s3

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ajent-social/amos/objects"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

type client interface {
	PutObject(context.Context, *awss3.PutObjectInput, ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	HeadObject(context.Context, *awss3.HeadObjectInput, ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
	GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	DeleteObject(context.Context, *awss3.DeleteObjectInput, ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error)
}

type Config struct {
	Bucket              string
	Prefix              string
	ExpectedBucketOwner string
	KMSKeyID            string
}

type Store struct {
	client client
	bucket string
	prefix string
	owner  string
	kmsKey string
}

func New(client client, config Config) (*Store, error) {
	prefix := strings.Trim(config.Prefix, "/")
	if client == nil || config.Bucket == "" || prefix == "" || config.ExpectedBucketOwner == "" || config.KMSKeyID == "" {
		return nil, objects.ErrInvalid
	}
	return &Store{client: client, bucket: config.Bucket, prefix: prefix,
		owner: config.ExpectedBucketOwner, kmsKey: config.KMSKeyID}, nil
}

func (s *Store) Put(ctx context.Context, ref objects.Reference, body io.Reader) error {
	if s == nil || body == nil || !validReference(ref) {
		return objects.ErrInvalid
	}
	counter := &countingReader{reader: body}
	out, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(ref.ID)),
		Body: counter, ContentLength: aws.Int64(ref.Size), ContentType: aws.String(ref.ContentType),
		ExpectedBucketOwner:  aws.String(s.owner),
		ServerSideEncryption: types.ServerSideEncryptionAwsKms, SSEKMSKeyId: aws.String(s.kmsKey),
		BucketKeyEnabled: aws.Bool(true), IfNoneMatch: aws.String("*"),
		Metadata: metadata(ref),
	})
	if err != nil {
		return normalizeAWSError(err)
	}
	versionID := ""
	if out != nil && out.VersionId != nil {
		versionID = *out.VersionId
	}
	if versionID == "" || versionID == "null" {
		// A known version can be removed exactly, including S3's literal null
		// version. With no returned version ID, cleanup cannot safely target
		// this write; fail closed and leave reconciliation to the caller.
		if versionID != "" {
			_, cleanupErr := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
				Bucket: aws.String(s.bucket), Key: aws.String(s.key(ref.ID)), VersionId: aws.String(versionID),
				ExpectedBucketOwner: aws.String(s.owner),
			})
			if cleanupErr != nil {
				return objects.ErrStorageUnavailable
			}
		}
		return objects.ErrStorageUnavailable
	}
	if counter.read != ref.Size {
		_, cleanupErr := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(s.key(ref.ID)), VersionId: aws.String(versionID),
			ExpectedBucketOwner: aws.String(s.owner),
		})
		if cleanupErr != nil {
			return objects.ErrStorageUnavailable
		}
		return objects.ErrInvalid
	}
	return nil
}

func (s *Store) Head(ctx context.Context, id uuid.UUID) (objects.Reference, error) {
	if s == nil || !validID(id) {
		return objects.Reference{}, objects.ErrInvalid
	}
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(id)), ExpectedBucketOwner: aws.String(s.owner),
	})
	if err != nil {
		return objects.Reference{}, normalizeAWSError(err)
	}
	if out == nil {
		return objects.Reference{}, objects.ErrStorageUnavailable
	}
	ref, err := decodeMetadata(id, out.Metadata)
	if err != nil || out.VersionId == nil || *out.VersionId == "" || *out.VersionId == "null" || out.ContentLength == nil || *out.ContentLength != ref.Size ||
		out.ServerSideEncryption != types.ServerSideEncryptionAwsKms ||
		out.SSEKMSKeyId == nil || *out.SSEKMSKeyId != s.kmsKey {
		return objects.Reference{}, objects.ErrNotFound
	}
	ref.VersionID = *out.VersionId
	return ref, nil
}

func (s *Store) Open(ctx context.Context, ref objects.Reference) (io.ReadCloser, error) {
	if s == nil || !validReference(ref) || ref.VersionID == "" {
		return nil, objects.ErrInvalid
	}
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(ref.ID)), VersionId: aws.String(ref.VersionID),
		ExpectedBucketOwner: aws.String(s.owner),
	})
	if err != nil {
		return nil, normalizeAWSError(err)
	}
	if out == nil || out.Body == nil {
		return nil, objects.ErrStorageUnavailable
	}
	return out.Body, nil
}

func (s *Store) Delete(ctx context.Context, ref objects.Reference) error {
	if s == nil || !validReference(ref) || ref.VersionID == "" {
		return objects.ErrInvalid
	}
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(ref.ID)), VersionId: aws.String(ref.VersionID),
		ExpectedBucketOwner: aws.String(s.owner),
	})
	if err != nil {
		return normalizeAWSError(err)
	}
	return nil
}

func normalizeAWSError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NoSuchVersion", "NotFound":
			return objects.ErrNotFound
		}
	}
	return objects.ErrStorageUnavailable
}

func (s *Store) key(id uuid.UUID) string { return s.prefix + "/" + id.String() }

func metadata(ref objects.Reference) map[string]string {
	return map[string]string{
		"amos-id":           ref.ID.String(),
		"amos-workspace":    ref.WorkspaceID.String(),
		"amos-request":      ref.RequestID.String(),
		"amos-content-type": ref.ContentType,
		"amos-size":         strconv.FormatInt(ref.Size, 10),
		"amos-created-at":   ref.CreatedAt.UTC().Format(time.RFC3339Nano),
		"amos-expires-at":   ref.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeMetadata(id uuid.UUID, values map[string]string) (objects.Reference, error) {
	parseID := func(key string) (uuid.UUID, error) {
		value := values["amos-"+key]
		parsed, err := uuid.Parse(value)
		if err != nil || !validID(parsed) {
			return uuid.Nil, objects.ErrNotFound
		}
		return parsed, nil
	}
	workspace, err := parseID("workspace")
	if err != nil {
		return objects.Reference{}, err
	}
	request, err := parseID("request")
	if err != nil {
		return objects.Reference{}, err
	}
	storedID, err := parseID("id")
	if err != nil || storedID != id {
		return objects.Reference{}, objects.ErrNotFound
	}
	size, err := strconv.ParseInt(values["amos-size"], 10, 64)
	if err != nil || size <= 0 {
		return objects.Reference{}, objects.ErrNotFound
	}
	created, err := time.Parse(time.RFC3339Nano, values["amos-created-at"])
	if err != nil {
		return objects.Reference{}, objects.ErrNotFound
	}
	expires, err := time.Parse(time.RFC3339Nano, values["amos-expires-at"])
	if err != nil || !expires.After(created) {
		return objects.Reference{}, objects.ErrNotFound
	}
	contentType := values["amos-content-type"]
	if contentType == "" || strings.ContainsAny(contentType, "\r\n") {
		return objects.Reference{}, objects.ErrNotFound
	}
	return objects.Reference{ID: id, WorkspaceID: workspace, RequestID: request,
		ContentType: contentType, Size: size, CreatedAt: created.UTC(), ExpiresAt: expires.UTC()}, nil
}

func validReference(ref objects.Reference) bool {
	return validID(ref.ID) && validID(ref.WorkspaceID) && validID(ref.RequestID) &&
		ref.Size > 0 && ref.ContentType != "" && ref.ExpiresAt.After(ref.CreatedAt)
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

var _ objects.Store = (*Store)(nil)
