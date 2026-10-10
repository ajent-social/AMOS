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
	putInput            *awss3.PutObjectInput
	headInput           *awss3.HeadObjectInput
	headOutput          *awss3.HeadObjectOutput
	getInput            *awss3.GetObjectInput
	deleteInput         *awss3.DeleteObjectInput
	putErr              error
	headErr             error
	deleteErr           error
	deleteCalls         []*awss3.DeleteObjectInput
	putOutput           *awss3.PutObjectOutput
	putOverride         bool
	headMetadataFromPut bool
	headNonceMismatch   bool
}

func (f *fakeClient) PutObject(_ context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	f.putInput = in
	if f.headMetadataFromPut && f.headOutput != nil {
		f.headOutput.Metadata = cloneMetadata(in.Metadata)
		if f.headNonceMismatch {
			f.headOutput.Metadata[writeNonceMetadataKey] = uuid.NewString()
		}
	}
	_, err := io.Copy(io.Discard, in.Body)
	if err != nil {
		return nil, err
	}
	if f.putErr != nil {
		return nil, f.putErr
	}
	if f.putOverride {
		return f.putOutput, nil
	}
	return &awss3.PutObjectOutput{VersionId: aws.String("version-1")}, nil
}
func (f *fakeClient) HeadObject(_ context.Context, in *awss3.HeadObjectInput, _ ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	f.headInput = in
	return f.headOutput, f.headErr
}
func (f *fakeClient) GetObject(_ context.Context, in *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	f.getInput = in
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader([]byte("export")))}, nil
}
func (f *fakeClient) DeleteObject(_ context.Context, in *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	f.deleteInput = in
	f.deleteCalls = append(f.deleteCalls, in)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
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
	writeNonce := client.putInput.Metadata[writeNonceMetadataKey]
	if _, err := uuid.Parse(writeNonce); err != nil || writeNonce == "" || len(client.putInput.Metadata) != len(metadata(ref))+1 {
		t.Fatalf("put metadata lacks unique operation nonce: %v", client.putInput.Metadata)
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

func TestStoreRequiresReconciliationWhenNullVersionCleanupFails(t *testing.T) {
	ref := objects.Reference{ID: testID(t), WorkspaceID: testID(t), RequestID: testID(t),
		ContentType: "application/json", Size: 6, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	client := &fakeClient{putOverride: true, putOutput: &awss3.PutObjectOutput{VersionId: aws.String("null")},
		deleteErr: errors.New("null cleanup response lost")}
	store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("export"))); err != objects.ErrReconciliationRequired {
		t.Fatalf("null cleanup failure returned %v, want reconciliation signal", err)
	}
	if client.deleteInput == nil || aws.ToString(client.deleteInput.VersionId) != "null" ||
		aws.ToString(client.deleteInput.Key) != "exports/"+ref.ID.String() {
		t.Fatalf("null cleanup did not target exact returned version: %+v", client.deleteInput)
	}
}

func TestStoreRequiresReconciliationWhenShortBodyCleanupFails(t *testing.T) {
	ref := objects.Reference{ID: testID(t), WorkspaceID: testID(t), RequestID: testID(t),
		ContentType: "application/json", Size: 10, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	client := &fakeClient{deleteErr: errors.New("partial cleanup response lost")}
	store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("short"))); err != objects.ErrReconciliationRequired {
		t.Fatalf("short-body cleanup failure returned %v, want reconciliation signal", err)
	}
	if client.deleteInput == nil || aws.ToString(client.deleteInput.VersionId) != "version-1" ||
		aws.ToString(client.deleteInput.Key) != "exports/"+ref.ID.String() {
		t.Fatalf("partial cleanup did not target exact returned version: %+v", client.deleteInput)
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

func TestStoreRejectsMissingOrNullVersionOnPut(t *testing.T) {
	workspace, request := testID(t), testID(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ref := objects.Reference{ID: testID(t), WorkspaceID: workspace, RequestID: request,
		ContentType: "application/json", Size: 6, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	tests := []struct {
		name              string
		output            *awss3.PutObjectOutput
		headVersion       *string
		wantDeleteVersion string
		wantHead          bool
	}{
		{name: "nil output", headVersion: aws.String("created-version"), wantDeleteVersion: "created-version", wantHead: true},
		{name: "missing version", output: &awss3.PutObjectOutput{}, headVersion: aws.String("created-version"), wantDeleteVersion: "created-version", wantHead: true},
		{name: "empty version", output: &awss3.PutObjectOutput{VersionId: aws.String("")}, headVersion: aws.String("created-version"), wantDeleteVersion: "created-version", wantHead: true},
		{name: "null version", output: &awss3.PutObjectOutput{VersionId: aws.String("null")}, wantDeleteVersion: "null"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{putOverride: true, putOutput: tc.output, headMetadataFromPut: tc.wantHead}
			if tc.wantHead {
				client.headOutput = headFor(ref, tc.headVersion, "key")
			}
			store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("export"))); !errors.Is(err, objects.ErrStorageUnavailable) {
				t.Fatalf("unversioned put accepted or reconciliation signal was unstable: %v", err)
			}
			if (client.headInput != nil) != tc.wantHead {
				t.Fatalf("head request presence = %t, want %t", client.headInput != nil, tc.wantHead)
			}
			if tc.wantHead && (aws.ToString(client.headInput.ExpectedBucketOwner) != "123456789012" ||
				aws.ToString(client.headInput.Key) != "exports/"+ref.ID.String()) {
				t.Fatalf("unsafe proof request: %+v", client.headInput)
			}
			if client.deleteInput == nil || aws.ToString(client.deleteInput.VersionId) != tc.wantDeleteVersion ||
				aws.ToString(client.deleteInput.ExpectedBucketOwner) != "123456789012" {
				t.Fatalf("cleanup request = %+v, want exact version %q", client.deleteInput, tc.wantDeleteVersion)
			}
		})
	}
}

func TestStoreRequiresReconciliationWhenMissingVersionCannotBeProven(t *testing.T) {
	workspace, request := testID(t), testID(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ref := objects.Reference{ID: testID(t), WorkspaceID: workspace, RequestID: request,
		ContentType: "application/json", Size: 6, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	other := ref
	other.RequestID = testID(t)
	tests := []struct {
		name                string
		head                *awss3.HeadObjectOutput
		headErr             error
		headMetadataFromPut bool
		headNonceMismatch   bool
	}{
		{name: "head unavailable", headErr: errors.New("temporary head failure")},
		{name: "head missing", head: nil},
		{name: "concurrent object metadata mismatch", head: headFor(other, aws.String("concurrent-version"), "key")},
		{name: "same-reference prior operation nonce mismatch", head: headFor(ref, aws.String("prior-operation-version"), "key"), headMetadataFromPut: true, headNonceMismatch: true},
		{name: "version unavailable", head: headFor(ref, nil, "key")},
		{name: "empty version", head: headFor(ref, aws.String(""), "key")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{putOverride: true, putOutput: &awss3.PutObjectOutput{}, headOutput: tc.head, headErr: tc.headErr,
				headMetadataFromPut: tc.headMetadataFromPut, headNonceMismatch: tc.headNonceMismatch}
			store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("export"))); !errors.Is(err, objects.ErrReconciliationRequired) ||
				err != objects.ErrReconciliationRequired {
				t.Fatalf("unsafe or unavailable proof returned %v, want stable reconciliation signal", err)
			}
			if client.deleteInput != nil || len(client.deleteCalls) != 0 {
				t.Fatalf("unproven object version was deleted: %+v", client.deleteCalls)
			}
		})
	}
}

