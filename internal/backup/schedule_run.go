package backup

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Scheduler) preflightDump(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, e := pgx.Connect(c, s.dumpURL)
	if e != nil {
		return ErrScheduleUnavailable
	}
	defer func() { _ = conn.Close(c) }()
	q := func(ctx context.Context, sql string, args ...any) scheduleScanner {
		return conn.QueryRow(ctx, sql, args...)
	}
	return checkScheduleIdentity(c, q, s.cfg.Database.DumpDatabase, s.cfg.Database.DumpRole)
}

// RunDue creates at most one currently-authorized attempt. PostgreSQL fencing
// cannot retract a remote PUT already in flight; only fenced completion can
// advance freshness, and a failed slot/attempt ID is never replayed.
func (s *Scheduler) RunDue(ctx context.Context) (ScheduleResult, error) {
	if s == nil || ctx == nil || !s.cfg.Targets.Activated {
		return "", ErrScheduleConfig
	}
	if ValidateScheduleDatabaseURL(s.cfg.Database, ScheduleDump, s.dumpURL) != nil {
		return "", ErrScheduleConfig
	}
	authority, ok := s.store.writer.(scheduleRuntimeAuthority)
	if !ok || authority.validateScheduleRuntime(s.cfg.AWS, s.cfg.RunTimeout+15*time.Second) != nil {
		return "", ErrScheduleUnavailable
	}
	if e := s.preflightDump(ctx); e != nil {
		return "", e
	}
	capacity, err := s.stagingCapacity(ctx)
	if err != nil {
		return "", err
	}
	if authority.validateScheduleRuntime(s.cfg.AWS, s.cfg.RunTimeout+15*time.Second) != nil {
		return "", ErrScheduleUnavailable
	}
	lease, result, err := s.admit(ctx, !capacity)
	if err != nil || result != "" {
		return result, err
	}
	runCtx, cancel := context.WithTimeout(ctx, s.cfg.RunTimeout)
	defer cancel()
	started := time.Now()
	var lost atomic.Bool
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if s.checkLease(runCtx, lease) != nil {
					lost.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-monitorDone }()
	fail := func(class string, unknown bool) (ScheduleResult, error) {
		if lost.Load() {
			class = "lease_lost"
			unknown = true
		} else if runCtx.Err() != nil {
			class = "cancelled"
		}
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if s.finish(cleanup, lease, nil, class, time.Since(started)) != nil {
			return ScheduleUnknown, ErrScheduleUnavailable
		}
		if unknown {
			return ScheduleUnknown, ErrScheduleFailed
		}
		return ScheduleFailed, ErrScheduleFailed
	}
	if s.checkLease(runCtx, lease) != nil {
		return fail("lease_lost", true)
	}
	dumpCtx, stopDump := context.WithTimeout(runCtx, s.cfg.Create.DumpTimeout)
	manifest, err := Create(dumpCtx, Request{ID: lease.id.String(), DatabaseURL: s.dumpURL, DestinationDir: s.cfg.Create.StagingDir, PGDumpPath: s.cfg.Create.PGDumpPath, PGDumpVersion: s.cfg.Create.PGDumpVersion, ModuleVersions: s.cfg.Create.ModuleVersions, MaxBytes: s.cfg.Create.MaxBytes, Timeout: s.cfg.Create.DumpTimeout, IsolatedClientEnvironment: true})
	stopDump()
	if err != nil {
		class := "dump"
		if errors.Is(err, ErrCanceled) {
			class = "cancelled"
		}
		return fail(class, false)
	}
	if s.checkLease(runCtx, lease) != nil {
		return fail("lease_lost", true)
	}
	uploadCtx, stopUpload := context.WithTimeout(runCtx, s.cfg.UploadTimeout)
	stored, err := s.store.Upload(uploadCtx, filepath.Join(s.cfg.Create.StagingDir, "backup-"+lease.id.String()), manifest)
	uploadCancelled := uploadCtx.Err() != nil
	stopUpload()
	if err != nil {
		class := "upload"
		unknown := true
		if errors.Is(err, ErrIntegrityMismatch) {
			class = "integrity"
			unknown = false
		}
		if uploadCancelled {
			class = "cancelled"
		}
		return fail(class, unknown)
	}
	if s.finish(runCtx, lease, &stored, "", time.Since(started)) != nil {
		return ScheduleUnknown, ErrScheduleUnavailable
	}
	return ScheduleCompleted, nil
}
