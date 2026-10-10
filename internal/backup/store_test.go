package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type memoryObject struct {
	body []byte
	meta map[string]string
}

type memoryObjectClient struct {
	objects        map[string]memoryObject
	putKeys        []string
	getVersions    []string
	failKey        string
	failError      error
	badChecksumKey string
	badVersionKey  string
	cancelKey      string
	cancel         context.CancelFunc
}

func newMemoryObjectClient() *memoryObjectClient {
	return &memoryObjectClient{objects: make(map[string]memoryObject)}
}

func (m *memoryObjectClient) PutObject(ctx context.Context, in PutObjectInput) (PutObjectOutput, error) {
	m.putKeys = append(m.putKeys, in.Key)
	if in.ServerSideEncrypt != "aws:kms" || in.KMSKeyID == "" || in.BucketKeyEnabled || in.IfNoneMatch != "*" {
		return PutObjectOutput{}, errors.New("missing encrypted conditional-write controls")
	}
	if in.Key == m.failKey {
		return PutObjectOutput{}, m.failError
	}
	if in.Key == m.cancelKey {
		m.cancel()
		return PutObjectOutput{}, ctx.Err()
	}
	if _, ok := m.objects[in.Key]; ok {
		return PutObjectOutput{}, ErrDuplicateBackup
	}
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return PutObjectOutput{}, err
	}
	if int64(len(body)) != in.ContentLength {
		return PutObjectOutput{}, errors.New("content length mismatch")
	}
	m.objects[in.Key] = memoryObject{body: body, meta: in.Metadata}
	sum := sha256.Sum256(body)
	result := PutObjectOutput{ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:]), VersionID: fmt.Sprintf("version-%d", len(m.putKeys))}
	if in.Key == m.badChecksumKey {
		result.ChecksumSHA256 = "invalid-checksum"
	}
	if in.Key == m.badVersionKey {
		result.VersionID = ""
	}
	return result, nil
}

func (m *memoryObjectClient) GetObject(_ context.Context, _, key, versionID string) (GetObjectOutput, error) {
	m.getVersions = append(m.getVersions, versionID)
	obj, ok := m.objects[key]
	if !ok {
		return GetObjectOutput{}, os.ErrNotExist
	}
	return GetObjectOutput{Body: io.NopCloser(bytes.NewReader(obj.body)), ContentLength: int64(len(obj.body)), Metadata: obj.meta}, nil
}