func TestStoreRequiresReconciliationWhenExactCleanupFails(t *testing.T) {
	ref := objects.Reference{ID: testID(t), WorkspaceID: testID(t), RequestID: testID(t),
		ContentType: "application/json", Size: 6, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	client := &fakeClient{
		putOverride: true, putOutput: &awss3.PutObjectOutput{}, headMetadataFromPut: true,
		headOutput: headFor(ref, aws.String("just-written-version"), "key"),
		deleteErr:  errors.New("cleanup response lost"),
	}
	store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), ref, bytes.NewReader([]byte("export"))); err != objects.ErrReconciliationRequired {
		t.Fatalf("cleanup failure returned %v, want stable reconciliation signal", err)
	}
	if client.deleteInput == nil || aws.ToString(client.deleteInput.VersionId) != "just-written-version" ||
		aws.ToString(client.deleteInput.Key) != "exports/"+ref.ID.String() {
		t.Fatalf("cleanup did not pin exact proven version: %+v", client.deleteInput)
	}
}

func cloneMetadata(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func headFor(ref objects.Reference, versionID *string, key string) *awss3.HeadObjectOutput {
	return &awss3.HeadObjectOutput{
		Metadata: metadata(ref), VersionId: versionID, ContentLength: aws.Int64(ref.Size),
		ServerSideEncryption: types.ServerSideEncryptionAwsKms, SSEKMSKeyId: aws.String(key),
	}
}

func TestStoreRejectsMissingOrNullHeadVersion(t *testing.T) {
	workspace, request := testID(t), testID(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ref := objects.Reference{ID: testID(t), WorkspaceID: workspace, RequestID: request,
		ContentType: "application/json", Size: 6, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	for _, versionID := range []*string{nil, aws.String(""), aws.String("null")} {
		client := &fakeClient{headOutput: &awss3.HeadObjectOutput{
			Metadata: metadata(ref), ContentLength: aws.Int64(ref.Size), VersionId: versionID,
			ServerSideEncryption: types.ServerSideEncryptionAwsKms, SSEKMSKeyId: aws.String("key"),
		}}
		store, err := New(client, Config{Bucket: "private", Prefix: "exports", ExpectedBucketOwner: "123456789012", KMSKeyID: "key"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Head(context.Background(), ref.ID); !errors.Is(err, objects.ErrNotFound) {
			t.Fatalf("version %v accepted: %v", versionID, err)
		}
	}
}
