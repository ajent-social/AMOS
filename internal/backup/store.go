package backup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

var (
	ErrInvalidStoreRequest = errors.New("invalid backup store request")
	ErrIncompleteBackup    = errors.New("backup is not complete in remote storage")
	ErrDuplicateBackup     = errors.New("backup identifier already exists")
	ErrIntegrityMismatch   = errors.New("backup integrity verification failed")
	storeIDPattern         = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$")
	sha256Pattern          = regexp.MustCompile("^[a-f0-9]{64}$")
	storeBucketPattern     = regexp.MustCompile("^[a-z0-9](?:[a-z0-9.-]{1,61}[a-z0-9])$")
	storeKMSKeyPattern     = regexp.MustCompile("^arn:(aws|aws-us-gov|aws-cn):kms:[a-z0-9-]+:[0-9]{12}:key/[A-Za-z0-9-]+$")
)

const remoteManifestFormat = "amos-remote-database-backup-v1"

type PutObjectInput struct {
	Bucket            string
	Key               string
	Body              io.Reader
	ContentLength     int64
	ChecksumSHA256    string
	IfNoneMatch       string
	Metadata          map[string]string
	ServerSideEncrypt string
	KMSKeyID          string
	BucketKeyEnabled  bool
}

type PutObjectOutput struct {
	ChecksumSHA256 string
	VersionID      string
}

type GetObjectOutput struct {
	Body          io.ReadCloser
	ContentLength int64
	Metadata      map[string]string
}

type ObjectWriter interface {
	PutObject(context.Context, PutObjectInput) (PutObjectOutput, error)
}

// ObjectReader must be constructed with the separate restore identity. Runtime
// backup credentials deliberately have no object-read or object-delete access.
type ObjectReader interface {
	GetObject(context.Context, string, string, string) (GetObjectOutput, error)
}

type StoredManifest struct {
	Format                string   `json:"format"`
	Backup                Manifest `json:"backup"`
	ArchiveVersionID      string   `json:"archive_version_id"`
	ArchiveChecksumSHA256 string   `json:"archive_checksum_sha256"`
}

type StoredBackup struct {
	Manifest          Manifest
	ArchiveVersionID  string
	ManifestVersionID string
}

type Store struct {
	writer  ObjectWriter
	bucket  string
	prefix  string
	kmsKey  string
	maxSize int64
}

func NewStore(writer ObjectWriter, bucket, prefix, kmsKey string, maxSize int64) (*Store, error) {
	if writer == nil || !validStoreBucket(bucket) || !validStorePrefix(prefix) ||
		!storeKMSKeyPattern.MatchString(kmsKey) || maxSize < 1 || maxSize > 5<<30 {
		return nil, ErrInvalidStoreRequest
	}
	return &Store{writer: writer, bucket: bucket, prefix: strings.Trim(prefix, "/"), kmsKey: kmsKey, maxSize: maxSize}, nil
}

