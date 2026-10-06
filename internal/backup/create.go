// Package backup creates consistency-aware PostgreSQL logical backups.
//
// Create writes one database or explicitly selected schema set to a protected
// local artifact. MaxBytes is mandatory and limits the compressed archive size;
// one successful call is one logical snapshot only and establishes no schedule,
// observed freshness, or RPO promise. The caller owns credential scoping, the
// exact pg_dump pin, module metadata, destination capacity, and schedule policy.
// Managed database automated/PITR snapshots complement this logical archive;
// block volume snapshots on the VM profile are crash-consistent and are not a
// logical substitute. Neither profile is production-qualified by this package.
package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidRequest  = errors.New("invalid backup request")
	ErrToolVersion     = errors.New("pg_dump version does not match the required pin")
	ErrServerVersion   = errors.New("PostgreSQL server major does not match pg_dump")
	ErrMigrationLedger = errors.New("database migration ledger is unavailable or invalid")
	ErrBackupTooLarge  = errors.New("backup exceeded the configured size limit")
	ErrDumpFailed      = errors.New("PostgreSQL dump failed")
	ErrDestination     = errors.New("backup destination is unavailable or already owned")
	ErrCanceled        = errors.New("backup was canceled")
)

const manifestFormat = "amos-database-backup-v1"

const (
	maxBackupTimeout = time.Hour
	cleanupTimeout   = 5 * time.Second
)

var (
	backupIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
	schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
	versionPattern    = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?$`)
	toolOutputPattern = regexp.MustCompile(`^pg_dump \(PostgreSQL\) (\d+(?:\.\d+){1,2})(?: .+)?$`)
	snapshotIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9A-Fa-f]+$`)
)

// Request contains all task-specific authority and limits. DatabaseURL is
// consumed only in memory and must name a credential scoped for backup reads.
// PGDumpPath and PGDumpVersion form an exact tool pin, while MaxBytes and
// Timeout (at most one hour) bound local resource use. Empty Schemas means the full database;
// non-empty Schemas makes an explicitly partial, schema-scoped artifact.
// ModuleVersions must come from trusted application composition, not user data.
// Callers must not mutate the request or its maps/slices until Create returns.
type Request struct {
	ID             string
	DatabaseURL    string
	DestinationDir string
	PGDumpPath     string
	PGDumpVersion  string
	ModuleVersions map[string]string
	Schemas        []string
	MaxBytes       int64
	Timeout        time.Duration
}

// Manifest is the completion marker and recovery boundary for one artifact.
// A manifest is published only after pg_dump succeeds, temporary credentials
// are removed, and the archive has been synced and hashed.
type Manifest struct {
	Format                       string             `json:"format"`
	State                        string             `json:"state"`
	ID                           string             `json:"id"`
	Scope                        string             `json:"scope"`
	Schemas                      []string           `json:"schemas,omitempty"`
	BackupStartedAt              time.Time          `json:"backup_started_at"`
	BackupFinishedAt             time.Time          `json:"backup_finished_at"`
	ConsistencyModel             string             `json:"consistency_model"`
	SnapshotTransactionStartedAt time.Time          `json:"snapshot_transaction_started_at"`
	ServerVersion                string             `json:"server_version"`
	ServerMajor                  int                `json:"server_major"`
	PGDumpVersion                string             `json:"pg_dump_version"`
	SchemaVersion                uint64             `json:"schema_version"`
	Migrations                   []MigrationVersion `json:"migrations"`
	Modules                      []ModuleVersion    `json:"modules"`
	ArchiveFormat                string             `json:"archive_format"`
	ArchiveName                  string             `json:"archive_name"`
	ArchiveBytes                 int64              `json:"archive_bytes"`
	ArchiveSHA256                string             `json:"archive_sha256"`
	PostgreSQLRolesIncluded      bool               `json:"postgresql_roles_included"`
}

type MigrationVersion struct {
	Sequence uint64 `json:"sequence"`
	ID       string `json:"id"`
	SHA256   string `json:"sha256"`
}

