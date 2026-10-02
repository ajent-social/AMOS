package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/jobs"
)

func TestT5_8_SchedulerScansAndReconcilesOnlyConfiguredBinding(t *testing.T) {
	fixture := newWebhookJobsFixture(t)
	now := time.Now().UTC()
	source := &testSnapshotSource{snapshot: snapshot(fixture.binding, "cus_current", "rev-scheduler", now, Subscription{
		Reference: "sub_scheduled", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1,
		PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(24 * time.Hour),
	})}
	reconciler, err := New(fixture.db, source, workerConfig())
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewScheduler(SchedulerConfig{
		Database: fixture.db, WebhookWorker: jobs.Worker{Repository: fixture.repository, Consumer: fixture.consumer, Owner: "billing-scheduler-test", Lease: time.Minute},
		Reconciler: reconciler, Endpoints: []ScanScope{fixture.scope}, PollInterval: time.Second, ScanInterval: time.Hour,
		ScanPageSize: 10, MaxWebhookJobsPerPass: 4, MaxReconciliationsPerPass: 4, MaxScanPagesPerPass: 1,
		OnError: func(err error) { t.Errorf("scheduler reported unexpected operational error: %v", err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := scheduler.RunOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("bounded pass should scan and reconcile the configured endpoint: worked=%v err=%v", worked, err)
	}
	if source.calls != 1 || source.last.Binding != fixture.binding || source.last.CustomerRef != "cus_current" {
		t.Fatalf("provider request escaped configured customer binding: calls=%d request=%#v", source.calls, source.last)
	}
	worked, err = scheduler.RunOne(context.Background())
	if err != nil || worked {
		t.Fatalf("idle bounded pass should not claim new work: worked=%v err=%v", worked, err)
	}
	var gotState string
	if err := fixture.db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT state FROM billing_subscription_projections WHERE billing_account_id=$1 AND subscription_ref='sub_scheduled'`, fixture.account).Scan(&gotState)
	}); err != nil {
		t.Fatal(err)
	}
	if gotState != "confirmed" {
		t.Fatalf("scheduled projection state=%q, want confirmed", gotState)
	}
}

func TestT5_8_SchedulerRejectsUnboundedOrUnobservedConfiguration(t *testing.T) {
	fixture := newWebhookJobsFixture(t)
	reconciler, err := New(fixture.db, &testSnapshotSource{}, workerConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := SchedulerConfig{
		Database: fixture.db, WebhookWorker: jobs.Worker{Repository: fixture.repository, Consumer: fixture.consumer, Owner: "billing-scheduler-test", Lease: time.Minute},
		Reconciler: reconciler, Endpoints: []ScanScope{fixture.scope}, PollInterval: time.Second, ScanInterval: time.Minute,
		ScanPageSize: 10, MaxWebhookJobsPerPass: 1, MaxReconciliationsPerPass: 1, MaxScanPagesPerPass: 1,
		OnError: func(error) {},
	}
	cases := []struct {
		name   string
		mutate func(*SchedulerConfig)
	}{
		{name: "missing error observer", mutate: func(cfg *SchedulerConfig) { cfg.OnError = nil }},
		{name: "excessive page size", mutate: func(cfg *SchedulerConfig) { cfg.ScanPageSize = 501 }},
		{name: "unbounded endpoint set", mutate: func(cfg *SchedulerConfig) { cfg.MaxScanPagesPerPass = maxSchedulerEndpoints + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if _, err := NewScheduler(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("configuration error=%v, want ErrInvalidInput", err)
			}
		})
	}
}
