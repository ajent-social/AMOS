package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type workerDouble struct {
	current  Job
	claimed  bool
	resolved []Resolution
}

func (d *workerDouble) Claim(context.Context, string, time.Duration) (Job, bool, error) {
	if d.claimed {
		return Job{}, false, nil
	}
	d.claimed = true
	return d.current, true, nil
}
func (d *workerDouble) Resolve(_ context.Context, j Job, _ string, r Resolution) error {
	d.resolved = append(d.resolved, r)
	return nil
}

type consumerDouble struct {
	executed, reconciled int
	key                  uuid.UUID
}

func (d *consumerDouble) Execute(_ context.Context, j Job, key uuid.UUID) Resolution {
	d.executed++
	d.key = key
	return Resolution{Kind: ResolutionUnknown}
}
func (d *consumerDouble) Reconcile(_ context.Context, j Job, key uuid.UUID) Resolution {
	d.reconciled++
	d.key = key
	return Resolution{Kind: ResolutionSucceeded}
}

func TestWorkerUsesReconciliationAfterLostProviderResponse(t *testing.T) {
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	db := &workerDouble{current: Job{ID: id, ExternalEffect: true, Action: ActionExecute}}
	provider := &consumerDouble{}
	worker := Worker{Repository: db, Consumer: provider, Owner: "worker-a", Lease: time.Second}
	processed, err := worker.RunOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("first process=%v err=%v", processed, err)
	}
	if provider.executed != 1 || provider.reconciled != 0 || provider.key != id {
		t.Fatalf("initial consumer calls execute=%d reconcile=%d key=%s", provider.executed, provider.reconciled, provider.key)
	}
	if got := db.resolved[0].Kind; got != ResolutionUnknown {
		t.Fatalf("lost response resolution=%s", got)
	}
	db.claimed = false
	db.current.Action = ActionReconcile
	db.current.State = StateUnknown
	_, err = worker.RunOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.executed != 1 || provider.reconciled != 1 || provider.key != id {
		t.Fatalf("reconciliation replayed side effect: execute=%d reconcile=%d", provider.executed, provider.reconciled)
	}
}