type ModuleVersion struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Create writes and atomically publishes a completed custom-format dump. The
// final directory is reserved with mkdir (which cannot replace an existing
// empty directory), but remains unselectable until manifest.json is linked as
// the last operation. Incomplete final directories are removed on ordinary
// errors; a process crash may leave one, but it has no completion manifest.
func Create(ctx context.Context, request Request) (Manifest, error) {
	request.Schemas = append([]string(nil), request.Schemas...)
	modules, err := moduleVersions(request.ModuleVersions)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateRequest(ctx, request); err != nil {
		return Manifest{}, err
	}
	backupCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	toolVersion, toolMajor, err := pinnedToolVersion(backupCtx, request)
	if err != nil {
		return Manifest{}, err
	}
	config, err := parseDatabaseURL(request.DatabaseURL)
	if err != nil {
		return Manifest{}, err
	}

	conn, err := pgx.ConnectConfig(backupCtx, config)
	if err != nil {
		if backupCtx.Err() != nil {
			return Manifest{}, ErrCanceled
		}
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not open the scoped backup connection"))
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()
		_ = conn.Close(cleanupCtx)
	}()

	tx, err := conn.BeginTx(backupCtx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		if backupCtx.Err() != nil {
			return Manifest{}, ErrCanceled
		}
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not begin a repeatable-read snapshot"))
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()
		_ = tx.Rollback(cleanupCtx)
	}()

	var snapshotID string
	var snapshotStarted time.Time
	var serverVersion string
	var serverVersionNumber int
	if err := tx.QueryRow(backupCtx, `SELECT pg_export_snapshot(), transaction_timestamp(), current_setting('server_version'), current_setting('server_version_num')::integer`).Scan(&snapshotID, &snapshotStarted, &serverVersion, &serverVersionNumber); err != nil {
		if backupCtx.Err() != nil {
			return Manifest{}, ErrCanceled
		}
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not export a consistent database snapshot"))
	}
	if !snapshotIDPattern.MatchString(snapshotID) {
		return Manifest{}, ErrDumpFailed
	}
	serverMajor := serverVersionNumber / 10000
	if serverMajor != toolMajor {
		return Manifest{}, ErrServerVersion
	}
	migrations, err := readMigrationLedger(backupCtx, tx, request.Schemas)
	if err != nil {
		if backupCtx.Err() != nil {
			return Manifest{}, ErrCanceled
		}
		return Manifest{}, err
	}
	schemas := append([]string(nil), request.Schemas...)
	sort.Strings(schemas)

	finalDir := filepath.Join(request.DestinationDir, "backup-"+request.ID)
	if err := os.Mkdir(finalDir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Manifest{}, ErrDestination
		}
		return Manifest{}, errors.Join(ErrDestination, errors.New("could not reserve an exclusive backup directory"))
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(finalDir)
		}
	}()

	servicePath, passwordPath, cleanupCredentials, err := writeConnectionFiles(finalDir, config, request.DatabaseURL)
	if err != nil {
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not create protected temporary connection files"))
	}
	credentialsRemoved := false
	defer func() {
		if !credentialsRemoved {
			_ = cleanupCredentials()
		}
	}()

	partialPath := filepath.Join(finalDir, "database.dump.partial")
	archivePath := filepath.Join(finalDir, "database.dump")
	archive, err := os.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Manifest{}, errors.Join(ErrDestination, errors.New("could not create a private archive file"))
	}
	hasher := sha256.New()
	limited := &limitedHashWriter{writer: archive, hasher: hasher, limit: request.MaxBytes}
	args := []string{"--format=custom", "--snapshot=" + snapshotID, "--no-password", "--no-owner", "--no-acl", "--dbname=service=amos-backup"}
	for _, schema := range schemas {
		args = append(args, `--schema="`+strings.ReplaceAll(schema, `"`, `""`)+`"`)
	}
	command := exec.CommandContext(backupCtx, request.PGDumpPath, args...)
	command.Env = childEnvironment(servicePath, passwordPath)
	command.Stdout = limited
	command.Stderr = io.Discard
	startedAt := time.Now().UTC()
	runErr := command.Run()
	if limited.err != nil {
		_ = archive.Close()
		if errors.Is(limited.err, ErrBackupTooLarge) {
			return Manifest{}, ErrBackupTooLarge
		}
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not write the archive"))
	}
	if backupCtx.Err() != nil {
		_ = archive.Close()
		return Manifest{}, ErrCanceled
	}
	if runErr != nil || limited.written == 0 {
		_ = archive.Close()
		return Manifest{}, ErrDumpFailed
	}
	if err := archive.Sync(); err != nil {
		_ = archive.Close()
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not sync the completed archive"))
	}
	if err := archive.Close(); err != nil {
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not close the completed archive"))
	}
	if err := os.Link(partialPath, archivePath); err != nil {
		return Manifest{}, errors.Join(ErrDestination, errors.New("could not publish the archive without replacement"))
	}
	if err := os.Remove(partialPath); err != nil {
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not remove the partial archive name"))
	}
	if err := cleanupCredentials(); err != nil {
		return Manifest{}, errors.Join(ErrDumpFailed, errors.New("could not remove temporary connection credentials"))
	}
	credentialsRemoved = true

	manifest := Manifest{
		Format:                       manifestFormat,
		State:                        "complete",
		ID:                           request.ID,
		Scope:                        "database",
		Schemas:                      schemas,
		BackupStartedAt:              startedAt,
		BackupFinishedAt:             time.Now().UTC(),
		ConsistencyModel:             "postgresql_exported_repeatable_read_snapshot",
		SnapshotTransactionStartedAt: snapshotStarted.UTC(),
		ServerVersion:                serverVersion,
		ServerMajor:                  serverMajor,
		PGDumpVersion:                toolVersion,
		Migrations:                   migrations,
		Modules:                      modules,
		ArchiveFormat:                "postgresql_custom",
		ArchiveName:                  filepath.Base(archivePath),
		ArchiveBytes:                 limited.written,
		ArchiveSHA256:                hex.EncodeToString(hasher.Sum(nil)),
		PostgreSQLRolesIncluded:      false,
	}
	if len(schemas) > 0 {
		manifest.Scope = "schemas"
	}
	if len(migrations) > 0 {
		manifest.SchemaVersion = migrations[len(migrations)-1].Sequence
	}
	if err := writeCompletionManifest(finalDir, manifest); err != nil {
		return Manifest{}, errors.Join(ErrDestination, errors.New("could not publish the completion manifest"))
	}
	published = true
	return manifest, nil
}

