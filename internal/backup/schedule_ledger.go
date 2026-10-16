package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/ajent-social/amos/migrations"
	"github.com/google/uuid"
)

type scheduleScanner interface{ Scan(...any) error }
type scheduleQuery func(context.Context, string, ...any) scheduleScanner

const scheduleIdentitySQL = `SELECT current_database(),session_user,current_user,
 r.rolsuper,r.rolcreaterole,r.rolcreatedb,r.rolinherit,r.rolreplication,r.rolbypassrls,
 has_database_privilege(current_user,current_database(),'CREATE'),
 has_database_privilege(current_user,current_database(),'TEMP'),
 has_schema_privilege(current_user,'public','CREATE'),
 COALESCE((SELECT ssl AND version='TLSv1.3' FROM pg_catalog.pg_stat_ssl WHERE pid=pg_backend_pid()),false)
 FROM pg_catalog.pg_roles AS r WHERE r.rolname=current_user`

func checkScheduleIdentity(ctx context.Context, q scheduleQuery, database, role string) error {
	var db, session, user string
	var flags [10]bool
	if q(ctx, scheduleIdentitySQL).Scan(&db, &session, &user, &flags[0], &flags[1], &flags[2], &flags[3], &flags[4], &flags[5], &flags[6], &flags[7], &flags[8], &flags[9]) != nil || db != database || session != role || user != role || !flags[9] {
		return ErrScheduleUnavailable
	}
	for _, v := range flags[:9] {
		if v {
			return ErrScheduleUnavailable
		}
	}
	return nil
}
func beginScheduleTx(ctx context.Context, db *sql.DB, cfg ScheduleConfig, reader bool) (*sql.Tx, context.Context, context.CancelFunc, error) {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	tx, e := db.BeginTx(bounded, &sql.TxOptions{ReadOnly: reader})
	if e != nil {
		cancel()
		return nil, nil, nil, ErrScheduleUnavailable
	}
	fail := func() (*sql.Tx, context.Context, context.CancelFunc, error) {
		_ = tx.Rollback()
		cancel()
		return nil, nil, nil, ErrScheduleUnavailable
	}
	role := cfg.Database.LedgerWriteRole
	if reader {
		role = cfg.Database.LedgerReadRole
	}
	q := func(c context.Context, s string, args ...any) scheduleScanner {
		return tx.QueryRowContext(c, s, args...)
	}
	if checkScheduleIdentity(bounded, q, cfg.Database.LedgerDatabase, role) != nil {
		return fail()
	}
	fragment, e := migrations.BackupScheduling(1)
	if e != nil {
		return fail()
	}
	registry, e := migrations.NewRegistry(fragment)
	if e != nil {
		return fail()
	}
	migration := registry.Migrations()[0]
	checksum := sha256.Sum256([]byte(migration.SQL))
	rows, e := tx.QueryContext(bounded, `SELECT sequence,migration_id,checksum FROM public.amos_schema_migrations ORDER BY sequence LIMIT 2`)
	if e != nil {
		return fail()
	}
	count := 0
	valid := true
	for rows.Next() {
		var sequence int64
		var id string
		var sum []byte
		if rows.Scan(&sequence, &id, &sum) != nil {
			valid = false
			break
		}
		count++
		if sequence != 1 || id != migration.ID() || !bytes.Equal(sum, checksum[:]) {
			valid = false
		}
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if !valid || count != 1 || rowErr != nil || closeErr != nil {
		return fail()
	}
	return tx, bounded, cancel, nil
}

type scheduleRow struct {
	id                  uuid.UUID
	digest              []byte
	fence               int64
	retained            int64
	skipped             int64
	slot                sql.NullTime
	watermark           time.Time
	state, failure      string
	active              uuid.NullUUID
	lease               sql.NullTime
	last                uuid.NullUUID
	started, finished   sql.NullTime
	duration            int64
	success             uuid.NullUUID
	published, snapshot sql.NullTime
}

const scheduleColumns = `schedule_id,config_digest,fence,retained_attempts,skipped_slots,last_slot_at,clock_watermark,state,failure_class,active_attempt_id,lease_until,last_attempt_id,last_attempted_at,last_attempt_completed_at,last_attempt_duration_ms,last_success_attempt_id,last_published_at,last_snapshot_at`

func scanScheduleRow(s scheduleScanner) (scheduleRow, error) {
	var r scheduleRow
	e := s.Scan(&r.id, &r.digest, &r.fence, &r.retained, &r.skipped, &r.slot, &r.watermark, &r.state, &r.failure, &r.active, &r.lease, &r.last, &r.started, &r.finished, &r.duration, &r.success, &r.published, &r.snapshot)
	return r, e
}
func selectScheduleRow(ctx context.Context, tx *sql.Tx, cfg ScheduleConfig, lock bool) (scheduleRow, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	return scanScheduleRow(tx.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM public.amos_backup_schedules WHERE installation_id=$1 AND application_id=$2 AND environment=$3`+suffix, cfg.InstallationID, cfg.ApplicationID, cfg.Environment))
}
func scheduleBound(r scheduleRow, cfg ScheduleConfig, digest [32]byte) bool {
	return r.id == cfg.ScheduleID && bytes.Equal(r.digest, digest[:]) && r.fence >= 0 && r.retained >= 0 && r.retained <= 4096 && r.skipped >= 0 && r.duration >= 0
}
func scheduleClockValid(r scheduleRow, now time.Time) bool {
	if now.Unix() <= 0 || now.Before(r.watermark) {
		return false
	}
	for _, t := range []sql.NullTime{r.slot, r.started, r.finished, r.published, r.snapshot} {
		if t.Valid && now.Before(t.Time) {
			return false
		}
	}
	return r.snapshot.Valid == r.published.Valid && r.success.Valid == r.published.Valid && (!r.snapshot.Valid || !r.snapshot.Time.After(r.published.Time))
}
func scheduleNow(ctx context.Context, tx *sql.Tx, clock func(time.Time) time.Time) (time.Time, error) {
	var now time.Time
	if tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now) != nil {
		return time.Time{}, ErrScheduleUnavailable
	}
	if clock != nil {
		now = clock(now)
	}
	return now.UTC(), nil
}

type scheduleLease struct {
	id      uuid.UUID
	fence   int64
	started time.Time
}

func (s *Scheduler) admit(ctx context.Context, capacity bool) (scheduleLease, ScheduleResult, error) {
	var zero scheduleLease
	tx, c, cancel, e := beginScheduleTx(ctx, s.db, s.cfg, false)
	if e != nil {
		return zero, "", e
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()
	_, e = tx.ExecContext(c, `INSERT INTO public.amos_backup_schedules(schedule_id,installation_id,application_id,environment,config_digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, s.cfg.ScheduleID, s.cfg.InstallationID, s.cfg.ApplicationID, s.cfg.Environment, s.digest[:])
	if e != nil {
		return zero, "", ErrScheduleUnavailable
	}
	row, e := selectScheduleRow(c, tx, s.cfg, true)
	if e != nil || !scheduleBound(row, s.cfg, s.digest) {
		return zero, "", ErrScheduleConfig
	}
	now, e := scheduleNow(c, tx, s.clock)
	if e != nil {
		return zero, "", e
	}
	commitResult := func(result ScheduleResult, err error) (scheduleLease, ScheduleResult, error) {
		if tx.Commit() != nil {
			return zero, "", ErrScheduleUnavailable
		}
		return zero, result, err
	}
	if !scheduleClockValid(row, now) {
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET state='clock_invalid',failure_class='clock' WHERE schedule_id=$1 AND fence=$2`, row.id, row.fence)
		if e != nil {
			return zero, "", ErrScheduleUnavailable
		}
		return commitResult(ScheduleUnknown, ErrScheduleUnavailable)
	}
	if row.active.Valid {
		if !row.lease.Valid {
			return zero, "", ErrScheduleUnavailable
		}
		if row.lease.Time.After(now) && row.state == "running" {
			return commitResult(ScheduleBusy, nil)
		}
		if row.lease.Time.After(now) {
			return commitResult(ScheduleBusy, nil)
		}
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_attempts SET state='unknown',failure_class='lease_lost',finished_at=$1 WHERE schedule_id=$2 AND attempt_id=$3 AND fence=$4 AND state='running'`, now, row.id, row.active.UUID, row.fence)
		if e != nil {
			return zero, "", ErrScheduleUnavailable
		}
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET active_attempt_id=NULL,lease_until=NULL,state='unknown',failure_class='lease_lost',last_attempt_completed_at=$1,clock_watermark=$1 WHERE schedule_id=$2 AND fence=$3`, now, row.id, row.fence)
		if e != nil {
			return zero, "", ErrScheduleUnavailable
		}
	}
	seconds := int64(s.cfg.Targets.BackupInterval / time.Second)
	slot := time.Unix(now.Unix()/seconds*seconds, 0).UTC()
	if row.slot.Valid && !slot.After(row.slot.Time) {
		return commitResult(ScheduleNotDue, nil)
	}
	if capacity || row.retained >= int64(s.cfg.MaxRetainedAttempts) {
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET state='capacity',failure_class='capacity',clock_watermark=$1 WHERE schedule_id=$2 AND fence=$3`, now, row.id, row.fence)
		if e != nil {
			return zero, "", ErrScheduleUnavailable
		}
		return commitResult(ScheduleCapacity, ErrScheduleFailed)
	}
	skipped := int64(0)
	if row.slot.Valid {
		skipped = int64(slot.Sub(row.slot.Time)/s.cfg.Targets.BackupInterval) - 1
		if skipped < 0 {
			skipped = 0
		}
	}
	if row.fence == math.MaxInt64 || row.skipped > math.MaxInt64-skipped {
		return zero, "", ErrScheduleUnavailable
	}
	authority, ok := s.store.writer.(scheduleRuntimeAuthority)
	if !ok || authority.validateScheduleRuntime(s.cfg.AWS, s.cfg.RunTimeout+15*time.Second) != nil {
		return zero, "", ErrScheduleUnavailable
	}
	id, e := uuid.NewV7()
	if e != nil {
		return zero, "", ErrScheduleUnavailable
	}
	fence := row.fence + 1
	_, e = tx.ExecContext(c, `INSERT INTO public.amos_backup_attempts(attempt_id,schedule_id,slot_at,fence,started_at,state) VALUES($1,$2,$3,$4,$5,'running')`, id, row.id, slot, fence, now)
	if e != nil {
		return zero, "", ErrScheduleUnavailable
	}
	_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET fence=$1,retained_attempts=retained_attempts+1,skipped_slots=skipped_slots+$2,last_slot_at=$3,clock_watermark=$4,state='running',failure_class='',active_attempt_id=$5,lease_until=$6,last_attempt_id=$5,last_attempted_at=$4,last_attempt_completed_at=NULL,last_attempt_duration_ms=0 WHERE schedule_id=$7 AND fence=$8`, fence, skipped, slot, now, id, now.Add(s.cfg.RunTimeout+15*time.Second), row.id, row.fence)
	if e != nil || tx.Commit() != nil {
		return zero, "", ErrScheduleUnavailable
	}
	return scheduleLease{id: id, fence: fence, started: now}, "", nil
}
func (s *Scheduler) lockedLease(ctx context.Context, lease scheduleLease) (*sql.Tx, context.Context, context.CancelFunc, scheduleRow, time.Time, error) {
	tx, c, cancel, e := beginScheduleTx(ctx, s.db, s.cfg, false)
	if e != nil {
		return nil, nil, nil, scheduleRow{}, time.Time{}, e
	}
	fail := func() (*sql.Tx, context.Context, context.CancelFunc, scheduleRow, time.Time, error) {
		_ = tx.Rollback()
		cancel()
		return nil, nil, nil, scheduleRow{}, time.Time{}, ErrScheduleUnavailable
	}
	row, e := selectScheduleRow(c, tx, s.cfg, true)
	if e != nil || !scheduleBound(row, s.cfg, s.digest) {
		return fail()
	}
	now, e := scheduleNow(c, tx, s.clock)
	if e != nil || !scheduleClockValid(row, now) || row.state != "running" || !row.active.Valid || row.active.UUID != lease.id || row.fence != lease.fence || !row.lease.Valid || !row.lease.Time.After(now) {
		return fail()
	}
	return tx, c, cancel, row, now, nil
}
func (s *Scheduler) checkLease(ctx context.Context, lease scheduleLease) error {
	tx, c, cancel, _, now, e := s.lockedLease(ctx, lease)
	if e != nil {
		return e
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()
	_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET clock_watermark=$1 WHERE schedule_id=$2 AND fence=$3 AND active_attempt_id=$4`, now, s.cfg.ScheduleID, lease.fence, lease.id)
	if e != nil || tx.Commit() != nil {
		return ErrScheduleUnavailable
	}
	return nil
}
func (s *Scheduler) finish(ctx context.Context, lease scheduleLease, stored *StoredBackup, failure string, elapsed time.Duration) error {
	tx, c, cancel, row, now, e := s.lockedLease(ctx, lease)
	if e != nil {
		return e
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()
	duration := max(elapsed.Milliseconds(), 0)
	state := "failed"
	if failure == "unknown" || failure == "lease_lost" || failure == "upload" {
		state = "unknown"
	}
	var snapshot, archiveVersion, manifestVersion any
	if stored != nil {
		at := stored.Manifest.SnapshotTransactionStartedAt.UTC()
		if at.IsZero() || at.After(now) || row.snapshot.Valid && at.Before(row.snapshot.Time) || len(stored.ArchiveVersionID) == 0 || len(stored.ArchiveVersionID) > 1024 || stored.ArchiveVersionID == "null" || len(stored.ManifestVersionID) == 0 || len(stored.ManifestVersionID) > 1024 || stored.ManifestVersionID == "null" {
			return ErrScheduleUnavailable
		}
		state = "completed"
		failure = ""
		snapshot = at
		archiveVersion = stored.ArchiveVersionID
		manifestVersion = stored.ManifestVersionID
	}
	var published any
	if stored != nil {
		published = now
	}
	result, e := tx.ExecContext(c, `UPDATE public.amos_backup_attempts SET state=$1,failure_class=$2,finished_at=$3,duration_ms=$4,snapshot_at=$5,published_at=$6,archive_version_id=$7,manifest_version_id=$8 WHERE schedule_id=$9 AND attempt_id=$10 AND fence=$11 AND state='running'`, state, failure, now, duration, snapshot, published, archiveVersion, manifestVersion, row.id, lease.id, lease.fence)
	if e != nil {
		return ErrScheduleUnavailable
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return ErrScheduleUnavailable
	}
	if stored != nil {
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET state='completed',failure_class='',active_attempt_id=NULL,lease_until=NULL,last_attempt_completed_at=$1,last_attempt_duration_ms=$2,last_success_attempt_id=$3,last_published_at=$1,last_snapshot_at=$4,clock_watermark=$1 WHERE schedule_id=$5 AND fence=$6 AND active_attempt_id=$3`, now, duration, lease.id, snapshot, row.id, lease.fence)
	} else {
		_, e = tx.ExecContext(c, `UPDATE public.amos_backup_schedules SET state=$1,failure_class=$2,active_attempt_id=NULL,lease_until=NULL,last_attempt_completed_at=$3,last_attempt_duration_ms=$4,clock_watermark=$3 WHERE schedule_id=$5 AND fence=$6 AND active_attempt_id=$7`, state, failure, now, duration, row.id, lease.fence, lease.id)
	}
	if e != nil || tx.Commit() != nil {
		return ErrScheduleUnavailable
	}
	return nil
}

func (r *ScheduleReader) Snapshot(ctx context.Context) (ScheduleSnapshot, error) {
	if r == nil || r.db == nil || ctx == nil {
		return ScheduleSnapshot{}, ErrScheduleConfig
	}
	return readScheduleSnapshot(ctx, r.db, r.cfg, r.digest, true, nil)
}
func (s *Scheduler) Snapshot(ctx context.Context) (ScheduleSnapshot, error) {
	if s == nil || s.db == nil || ctx == nil {
		return ScheduleSnapshot{}, ErrScheduleConfig
	}
	return readScheduleSnapshot(ctx, s.db, s.cfg, s.digest, false, s.clock)
}
func readScheduleSnapshot(ctx context.Context, db *sql.DB, cfg ScheduleConfig, digest [32]byte, reader bool, clock func(time.Time) time.Time) (ScheduleSnapshot, error) {
	out := ScheduleSnapshot{Version: ScheduleStatusVersion, State: "unavailable", RetainedLimit: cfg.MaxRetainedAttempts}
	// Even a writer's Snapshot opens a read-only transaction.
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, e := db.BeginTx(bounded, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, ErrScheduleUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	role := cfg.Database.LedgerWriteRole
	if reader {
		role = cfg.Database.LedgerReadRole
	}
	q := func(c context.Context, s string, args ...any) scheduleScanner {
		return tx.QueryRowContext(c, s, args...)
	}
	if checkScheduleIdentity(bounded, q, cfg.Database.LedgerDatabase, role) != nil {
		return out, ErrScheduleUnavailable
	}
	// Reuse migration verification on this same read-only connection.
	if verifyScheduleMigration(bounded, tx) != nil {
		return out, ErrScheduleUnavailable
	}
	row, e := selectScheduleRow(bounded, tx, cfg, false)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrScheduleUnavailable
	}
	if e != nil {
		return out, ErrScheduleUnavailable
	}
	if !scheduleBound(row, cfg, digest) {
		return out, ErrScheduleConfig
	}
	now, e := scheduleNow(bounded, tx, clock)
	if e != nil {
		return out, e
	}
	out.ObservedAt = now
	if !scheduleClockValid(row, now) {
		out.State = "clock_invalid"
		return out, ErrScheduleUnavailable
	}
	out.State = row.state
	out.FailureClass = row.failure
	out.RetainedAttempts = uint32(row.retained)
	out.AttemptDurationMilliseconds = uint64(row.duration)
	if row.started.Valid {
		out.LastAttemptedAt = row.started.Time.UTC()
	}
	if row.finished.Valid {
		out.LastAttemptCompletedAt = row.finished.Time.UTC()
	}
	if row.active.Valid && (!row.lease.Valid || !row.lease.Time.After(now)) {
		out.State = "unknown"
		out.FailureClass = "lease_lost"
	}
	if row.success.Valid {
		var state string
		var snapshot, published time.Time
		var archive, manifest string
		e = tx.QueryRowContext(bounded, `SELECT state,snapshot_at,published_at,archive_version_id,manifest_version_id FROM public.amos_backup_attempts WHERE schedule_id=$1 AND attempt_id=$2`, row.id, row.success.UUID).Scan(&state, &snapshot, &published, &archive, &manifest)
		if e != nil || state != "completed" || !snapshot.Equal(row.snapshot.Time) || !published.Equal(row.published.Time) || len(archive) == 0 || len(archive) > 1024 || archive == "null" || len(manifest) == 0 || len(manifest) > 1024 || manifest == "null" {
			out.State = "unavailable"
			return out, ErrScheduleUnavailable
		}
		out.PublicationKnown = true
		out.LastPublishedAt = published.UTC()
		out.LastSnapshotAt = snapshot.UTC()
	}
	if tx.Commit() != nil {
		out.PublicationKnown = false
		out.State = "unavailable"
		return out, ErrScheduleUnavailable
	}
	return out, nil
}
func verifyScheduleMigration(ctx context.Context, tx *sql.Tx) error {
	f, e := migrations.BackupScheduling(1)
	if e != nil {
		return ErrScheduleUnavailable
	}
	reg, e := migrations.NewRegistry(f)
	if e != nil {
		return ErrScheduleUnavailable
	}
	m := reg.Migrations()[0]
	sum := sha256.Sum256([]byte(m.SQL))
	rows, e := tx.QueryContext(ctx, `SELECT sequence,migration_id,checksum FROM public.amos_schema_migrations ORDER BY sequence LIMIT 2`)
	if e != nil {
		return ErrScheduleUnavailable
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var seq int64
		var id string
		var checksum []byte
		if rows.Scan(&seq, &id, &checksum) != nil || seq != 1 || id != m.ID() || !bytes.Equal(sum[:], checksum) {
			return ErrScheduleUnavailable
		}
		count++
	}
	if rows.Err() != nil || count != 1 {
		return ErrScheduleUnavailable
	}
	return nil
}