// Upload publishes the archive first and the remote manifest last. Each
// accepted PutObject must return the checksum S3 validated and a version ID;
// the remote manifest pins the exact archive version for recovery.
func (s *Store) Upload(ctx context.Context, backupDir string, want Manifest) (StoredBackup, error) {
	if ctx == nil || s == nil || s.writer == nil || !validBackupManifest(want, s.maxSize) {
		return StoredBackup{}, ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return StoredBackup{}, errors.Join(ErrIncompleteBackup, err)
	}
	manifest, _, err := readStoreManifest(backupDir, want)
	if err != nil {
		return StoredBackup{}, err
	}
	archivePath := filepath.Join(backupDir, manifest.ArchiveName)
	file, err := os.Open(archivePath)
	if err != nil {
		return StoredBackup{}, errors.Join(ErrInvalidStoreRequest, errors.New("backup archive is unavailable"))
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != manifest.ArchiveBytes {
		return StoredBackup{}, ErrIntegrityMismatch
	}
	localHash := sha256.New()
	if _, err := io.Copy(localHash, io.LimitReader(file, s.maxSize+1)); err != nil {
		return StoredBackup{}, errors.Join(ErrInvalidStoreRequest, errors.New("backup archive could not be read"))
	}
	if hex.EncodeToString(localHash.Sum(nil)) != manifest.ArchiveSHA256 {
		return StoredBackup{}, ErrIntegrityMismatch
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return StoredBackup{}, ErrInvalidStoreRequest
	}

	archiveKey := s.objectKey(want.ID, manifest.ArchiveName)
	expectedArchiveChecksum := base64.StdEncoding.EncodeToString(localHash.Sum(nil))
	archiveResult, err := s.writer.PutObject(ctx, PutObjectInput{
		Bucket: s.bucket, Key: archiveKey, Body: file, ContentLength: manifest.ArchiveBytes,
		ChecksumSHA256: expectedArchiveChecksum, IfNoneMatch: "*",
		Metadata:          map[string]string{"sha256": manifest.ArchiveSHA256, "backup-id": manifest.ID},
		ServerSideEncrypt: "aws:kms", KMSKeyID: s.kmsKey, BucketKeyEnabled: false,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateBackup) {
			return StoredBackup{}, ErrDuplicateBackup
		}
		return StoredBackup{}, errors.Join(ErrIncompleteBackup, err)
	}
	if archiveResult.VersionID == "" || archiveResult.ChecksumSHA256 != expectedArchiveChecksum {
		return StoredBackup{}, ErrIntegrityMismatch
	}

	remoteManifest := StoredManifest{
		Format: remoteManifestFormat, Backup: manifest, ArchiveVersionID: archiveResult.VersionID,
		ArchiveChecksumSHA256: archiveResult.ChecksumSHA256,
	}
	manifestBytes, err := json.Marshal(remoteManifest)
	if err != nil {
		return StoredBackup{}, ErrInvalidStoreRequest
	}
	manifestKey := s.objectKey(want.ID, "manifest.json")
	expectedManifestChecksum := checksumBase64(manifestBytes)
	manifestResult, err := s.writer.PutObject(ctx, PutObjectInput{
		Bucket: s.bucket, Key: manifestKey, Body: strings.NewReader(string(manifestBytes)), ContentLength: int64(len(manifestBytes)),
		ChecksumSHA256: expectedManifestChecksum, IfNoneMatch: "*",
		Metadata:          map[string]string{"backup-id": manifest.ID, "manifest": "complete"},
		ServerSideEncrypt: "aws:kms", KMSKeyID: s.kmsKey, BucketKeyEnabled: false,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateBackup) {
			return StoredBackup{}, ErrDuplicateBackup
		}
		return StoredBackup{}, errors.Join(ErrIncompleteBackup, err)
	}
	if manifestResult.VersionID == "" || manifestResult.ChecksumSHA256 != expectedManifestChecksum {
		return StoredBackup{}, ErrIntegrityMismatch
	}
	return StoredBackup{Manifest: manifest, ArchiveVersionID: archiveResult.VersionID, ManifestVersionID: manifestResult.VersionID}, nil
}

// VerifyComplete uses the separately authorized reader identity and reads the
// exact archive version pinned by the manifest-last completion marker.
func (s *Store) VerifyComplete(ctx context.Context, reader ObjectReader, id string) (StoredBackup, error) {
	if ctx == nil || s == nil || s.writer == nil || reader == nil || !storeIDPattern.MatchString(id) {
		return StoredBackup{}, ErrInvalidStoreRequest
	}
	data, err := getBytes(ctx, reader, s.bucket, s.objectKey(id, "manifest.json"), "", 1<<20)
	if err != nil {
		return StoredBackup{}, errors.Join(ErrIncompleteBackup, err)
	}
	var remote StoredManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&remote); err != nil || !validStoredManifest(remote, s.maxSize) || remote.Backup.ID != id {
		return StoredBackup{}, ErrIncompleteBackup
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return StoredBackup{}, ErrIncompleteBackup
	}
	if err := verifyObject(ctx, reader, s.bucket, s.objectKey(id, remote.Backup.ArchiveName), remote.ArchiveVersionID, remote.Backup.ArchiveBytes, remote.Backup.ArchiveSHA256); err != nil {
		return StoredBackup{}, err
	}
	return StoredBackup{Manifest: remote.Backup, ArchiveVersionID: remote.ArchiveVersionID}, nil
}