func validateRequest(ctx context.Context, request Request) error {
	if ctx == nil || request.ID == "" || !backupIDPattern.MatchString(request.ID) || request.DatabaseURL == "" || request.DestinationDir == "" || request.PGDumpPath == "" || !filepath.IsAbs(request.PGDumpPath) || request.PGDumpVersion == "" || !validVersion(request.PGDumpVersion) || request.MaxBytes <= 0 || request.Timeout <= 0 || request.Timeout > maxBackupTimeout {
		return ErrInvalidRequest
	}
	info, err := os.Stat(request.PGDumpPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ErrInvalidRequest
	}
	info, err = os.Lstat(request.DestinationDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return ErrInvalidRequest
	}
	if len(request.ModuleVersions) == 0 {
		return ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(request.Schemas))
	for _, schema := range request.Schemas {
		if !schemaNamePattern.MatchString(schema) {
			return ErrInvalidRequest
		}
		if _, ok := seen[schema]; ok {
			return ErrInvalidRequest
		}
		seen[schema] = struct{}{}
	}
	return nil
}

func validVersion(value string) bool {
	return versionPattern.MatchString(value)
}

func pinnedToolVersion(ctx context.Context, request Request) (string, int, error) {
	command := exec.CommandContext(ctx, request.PGDumpPath, "--version")
	command.Env = childEnvironment("", "")
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, ErrCanceled
		}
		return "", 0, ErrToolVersion
	}
	match := toolOutputPattern.FindStringSubmatch(strings.TrimSpace(string(output)))
	if len(match) != 2 || match[1] != request.PGDumpVersion {
		return "", 0, ErrToolVersion
	}
	parsed := versionPattern.FindStringSubmatch(match[1])
	major, err := strconv.Atoi(parsed[1])
	if err != nil || major < 10 {
		return "", 0, ErrToolVersion
	}
	return match[1], major, nil
}

