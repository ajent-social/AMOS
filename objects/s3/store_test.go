package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ajent-social/amos/objects"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

type fakeClient struct {
	putInput    *awss3.PutObjectInput
	headInput   *awss3.HeadObjectInput
	headOutput  *awss3.HeadObjectOutput
	getInput    *awss3.GetObjectInput
	deleteInput *awss3.DeleteObjectInput
	putErr      error
}

func (f *fakeClient) PutObject(_ context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	f.putInput = in
	_, err := io.Copy(io.Discard, in.Body)
	if err != nil {
		return nil, err
	}
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &awss3.PutObjectOutput{VersionId: aws.String("version-1")}, nil
}
func (f *fakeClient) HeadObject(_ context.Context, in *awss3.HeadObjectInput, _ ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	f.headInput = in
	return f.headOutput, nil
}
func (f *fakeClient) GetObject(_ context.Context, in *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	f.getInput = in
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader([]byte("export")))}, nil
}
func (f *fakeClient) DeleteObject(_ context.Context, in *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	f.deleteInput = in
	return &awss3.DeleteObjectOutput{}, nil
}

func TestStorePinsOwnerEncryptionAndObjectVersion(t *testing.T) {
	workspace, request := testID(t), testID(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ref := objects.Reference{ID: testID(t), WorkspaceID: workspace, RequestID: request,
		ContentType: "application/json", Size: 6, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	client := &fakeClient{}
	store, err := New(client, Config{Bucket: "private-exports", Prefix: "amos/lifecycle",
		ExpectedBucketOwner: "123456789012", KMSKeyID: "arn:aws:kms:us-east-1:123456789012:key/example"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("export"))); err != nil {
		t.Fatalf("put export: %v", err)
	}
	if client.putInput == nil || aws.ToString(client.putInput.ExpectedBucketOwner) != "123456789012" ||
		client.putInput.ServerSideEncryption != types.ServerSideEncryptionAwsKms ||
		aws.ToString(client.putInput.SSEKMSKeyId) == "" || !aws.ToBool(client.putInput.BucketKeyEnabled) ||
		aws.ToString(client.putInput.IfNoneMatch) != "*" || client.putInput.ACL != "" {
		t.Fatalf("unsafe S3 put request: %+v", client.putInput)
	}
	if got := aws.ToString(client.putInput.Key); got != "amos/lifecycle/"+ref.ID.String() {
		t.Fatalf("object key is not opaque ID only: %q", got)
	}
	if client.putInput.Metadata["amos-workspace"] != workspace.String() ||
		client.putInput.Metadata["amos-request"] != request.String() {
		t.Fatalf("ownership metadata missing: %+v", client.putInput.Metadata)
	}

	client.headOutput = &awss3.HeadObjectOutput{
		Metadata: client.putInput.Metadata, VersionId: aws.String("version-1"),
		ContentLength: aws.Int64(ref.Size), ServerSideEncryption: types.ServerSideEncryptionAwsKms,
		SSEKMSKeyId: aws.String("arn:aws:kms:us-east-1:123456789012:key/example"),
	}
	got, err := store.Head(context.Background(), ref.ID)
	if err != nil {
		t.Fatalf("head object: %v", err)
	}
	if got.WorkspaceID != workspace || got.RequestID != request || got.VersionID != "version-1" {
		t.Fatalf("unexpected decoded reference: %+v", got)
	}
	body, err := store.Open(context.Background(), got)
	if err != nil {
		t.Fatalf("open exact version: %v", err)
	}
	contents, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || string(contents) != "export" ||
		aws.ToString(client.getInput.VersionId) != "version-1" {
		t.Fatalf("open did not pin verified version: %q %v %v input=%+v", contents, readErr, closeErr, client.getInput)
	}
	if err = store.Delete(context.Background(), got); err != nil ||
		aws.ToString(client.deleteInput.VersionId) != "version-1" {
		t.Fatalf("delete exact version: %v input=%+v", err, client.deleteInput)
	}
}

func TestStoreRejectsMissingOwnerAndMalformedMetadata(t *testing.T) {
	if _, err := New(&fakeClient{}, Config{Bucket: "private", Prefix: "x", KMSKeyID: "key"}); !errors.Is(err, objects.ErrInvalid) {
		t.Fatalf("missing expected bucket owner accepted: %v", err)
	}
	workspace, request, id := testID(t), testID(t), testID(t)
	now := time.Now().UTC()
	ref := objects.Reference{ID: id, WorkspaceID: workspace, RequestID: request,
		ContentType: "application/json", Size: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	client := &fakeClient{headOutput: &awss3.HeadObjectOutput{
		Metadata: map[string]string{"amos-id": id.String()}, VersionId: aws.String("version-1"),
		ContentLength: aws.Int64(1), ServerSideEncryption: types.ServerSideEncryptionAwsKms,
		SSEKMSKeyId: aws.String("key"),
	}}
	store, err := New(client, Config{Bucket: "private", Prefix: "x", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Head(context.Background(), ref.ID); !errors.Is(err, objects.ErrNotFound) {
		t.Fatalf("malformed metadata accepted: %v", err)
	}
}

func TestStoreRejectsShortBodyAndRemovesPartialObject(t *testing.T) {
	ref := objects.Reference{ID: testID(t), WorkspaceID: testID(t), RequestID: testID(t),
		ContentType: "application/json", Size: 10, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour)}
	client := &fakeClient{}
	store, err := New(client, Config{Bucket: "private", Prefix: "x", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("short"))); !errors.Is(err, objects.ErrInvalid) {
		t.Fatalf("short body accepted: %v", err)
	}
	if client.deleteInput == nil || aws.ToString(client.deleteInput.VersionId) != "version-1" {
		t.Fatalf("partial object version was not removed: %+v", client.deleteInput)
	}
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