func validStoredManifest(remote StoredManifest, maxSize int64) bool {
	return remote.Format == remoteManifestFormat && validBackupManifest(remote.Backup, maxSize) &&
		remote.ArchiveVersionID != "" && len(remote.ArchiveVersionID) <= 1024 &&
		remote.ArchiveChecksumSHA256 == checksumBase64FromHex(remote.Backup.ArchiveSHA256)
}

// T10.7 emits only PostgreSQL custom archives and either a full-database scope
// with no schema filter or an explicit schema subset. Reject other manifests
// before publication and again when reading the remote completion marker.
func validBackupManifest(manifest Manifest, maxSize int64) bool {
	if !storeIDPattern.MatchString(manifest.ID) || manifest.Format != manifestFormat ||
		manifest.State != "complete" || manifest.ArchiveFormat != "postgresql_custom" ||
		manifest.ArchiveName != "database.dump" || manifest.ArchiveBytes < 1 ||
		manifest.ArchiveBytes > maxSize || !sha256Pattern.MatchString(manifest.ArchiveSHA256) {
		return false
	}
	switch manifest.Scope {
	case "database":
		if len(manifest.Schemas) != 0 {
			return false
		}
	case "schemas":
		if len(manifest.Schemas) == 0 {
			return false
		}
	default:
		return false
	}
	previous := ""
	for _, schema := range manifest.Schemas {
		if !schemaNamePattern.MatchString(schema) || schema <= previous {
			return false
		}
		previous = schema
	}
	return true
}

func verifyObject(ctx context.Context, reader ObjectReader, bucket, key, versionID string, size int64, wantSHA string) error {
	obj, err := reader.GetObject(ctx, bucket, key, versionID)
	if err != nil {
		return errors.Join(ErrIncompleteBackup, err)
	}
	if obj.Body == nil {
		return ErrIncompleteBackup
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(obj.Body, size+1))
	closeErr := obj.Body.Close()
	if err != nil || closeErr != nil || n != size || obj.ContentLength != size || hex.EncodeToString(hash.Sum(nil)) != wantSHA {
		return ErrIntegrityMismatch
	}
	return nil
}

func getBytes(ctx context.Context, reader ObjectReader, bucket, key, versionID string, max int64) ([]byte, error) {
	obj, err := reader.GetObject(ctx, bucket, key, versionID)
	if err != nil {
		return nil, err
	}
	if obj.Body == nil {
		return nil, ErrIncompleteBackup
	}
	data, err := io.ReadAll(io.LimitReader(obj.Body, max+1))
	closeErr := obj.Body.Close()
	if err != nil || closeErr != nil || int64(len(data)) > max || obj.ContentLength != int64(len(data)) {
		return nil, ErrIncompleteBackup
	}
	return data, nil
}

func readStoreManifest(dir string, want Manifest) (Manifest, []byte, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, nil, ErrInvalidStoreRequest
	}
	path := filepath.Join(dir, "manifest.json")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1<<20 {
		return Manifest{}, nil, ErrInvalidStoreRequest
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, nil, ErrInvalidStoreRequest
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || len(data) > 1<<20 {
		return Manifest{}, nil, ErrInvalidStoreRequest
	}
	var actual Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&actual); err != nil || !reflect.DeepEqual(actual, want) {
		return Manifest{}, nil, ErrIntegrityMismatch
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, nil, ErrIntegrityMismatch
	}
	return actual, data, nil
}

func (s *Store) objectKey(id, name string) string {
	return s.prefix + "/" + id + "/" + name
}

func checksumBase64(data []byte) string {
	sum := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func checksumBase64FromHex(value string) string {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(decoded)
}

func validStoreBucket(bucket string) bool {
	return storeBucketPattern.MatchString(bucket) && !strings.Contains(bucket, "..") && net.ParseIP(bucket) == nil
}

func validStorePrefix(prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || strings.ContainsAny(prefix, "*?#") {
		return false
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