func makeStoreFixture(t *testing.T, id string) (string, Manifest) {
	t.Helper()
	dir := t.TempDir()
	archive := []byte("synthetic consistent database archive")
	sum := sha256.Sum256(archive)
	manifest := Manifest{
		Format: manifestFormat, State: "complete", ID: id, Scope: "database",
		ArchiveFormat: "postgresql_custom", ArchiveName: "database.dump", ArchiveBytes: int64(len(archive)),
		ArchiveSHA256: hex.EncodeToString(sum[:]),
	}
	if err := os.WriteFile(filepath.Join(dir, "database.dump"), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}

func newTestStore(t *testing.T, client ObjectWriter) *Store {
	t.Helper()
	store, err := NewStore(client, "private-backups", "installations/test", "arn:aws:kms:us-east-1:123456789012:key/test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreUploadsVerifiedArchiveAndCompletionManifest(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-1")
	stored, err := store.Upload(context.Background(), dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ArchiveVersionID != "version-1" || stored.ManifestVersionID != "version-2" {
		t.Fatalf("versioned upload receipt = %+v", stored)
	}
	if len(client.putKeys) != 2 || client.putKeys[0] != "installations/test/backup-1/database.dump" ||
		client.putKeys[1] != "installations/test/backup-1/manifest.json" {
		t.Fatalf("unexpected publication order: %v", client.putKeys)
	}
	got, err := store.VerifyComplete(context.Background(), client, manifest.ID)
	if err != nil || got.Manifest.ID != manifest.ID || got.Manifest.ArchiveSHA256 != manifest.ArchiveSHA256 ||
		got.ArchiveVersionID != "version-1" {
		t.Fatalf("verified backup = %+v, err=%v", got, err)
	}
	if len(client.getVersions) != 2 || client.getVersions[0] != "" || client.getVersions[1] != "version-1" {
		t.Fatalf("reader did not pin the archived object version: %v", client.getVersions)
	}
	for _, key := range client.putKeys {
		if _, ok := client.objects[key]; !ok {
			t.Fatalf("object %q missing", key)
		}
	}
}

func TestStoreInterruptedArchiveUploadRemainsUnselectable(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-interrupted")
	ctx, cancel := context.WithCancel(context.Background())
	client.cancelKey = "installations/test/backup-interrupted/database.dump"
	client.cancel = cancel
	if _, err := store.Upload(ctx, dir, manifest); !errors.Is(err, ErrIncompleteBackup) {
		t.Fatalf("Upload error = %v, want incomplete backup", err)
	}
	if len(client.objects) != 0 {
		t.Fatalf("interrupted upload left selected objects: %v", client.objects)
	}
	if _, err := store.VerifyComplete(context.Background(), client, manifest.ID); !errors.Is(err, ErrIncompleteBackup) {
		t.Fatalf("VerifyComplete error = %v, want incomplete backup", err)
	}
}

func TestStoreManifestFailureLeavesBackupUnselectable(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-manifest-failure")
	client.failKey = "installations/test/backup-manifest-failure/manifest.json"
	client.failError = errors.New("synthetic manifest upload failure")
	if _, err := store.Upload(context.Background(), dir, manifest); !errors.Is(err, ErrIncompleteBackup) {
		t.Fatalf("Upload error = %v, want incomplete backup", err)
	}
	if _, ok := client.objects["installations/test/backup-manifest-failure/database.dump"]; !ok {
		t.Fatal("test did not simulate successful data upload")
	}
	if _, ok := client.objects[client.failKey]; ok {
		t.Fatal("failed manifest upload unexpectedly published completion marker")
	}
	if _, err := store.VerifyComplete(context.Background(), client, manifest.ID); !errors.Is(err, ErrIncompleteBackup) {
		t.Fatalf("VerifyComplete error = %v, want incomplete backup", err)
	}
}

func TestStoreRejectsDuplicateIDWithoutOverwriting(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-duplicate")
	if _, err := store.Upload(context.Background(), dir, manifest); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), client.objects["installations/test/backup-duplicate/database.dump"].body...)
	if _, err := store.Upload(context.Background(), dir, manifest); !errors.Is(err, ErrDuplicateBackup) {
		t.Fatalf("duplicate Upload error = %v", err)
	}
	after := client.objects["installations/test/backup-duplicate/database.dump"].body
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate identifier replaced existing archive")
	}
}

func TestStoreRejectsMalformedRemoteManifest(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown field", func(doc map[string]any) { doc["unexpected"] = true }},
		{"unsupported archive format", func(doc map[string]any) { doc["backup"].(map[string]any)["archive_format"] = "tar" }},
		{"unsupported scope", func(doc map[string]any) { doc["backup"].(map[string]any)["scope"] = "test" }},
		{"database scope with schema filter", func(doc map[string]any) { doc["backup"].(map[string]any)["schemas"] = []any{"public"} }},
		{"schema scope without schema filter", func(doc map[string]any) {
			b := doc["backup"].(map[string]any)
			b["scope"] = "schemas"
			b["schemas"] = []any{}
		}},
		{"missing archive version", func(doc map[string]any) { doc["archive_version_id"] = "" }},
		{"archive checksum mismatch", func(doc map[string]any) {
			doc["archive_checksum_sha256"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMemoryObjectClient()
			store := newTestStore(t, client)
			dir, manifest := makeStoreFixture(t, "backup-malformed")
			if _, err := store.Upload(context.Background(), dir, manifest); err != nil {
				t.Fatal(err)
			}
			key := "installations/test/backup-malformed/manifest.json"
			obj := client.objects[key]
			var doc map[string]any
			if err := json.Unmarshal(obj.body, &doc); err != nil {
				t.Fatal(err)
			}
			tc.mutate(doc)
			obj.body, _ = json.Marshal(doc)
			client.objects[key] = obj
			if _, err := store.VerifyComplete(context.Background(), client, manifest.ID); !errors.Is(err, ErrIncompleteBackup) {
				t.Fatalf("VerifyComplete error = %v, want incomplete backup", err)
			}
		})
	}
}

func TestStoreRejectsBadPutChecksumOrVersionResponse(t *testing.T) {
	cases := []struct {
		name   string
		setKey func(*memoryObjectClient, string)
	}{
		{"bad checksum", func(c *memoryObjectClient, key string) { c.badChecksumKey = key }},
		{"missing version", func(c *memoryObjectClient, key string) { c.badVersionKey = key }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMemoryObjectClient()
			store := newTestStore(t, client)
			dir, manifest := makeStoreFixture(t, "backup-bad-response")
			tc.setKey(client, "installations/test/backup-bad-response/database.dump")
			if _, err := store.Upload(context.Background(), dir, manifest); !errors.Is(err, ErrIntegrityMismatch) {
				t.Fatalf("Upload error = %v, want integrity mismatch", err)
			}
			if _, ok := client.objects["installations/test/backup-bad-response/manifest.json"]; ok {
				t.Fatal("bad archive receipt published a completion manifest")
			}
		})
	}
}

