package backup

import (
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/google/uuid"
)

const ScheduleConfigVersion = "amos-backup-config-v1"
const ScheduleStatusVersion = "amos-backup-status-v1"

var ErrScheduleConfig = errors.New("invalid backup scheduling configuration")
var ErrScheduleUnavailable = errors.New("backup scheduling unavailable")
var ErrScheduleFailed = errors.New("backup attempt failed")

type ScheduleConfig struct {
	Version                                   string
	ScheduleID, InstallationID, ApplicationID uuid.UUID
	Environment                               string
	Targets                                   opsconfig.Config
	Database                                  ScheduleDatabaseConfig
	AWS                                       ScheduleAWSConfig
	Create                                    ScheduleCreateConfig
	UploadTimeout, RunTimeout                 time.Duration
	MaxRetainedAttempts, MaxStagingArtifacts  uint32
	MaxStagingBytes                           int64
}
type ScheduleDatabaseConfig struct {
	LedgerWriteRef, LedgerReadRef, DumpRef       string
	LedgerWriteRole, LedgerReadRole, DumpRole    string
	LedgerHost, LedgerDatabase, LedgerRootCAFile string
	DumpHost, DumpDatabase, DumpRootCAFile       string
	LedgerPort, DumpPort                         uint16
}
type ScheduleAWSConfig struct {
	CredentialKind, AccountID, Region, RuntimeRoleARN string
	BucketAccountID, Bucket, Prefix, KMSKeyARN        string
}
type ScheduleCreateConfig struct {
	StagingDir, PGDumpPath, PGDumpVersion string
	ModuleVersions                        map[string]string
	MaxBytes                              int64
	DumpTimeout                           time.Duration
}
type ScheduleResult string

const (
	ScheduleNotDue    ScheduleResult = "not_due"
	ScheduleBusy      ScheduleResult = "busy"
	ScheduleCompleted ScheduleResult = "completed"
	ScheduleFailed    ScheduleResult = "failed"
	ScheduleUnknown   ScheduleResult = "unknown"
	ScheduleCapacity  ScheduleResult = "capacity"
)

type ScheduleSnapshot struct {
	Version                     string    `json:"version"`
	State                       string    `json:"state"`
	FailureClass                string    `json:"failure_class"`
	ObservedAt                  time.Time `json:"observed_at"`
	LastAttemptedAt             time.Time `json:"last_attempted_at"`
	LastAttemptCompletedAt      time.Time `json:"last_attempt_completed_at"`
	LastPublishedAt             time.Time `json:"last_published_at"`
	LastSnapshotAt              time.Time `json:"last_snapshot_at"`
	AttemptDurationMilliseconds uint64    `json:"attempt_duration_milliseconds"`
	RetainedAttempts            uint32    `json:"retained_attempts"`
	RetainedLimit               uint32    `json:"retained_limit"`
	PublicationKnown            bool      `json:"publication_known"`
}

// Implemented only by the runtime Store's owned writer. This private binding is
// not a caller-provided assertion of provider qualification.
type scheduleRuntimeAuthority interface {
	validateScheduleRuntime(ScheduleAWSConfig, time.Duration) error
}

type Scheduler struct {
	db      *sql.DB
	cfg     ScheduleConfig
	digest  [32]byte
	store   *Store
	dumpURL string
	// Private finite test seam only. Production always reads PostgreSQL time.
	clock func(time.Time) time.Time
}
type ScheduleReader struct {
	db     *sql.DB
	cfg    ScheduleConfig
	digest [32]byte
}

// NewScheduler validates copied configuration and the private factory binding.
// It does not resolve envrefs, dial a service, dump, upload or mutate the ledger.
func NewScheduler(db *sql.DB, cfg ScheduleConfig, store *Store, dumpDatabaseURL string) (*Scheduler, error) {
	if db == nil || ValidateScheduleConfig(cfg) != nil || ValidateScheduleDatabaseURL(cfg.Database, ScheduleDump, dumpDatabaseURL) != nil {
		return nil, ErrScheduleConfig
	}
	if store == nil || store.bucket != cfg.AWS.Bucket || store.prefix != cfg.AWS.Prefix || store.kmsKey != cfg.AWS.KMSKeyARN || store.maxSize != cfg.Create.MaxBytes {
		return nil, ErrScheduleConfig
	}
	authority, ok := store.writer.(scheduleRuntimeAuthority)
	if !ok || authority.validateScheduleRuntime(cfg.AWS, cfg.RunTimeout+15*time.Second) != nil {
		return nil, ErrScheduleConfig
	}
	dumpDatabaseURL, _ = NormalizeScheduleDatabaseURL(cfg.Database, ScheduleDump, dumpDatabaseURL)
	cfg = copyScheduleConfig(cfg)
	digest, _ := ScheduleConfigDigest(cfg)
	return &Scheduler{db: db, cfg: cfg, digest: digest, store: store, dumpURL: dumpDatabaseURL}, nil
}
func NewScheduleReader(db *sql.DB, cfg ScheduleConfig) (*ScheduleReader, error) {
	if db == nil || ValidateScheduleConfig(cfg) != nil {
		return nil, ErrScheduleConfig
	}
	cfg = copyScheduleConfig(cfg)
	digest, _ := ScheduleConfigDigest(cfg)
	return &ScheduleReader{db: db, cfg: cfg, digest: digest}, nil
}
func copyScheduleConfig(c ScheduleConfig) ScheduleConfig {
	m := make(map[string]string, len(c.Create.ModuleVersions))
	for k, v := range c.Create.ModuleVersions {
		m[k] = v
	}
	c.Create.ModuleVersions = m
	return c
}
