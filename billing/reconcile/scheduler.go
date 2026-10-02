package reconcile

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ajent-social/amos/jobs"
	"github.com/google/uuid"
)

const (
	maxSchedulerEndpoints = 64
	maxSchedulerWork      = 100
)

// SchedulerConfig bounds each service pass so busy queues cannot prevent
// checkpointed account scans from making progress.
type SchedulerConfig struct {
	Database                  TxRunner
	WebhookWorker             jobs.Worker
	Reconciler                *Reconciler
	Endpoints                 []ScanScope
	PollInterval              time.Duration
	ScanInterval              time.Duration
	ScanPageSize              int
	MaxWebhookJobsPerPass     int
	MaxReconciliationsPerPass int
	MaxScanPagesPerPass       int
	OnError                   func(error)
}

// Scheduler owns the service lifecycle for verified webhook jobs, dirty
// customer reconciliation and resumable periodic scans.
type Scheduler struct {
	db             TxRunner
	webhookWorker  jobs.Worker
	reconciler     *Reconciler
	endpoints      []ScanScope
	checkpointIDs  map[ScanScope]uuid.UUID
	pollInterval   time.Duration
	scanInterval   time.Duration
	scanPageSize   int
	maxWebhookJobs int
	maxReconcile   int
	maxScanPages   int
	onError        func(error)
	nextScan       time.Time
	scanCursor     int
	runGate        chan struct{}
}

// NewScheduler validates and copies all endpoint scopes. Each endpoint's scan
// checkpoint is allocated once per process and then resumed from PostgreSQL.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Database == nil || cfg.Reconciler == nil || cfg.WebhookWorker.Repository == nil || cfg.WebhookWorker.Consumer == nil || cfg.WebhookWorker.Owner == "" || cfg.WebhookWorker.Lease <= 0 ||
		len(cfg.Endpoints) == 0 || len(cfg.Endpoints) > maxSchedulerEndpoints || cfg.PollInterval <= 0 || cfg.PollInterval > time.Minute || cfg.ScanInterval < cfg.PollInterval || cfg.ScanInterval > 24*time.Hour || cfg.ScanPageSize < 1 || cfg.ScanPageSize > 500 ||
		cfg.MaxWebhookJobsPerPass < 1 || cfg.MaxWebhookJobsPerPass > maxSchedulerWork || cfg.MaxReconciliationsPerPass < 1 || cfg.MaxReconciliationsPerPass > maxSchedulerWork || cfg.MaxScanPagesPerPass < len(cfg.Endpoints) || cfg.MaxScanPagesPerPass > maxSchedulerEndpoints || cfg.OnError == nil {
		return nil, ErrInvalidInput
	}
	endpoints := append([]ScanScope(nil), cfg.Endpoints...)
	checkpoints := make(map[ScanScope]uuid.UUID, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.valid() {
			return nil, ErrInvalidInput
		}
		if _, duplicate := checkpoints[endpoint]; duplicate {
			return nil, ErrInvalidInput
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		checkpoints[endpoint] = id
	}
	return &Scheduler{
		db: cfg.Database, webhookWorker: cfg.WebhookWorker, reconciler: cfg.Reconciler,
		endpoints: endpoints, checkpointIDs: checkpoints, pollInterval: cfg.PollInterval,
		scanInterval: cfg.ScanInterval, scanPageSize: cfg.ScanPageSize,
		maxWebhookJobs: cfg.MaxWebhookJobsPerPass, maxReconcile: cfg.MaxReconciliationsPerPass,
		maxScanPages: cfg.MaxScanPagesPerPass, onError: cfg.OnError, runGate: make(chan struct{}, 1),
	}, nil
}

// Run continuously schedules bounded service passes until its context ends.
// Operational errors are reported to OnError after durable retry handling,
// then the scheduler waits before continuing.
func (s *Scheduler) Run(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrInvalidInput
	}
	for {
		worked, err := s.RunOne(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			s.onError(err)
		}
		if !worked || err != nil {
			timer := time.NewTimer(s.pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

// RunOne executes one bounded scheduling pass. It is exposed so hosts can
// integrate the scheduler with their own lifecycle and health reporting.
func (s *Scheduler) RunOne(ctx context.Context) (bool, error) {
	if s == nil || ctx == nil {
		return false, ErrInvalidInput
	}
	select {
	case s.runGate <- struct{}{}:
		defer func() { <-s.runGate }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	worked, err := s.runScanPages(ctx, time.Now())
	if err != nil {
		return worked, err
	}
	for i := 0; i < s.maxWebhookJobs; i++ {
		didWork, err := s.webhookWorker.RunOne(ctx)
		if err != nil {
			return worked || didWork, err
		}
		if !didWork {
			break
		}
		worked = true
	}
	for i := 0; i < s.maxReconcile; i++ {
		didWork, err := s.reconciler.RunOne(ctx)
		if err != nil {
			return worked || didWork, err
		}
		if !didWork {
			break
		}
		worked = true
	}
	return worked, nil
}

func (s *Scheduler) runScanPages(ctx context.Context, now time.Time) (bool, error) {
	if s.nextScan.After(now) {
		return false, nil
	}
	s.nextScan = now.Add(s.scanInterval)
	for i := 0; i < s.maxScanPages; i++ {
		index := s.scanCursor
		s.scanCursor = (s.scanCursor + 1) % len(s.endpoints)
		endpoint := s.endpoints[index]
		checkpointID := s.checkpointIDs[endpoint]
		err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			store, err := NewStore(tx)
			if err != nil {
				return err
			}
			_, err = store.ScanPage(ctx, checkpointID, endpoint, s.scanPageSize)
			return err
		})
		if err != nil {
			return true, fmt.Errorf("run checkpointed billing scan: %w", err)
		}
	}
	return true, nil
}