func parseDatabaseURL(value string) (*pgx.ConnConfig, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.Path == "" || u.Path == "/" {
		return nil, ErrInvalidRequest
	}
	for key := range u.Query() {
		switch key {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "sslpassword", "application_name", "target_session_attrs", "connect_timeout", "hostaddr":
		default:
			return nil, ErrInvalidRequest
		}
	}
	config, err := pgx.ParseConfig(value)
	if err != nil || config.Host == "" || config.Database == "" || config.User == "" || strings.Contains(config.Host, ",") {
		return nil, ErrInvalidRequest
	}
	return config, nil
}

func readMigrationLedger(ctx context.Context, tx pgx.Tx, schemas []string) ([]MigrationVersion, error) {
	var table string
	if len(schemas) > 0 {
		var found string
		for _, schema := range schemas {
			var exists bool
			regclassName := pgx.Identifier{schema, "amos_schema_migrations"}.Sanitize()
			if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, regclassName).Scan(&exists); err != nil {
				return nil, ErrMigrationLedger
			}
			if exists {
				if found != "" {
					return nil, ErrMigrationLedger
				}
				found = schema
			}
		}
		if found == "" {
			return nil, ErrMigrationLedger
		}
		table = pgx.Identifier{found, "amos_schema_migrations"}.Sanitize()
	} else {
		var currentSchema string
		if err := tx.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema == "" {
			return nil, ErrMigrationLedger
		}
		table = pgx.Identifier{currentSchema, "amos_schema_migrations"}.Sanitize()
	}
	rows, err := tx.Query(ctx, `SELECT sequence, migration_id, encode(checksum, 'hex') FROM `+table+` ORDER BY sequence`)
	if err != nil {
		return nil, ErrMigrationLedger
	}
	defer rows.Close()
	var result []MigrationVersion
	var expected uint64 = 1
	for rows.Next() {
		var item MigrationVersion
		if err := rows.Scan(&item.Sequence, &item.ID, &item.SHA256); err != nil || item.Sequence != expected || item.ID == "" || len(item.SHA256) != sha256.Size*2 {
			return nil, ErrMigrationLedger
		}
		result = append(result, item)
		expected++
	}
	if err := rows.Err(); err != nil {
		return nil, ErrMigrationLedger
	}
	return result, nil
}

