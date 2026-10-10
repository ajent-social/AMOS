package restore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalid      = errors.New("invalid restore request")
	ErrIntegrity    = errors.New("backup integrity check failed")
	ErrIncompatible = errors.New("backup is incompatible")
	ErrTargetExists = errors.New("restore target already exists")
	ErrActivation   = errors.New("restore activation barrier failed")
	ErrCleanup      = errors.New("restore cleanup failed")
	nameRE          = regexp.MustCompile("^[a-z][a-z0-9_]{0,62}$")
)

type Request struct {
	BackupDir, AdminURL, Target, PGRestorePath, TempDir string
	CurrentMigrations                                   []backup.MigrationVersion
	CurrentModules                                      []backup.ModuleVersion
	MaxBytes                                            int64
	Timeout                                             time.Duration
}
type Plan struct {
	Manifest    backup.Manifest
	ArchivePath string
}
type Prepared struct {
	request   Request
	manifest  backup.Manifest
	stage     string
	elapsed   time.Duration
	mu        sync.Mutex
	active    bool
	discarded bool
}
type ProviderChecker interface{ CheckCurrentProviderState(context.Context) error }

func Preflight(ctx context.Context, r Request) (plan Plan, resultErr error) {
	if r.BackupDir == "" || r.AdminURL == "" || r.PGRestorePath == "" || r.TempDir == "" || !nameRE.MatchString(r.Target) || r.MaxBytes <= 0 || r.Timeout <= 0 || r.Timeout > time.Hour || len(r.CurrentModules) == 0 {
		return Plan{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	adminConfig, e := pgx.ParseConfig(r.AdminURL)
	if e != nil || !isLoopback(adminConfig.Host) {
		return Plan{}, ErrInvalid
	}
	// No frozen secret inventory or verification source exists for this restore contract.
	// Integration must validate required secret references before activation; a caller-supplied
	// nonempty byte map would falsely claim availability.
	f, e := os.Open(filepath.Join(r.BackupDir, "manifest.json"))
	if e != nil {
		return Plan{}, ErrIntegrity
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	var m backup.Manifest
	if e = d.Decode(&m); e != nil {
		return Plan{}, ErrIntegrity
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Plan{}, ErrIntegrity
	}
	if m.Format != "amos-database-backup-v1" || m.State != "complete" || m.Scope != "database" || m.ArchiveFormat != "postgresql_custom" || m.ArchiveName != "database.dump" || m.PostgreSQLRolesIncluded || m.ServerMajor < 10 || m.ArchiveBytes <= 0 || m.ArchiveBytes > r.MaxBytes || len(m.ArchiveSHA256) != 64 || len(m.Migrations) == 0 {
		return Plan{}, ErrIncompatible
	}
	if m.SchemaVersion != m.Migrations[len(m.Migrations)-1].Sequence || m.BackupStartedAt.IsZero() || m.BackupFinishedAt.Before(m.BackupStartedAt) || m.SnapshotTransactionStartedAt.After(m.BackupFinishedAt) || !migrationPrefix(m.Migrations, r.CurrentMigrations) || !sameModules(m.Modules, r.CurrentModules) {
		return Plan{}, ErrIncompatible
	}
	p := filepath.Join(r.BackupDir, m.ArchiveName)
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() != m.ArchiveBytes {
		return Plan{}, ErrIntegrity
	}
	af, e := os.Open(p)
	if e != nil {
		return Plan{}, ErrIntegrity
	}
	h := sha256.New()
	n, e1 := io.Copy(h, io.LimitReader(af, r.MaxBytes+1))
	e2 := af.Close()
	if e1 != nil || e2 != nil || n != m.ArchiveBytes || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.ArchiveSHA256) {
		return Plan{}, ErrIntegrity
	}
	v, e := exec.CommandContext(ctx, r.PGRestorePath, "--version").Output()
	if e != nil {
		return Plan{}, ErrIncompatible
	}
	var major int
	if _, e = fmt.Sscanf(strings.TrimSpace(string(v)), "pg_restore (PostgreSQL) %d", &major); e != nil || major < m.ServerMajor {
		return Plan{}, ErrIncompatible
	}
	c := exec.CommandContext(ctx, r.PGRestorePath, "--list", p)
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	if c.Run() != nil {
		return Plan{}, ErrIntegrity
	}
	return Plan{m, p}, nil
}
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func migrationPrefix(a, b []backup.MigrationVersion) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] || a[i].Sequence != uint64(i+1) {
			return false
		}
	}
	return true
}
func equalMigrations(a, b []backup.MigrationVersion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameModules(a, b []backup.ModuleVersion) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]backup.ModuleVersion(nil), a...)
	y := append([]backup.ModuleVersion(nil), b...)
	sort.Slice(x, func(i, j int) bool { return x[i].ID < x[j].ID })
	sort.Slice(y, func(i, j int) bool { return y[i].ID < y[j].ID })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