func TestStoreAcceptsSchemaScopedBackup(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-schema-scope")
	manifest.Scope = "schemas"
	manifest.Schemas = []string{"app_data", "public"}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(context.Background(), dir, manifest); err != nil {
		t.Fatalf("Upload schema-scoped backup: %v", err)
	}
	verified, err := store.VerifyComplete(context.Background(), client, manifest.ID)
	if err != nil || verified.Manifest.Scope != "schemas" || len(verified.Manifest.Schemas) != 2 ||
		verified.Manifest.Schemas[0] != "app_data" || verified.Manifest.Schemas[1] != "public" {
		t.Fatalf("verified schema-scoped backup = %+v, err=%v", verified, err)
	}
}

func TestStoreRejectsUnsupportedManifestContract(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Manifest)
	}{
		{"unsupported archive format", func(m *Manifest) { m.ArchiveFormat = "tar" }},
		{"unsupported scope", func(m *Manifest) { m.Scope = "test" }},
		{"database scope with schemas", func(m *Manifest) { m.Schemas = []string{"public"} }},
		{"schema scope without schemas", func(m *Manifest) { m.Scope = "schemas" }},
		{"unsorted schema filter", func(m *Manifest) { m.Scope = "schemas"; m.Schemas = []string{"public", "app_data"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMemoryObjectClient()
			store := newTestStore(t, client)
			dir, manifest := makeStoreFixture(t, "backup-contract")
			tc.change(&manifest)
			if _, err := store.Upload(context.Background(), dir, manifest); !errors.Is(err, ErrInvalidStoreRequest) {
				t.Fatalf("Upload error = %v, want invalid request", err)
			}
			if len(client.putKeys) != 0 {
				t.Fatalf("invalid manifest reached remote storage: %v", client.putKeys)
			}
		})
	}
}

func TestStoreRejectsCorruptRemoteArchive(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-corrupt")
	if _, err := store.Upload(context.Background(), dir, manifest); err != nil {
		t.Fatal(err)
	}
	key := "installations/test/backup-corrupt/database.dump"
	client.objects[key] = memoryObject{body: []byte("corrupt")}
	if _, err := store.VerifyComplete(context.Background(), client, manifest.ID); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("VerifyComplete error = %v, want integrity mismatch", err)
	}
}

func TestStoreRejectsLocalManifestAndArchiveMismatch(t *testing.T) {
	client := newMemoryObjectClient()
	store := newTestStore(t, client)
	dir, manifest := makeStoreFixture(t, "backup-local-mismatch")
	if err := os.WriteFile(filepath.Join(dir, "database.dump"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(context.Background(), dir, manifest); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("Upload error = %v, want integrity mismatch", err)
	}
	if len(client.putKeys) != 0 {
		t.Fatalf("invalid local artifact reached object storage: %v", client.putKeys)
	}
}

func TestStoreRejectsUnsafeConfigurationAndIdentifier(t *testing.T) {
	client := newMemoryObjectClient()
	for _, tc := range []struct {
		bucket, prefix, key string
		size                int64
	}{
		{"", "installations/test", "arn:aws:kms:region:account:key/x", 10},
		{"bucket", "../other", "arn:aws:kms:region:account:key/x", 10},
		{"bucket", "installations/*", "arn:aws:kms:region:account:key/x", 10},
		{"bucket", "installations/test", "arn:aws:kms:region:account:key/*", 10},
		{"bucket", "installations/test", "arn:aws:kms:us-east-1:123456789012:key/x", 6 << 30},
		{"bucket", "installations/test", "arn:aws:kms:region:account:key/x", 0},
	} {
		if _, err := NewStore(client, tc.bucket, tc.prefix, tc.key, tc.size); !errors.Is(err, ErrInvalidStoreRequest) {
			t.Errorf("NewStore(%+v) error = %v", tc, err)
		}
	}
	store := newTestStore(t, client)
	if _, err := store.VerifyComplete(context.Background(), client, "../other"); !errors.Is(err, ErrInvalidStoreRequest) {
		t.Fatalf("unsafe ID error = %v", err)
	}
}