func moduleVersions(values map[string]string) ([]ModuleVersion, error) {
	if len(values) == 0 {
		return nil, ErrInvalidRequest
	}
	result := make([]ModuleVersion, 0, len(values))
	for id, version := range values {
		if !validModuleID(id) || strings.TrimSpace(version) == "" || strings.ContainsAny(version, "\r\n\x00") {
			return nil, ErrInvalidRequest
		}
		result = append(result, ModuleVersion{ID: id, Version: version})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func validModuleID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for i, char := range value {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		isSeparator := i > 0 && (char == '-' || char == '_')
		if !isLetter && !isDigit && !isSeparator {
			return false
		}
	}
	return true
}

func writeConnectionFiles(directory string, config *pgx.ConnConfig, databaseURL string) (string, string, func() error, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", "", nil, ErrInvalidRequest
	}
	servicePath := filepath.Join(directory, ".pg_service.conf")
	passwordPath := filepath.Join(directory, ".pgpass")
	serviceValues := map[string]string{
		"host":   config.Host,
		"port":   strconv.Itoa(int(config.Port)),
		"user":   config.User,
		"dbname": config.Database,
	}
	allowed := map[string]string{
		"sslmode": "sslmode", "sslrootcert": "sslrootcert", "sslcert": "sslcert", "sslkey": "sslkey",
		"sslpassword": "sslpassword", "application_name": "application_name", "target_session_attrs": "target_session_attrs",
		"connect_timeout": "connect_timeout", "hostaddr": "hostaddr",
	}
	for key, values := range u.Query() {
		if target, ok := allowed[key]; ok {
			if len(values) != 1 || strings.ContainsAny(values[0], "\r\n\x00") {
				return "", "", nil, ErrInvalidRequest
			}
			serviceValues[target] = values[0]
		}
	}
	var service strings.Builder
	service.WriteString("[amos-backup]\n")
	for _, key := range []string{"host", "hostaddr", "port", "user", "dbname", "sslmode", "sslrootcert", "sslcert", "sslkey", "sslpassword", "application_name", "target_session_attrs", "connect_timeout"} {
		value, ok := serviceValues[key]
		if !ok || value == "" {
			continue
		}
		plain, err := quoteServiceValue(value)
		if err != nil {
			return "", "", nil, err
		}
		service.WriteString(key + "=" + plain + "\n")
	}
	if err := writePrivateFile(servicePath, []byte(service.String())); err != nil {
		return "", "", nil, err
	}
	password := config.Password
	if strings.ContainsAny(password, "\r\n\x00") {
		_ = os.Remove(servicePath)
		return "", "", nil, ErrInvalidRequest
	}
	passHost := config.Host
	if strings.HasPrefix(passHost, "/") {
		passHost = ""
	}
	var passfile strings.Builder
	if password != "" {
		passfile.WriteString(escapePassfile(passHost) + ":" + strconv.Itoa(int(config.Port)) + ":" + escapePassfile(config.Database) + ":" + escapePassfile(config.User) + ":" + escapePassfile(password) + "\n")
	}
	if err := writePrivateFile(passwordPath, []byte(passfile.String())); err != nil {
		_ = os.Remove(servicePath)
		return "", "", nil, err
	}
	cleanup := func() error {
		var errs []error
		if err := os.Remove(servicePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		if err := os.Remove(passwordPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
	return servicePath, passwordPath, cleanup, nil
}

func quoteServiceValue(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00#") || strings.TrimSpace(value) != value {
		return "", ErrInvalidRequest
	}
	return value, nil
}

func escapePassfile(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, ":", `\:`)
}

func writePrivateFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func childEnvironment(servicePath, passwordPath string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "SYSTEMROOT": true, "WINDIR": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	env := make([]string, 0, len(allowed)+2)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[key] {
			env = append(env, entry)
		}
	}
	if servicePath != "" {
		env = append(env, "PGSERVICEFILE="+servicePath)
	}
	if passwordPath != "" {
		env = append(env, "PGPASSFILE="+passwordPath)
	}
	return env
}

type limitedHashWriter struct {
	writer  io.Writer
	hasher  hash.Hash
	limit   int64
	written int64
	err     error
}

func (w *limitedHashWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	remaining := w.limit - w.written
	if int64(len(p)) > remaining {
		if remaining > 0 {
			written, err := w.writer.Write(p[:remaining])
			if written > 0 {
				_, _ = w.hasher.Write(p[:written])
				w.written += int64(written)
			}
			if err != nil {
				w.err = err
				return written, err
			}
		}
		w.err = ErrBackupTooLarge
		return int(remaining), w.err
	}
	written, err := w.writer.Write(p)
	if written > 0 {
		_, _ = w.hasher.Write(p[:written])
		w.written += int64(written)
	}
	if err != nil {
		w.err = err
	}
	return written, err
}

func writeCompletionManifest(directory string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := filepath.Join(directory, "manifest.json.partial")
	complete := filepath.Join(directory, "manifest.json")
	if err := writePrivateFile(temporary, data); err != nil {
		return err
	}
	if err := os.Link(temporary, complete); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Remove(temporary); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}