func Restore(ctx context.Context, r Request) (prepared *Prepared, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	start := time.Now()
	p, e := Preflight(ctx, r)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(r.TempDir, 0700); e != nil {
		return nil, ErrInvalid
	}
	dir, e := os.MkdirTemp(r.TempDir, "restore-")
	if e != nil {
		return nil, ErrInvalid
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	archive := filepath.Join(dir, "database.dump")
	in, e := os.Open(p.ArchivePath)
	if e != nil {
		return nil, ErrIntegrity
	}
	out, e := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		if closeErr := in.Close(); closeErr != nil {
			return nil, errors.Join(ErrInvalid, ErrCleanup)
		}
		return nil, ErrInvalid
	}
	h := sha256.New()
	n, e1 := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, r.MaxBytes+1))
	e2 := in.Close()
	e3 := out.Close()
	if e1 != nil || e2 != nil || e3 != nil || n != p.Manifest.ArchiveBytes || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(p.Manifest.ArchiveSHA256) {
		return nil, ErrIntegrity
	}
	admin, e := pgx.Connect(ctx, r.AdminURL)
	if e != nil {
		return nil, ErrInvalid
	}
	defer func() {
		if closeErr := admin.Close(context.Background()); closeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	var exists bool
	if e = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", r.Target).Scan(&exists); e != nil {
		return nil, ErrInvalid
	}
	if exists {
		return nil, ErrTargetExists
	}
	var sv string
	if e = admin.QueryRow(ctx, "SELECT current_setting('server_version_num')").Scan(&sv); e != nil {
		return nil, ErrInvalid
	}
	var ver int
	if n, scanErr := fmt.Sscanf(sv, "%d", &ver); scanErr != nil || n != 1 || ver/10000 != p.Manifest.ServerMajor {
		return nil, ErrIncompatible
	}
	var rb [8]byte
	if _, e = rand.Read(rb[:]); e != nil {
		return nil, ErrInvalid
	}
	stage := "amos_restore_" + hex.EncodeToString(rb[:])
	keep := false
	defer func() {
		if !keep {
			cc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stageName := pgx.Identifier{stage}.Sanitize()
			if _, fenceErr := admin.Exec(cc, "ALTER DATABASE "+stageName+" WITH ALLOW_CONNECTIONS false"); fenceErr != nil {
				resultErr = errors.Join(resultErr, ErrCleanup)
			}
			if _, cleanupErr := admin.Exec(cc, "DROP DATABASE IF EXISTS "+stageName); cleanupErr != nil {
				resultErr = errors.Join(resultErr, ErrCleanup)
			}
		}
	}()
	// Keep the stage unreachable from the instant PostgreSQL creates it. The
	// cleanup defer is already armed in case CREATE DATABASE commits but its
	// response is lost or the request context is canceled.
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{stage}.Sanitize()+" WITH ALLOW_CONNECTIONS false"); e != nil {
		return nil, ErrInvalid
	}
	if _, e = admin.Exec(ctx, "REVOKE CONNECT ON DATABASE "+pgx.Identifier{stage}.Sanitize()+" FROM PUBLIC"); e != nil {
		return nil, ErrInvalid
	}
	// The restore connection is the database owner. Re-enable connections only
	// after public access is revoked; cleanup fences and drops this private stage.
	if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{stage}.Sanitize()+" WITH ALLOW_CONNECTIONS true"); e != nil {
		return nil, ErrInvalid
	}
	if e = runRestore(ctx, r, stage, archive, dir); e != nil {
		return nil, errors.Join(ErrIncompatible, errors.New("pg_restore execution failed"))
	}
	u, e := dbURL(r.AdminURL, stage)
	if e != nil {
		return nil, ErrInvalid
	}
	c, e := pgx.Connect(ctx, u)
	if e != nil {
		return nil, ErrInvalid
	}
	ledger, e := readLedger(ctx, c)
	_ = c.Close(context.Background())
	if e != nil || !equalMigrations(ledger, p.Manifest.Migrations) {
		return nil, errors.Join(ErrIncompatible, errors.New("restored migration ledger mismatch"))
	}
	if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{stage}.Sanitize()+" WITH ALLOW_CONNECTIONS false"); e != nil {
		return nil, ErrInvalid
	}
	keep = true
	return &Prepared{request: r, manifest: p.Manifest, stage: stage, elapsed: time.Since(start)}, nil
}
func (p *Prepared) Activate(ctx context.Context, checker ProviderChecker) (resultErr error) {
	if p == nil || checker == nil {
		return ErrActivation
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discarded {
		return ErrActivation
	}
	if p.active {
		if e := checker.CheckCurrentProviderState(ctx); e != nil {
			return errors.Join(ErrActivation, e)
		}
		return nil
	}
	if e := checker.CheckCurrentProviderState(ctx); e != nil {
		return errors.Join(ErrActivation, e)
	}
	admin, e := pgx.Connect(ctx, p.request.AdminURL)
	if e != nil {
		return ErrActivation
	}
	defer func() {
		if closeErr := admin.Close(context.Background()); closeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	activeName := p.stage
	activated := false
	defer func() {
		if activated {
			return
		}
		cc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, closeErr := admin.Exec(cc, "ALTER DATABASE "+pgx.Identifier{activeName}.Sanitize()+" WITH ALLOW_CONNECTIONS false"); closeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	// Arm the fence cleanup before enabling connections. An ambiguous DDL
	// response must leave the restored database inaccessible on return.
	if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{p.stage}.Sanitize()+" WITH ALLOW_CONNECTIONS true"); e != nil {
		return ErrActivation
	}
	u, e := dbURL(p.request.AdminURL, p.stage)
	if e != nil {
		return ErrActivation
	}
	c, e := pgx.Connect(ctx, u)
	if e != nil {
		return ErrActivation
	}
	_, e = c.Exec(ctx, "UPDATE public.identity_sessions SET revoked_at=COALESCE(revoked_at,transaction_timestamp()) WHERE revoked_at IS NULL")
	_ = c.Close(context.Background())
	if e != nil {
		return errors.Join(ErrActivation, e)
	}
	if e = checker.CheckCurrentProviderState(ctx); e != nil {
		return ErrActivation
	}
	if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{p.stage}.Sanitize()+" WITH ALLOW_CONNECTIONS false"); e != nil {
		return ErrActivation
	}
	if p.stage != p.request.Target {
		if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{p.stage}.Sanitize()+" RENAME TO "+pgx.Identifier{p.request.Target}.Sanitize()); e != nil {
			return ErrActivation
		}
		p.stage = p.request.Target
		activeName = p.stage
	}
	if e = checker.CheckCurrentProviderState(ctx); e != nil {
		return ErrActivation
	}
	if _, e = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{p.request.Target}.Sanitize()+" WITH ALLOW_CONNECTIONS true"); e != nil {
		return ErrActivation
	}
	activated = true
	p.active = true
	return nil
}
func (p *Prepared) Discard(ctx context.Context) (resultErr error) {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discarded {
		return nil
	}
	c, e := pgx.Connect(ctx, p.request.AdminURL)
	if e != nil {
		return e
	}
	defer func() {
		if closeErr := c.Close(context.Background()); closeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	_, e = c.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{p.stage}.Sanitize())
	if e == nil {
		p.active = false
		p.discarded = true
	}
	return e
}
func (p *Prepared) RecoveryPointLag() time.Duration {
	if p == nil {
		return 0
	}
	lag := time.Since(p.manifest.SnapshotTransactionStartedAt)
	if lag < 0 {
		return 0
	}
	return lag
}
func (p *Prepared) Evidence() (backup.Manifest, time.Duration) {
	if p == nil {
		return backup.Manifest{}, 0
	}
	return p.manifest, p.elapsed
}
func runRestore(ctx context.Context, r Request, db, archive, dir string) (resultErr error) {
	c, err := pgx.ParseConfig(r.AdminURL)
	if err != nil {
		return ErrInvalid
	}
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	if strings.ContainsAny(host+c.User+c.Password, "\r\n\x00") || strings.TrimSpace(host) != host || strings.TrimSpace(c.User) != c.User || strings.ContainsAny(host+c.User, "#") {
		return ErrInvalid
	}
	svc := filepath.Join(dir, "service.conf")
	pass := filepath.Join(dir, "pgpass")
	service := fmt.Sprintf("[rest]\nhost=%s\nport=%d\ndbname=%s\nuser=%s\nsslmode=disable\n", ini(host), c.Port, ini(db), ini(c.User))
	if err = os.WriteFile(svc, []byte(service), 0600); err != nil {
		return ErrInvalid
	}
	esc := func(s string) string { return strings.NewReplacer("\\", "\\\\", ":", "\\:", "\n", "\\n").Replace(s) }
	passline := fmt.Sprintf("%s:%d:%s:%s:%s\n", esc(host), c.Port, esc(db), esc(c.User), esc(c.Password))
	if err = os.WriteFile(pass, []byte(passline), 0600); err != nil {
		return ErrInvalid
	}
	defer func() {
		if removeErr := os.Remove(svc); removeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	defer func() {
		if removeErr := os.Remove(pass); removeErr != nil {
			resultErr = errors.Join(resultErr, ErrCleanup)
		}
	}()
	cmd := exec.CommandContext(ctx, r.PGRestorePath, "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname=service=rest", archive)
	cmd.Env = cleanEnv(os.Environ())
	cmd.Env = append(cmd.Env, "PGSERVICEFILE="+svc, "PGPASSFILE="+pass)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if cmd.Run() != nil {
		return ErrIncompatible
	}
	return nil
}
func dbURL(raw, db string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	u.Path = "/" + db
	return u.String(), nil
}

func readLedger(ctx context.Context, c *pgx.Conn) ([]backup.MigrationVersion, error) {
	var ok bool
	if e := c.QueryRow(ctx, "SELECT to_regclass('public.amos_schema_migrations') IS NOT NULL").Scan(&ok); e != nil || !ok {
		return nil, ErrIncompatible
	}
	rows, e := c.Query(ctx, "SELECT sequence,migration_id,encode(checksum,'hex') FROM public.amos_schema_migrations ORDER BY sequence")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []backup.MigrationVersion
	for rows.Next() {
		var m backup.MigrationVersion
		if e = rows.Scan(&m.Sequence, &m.ID, &m.SHA256); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func ini(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\n", "\\n")
}
func cleanEnv(e []string) []string {
	var o []string
	for _, x := range e {
		key, _, _ := strings.Cut(x, "=")
		if !strings.HasPrefix(key, "PG") {
			o = append(o, x)
		}
	}
	return o
}
