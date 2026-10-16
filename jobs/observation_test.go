package jobs

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type observationRepo struct {
	job                  Job
	found                bool
	claimErr, resolveErr error
}

func (r observationRepo) Claim(context.Context, string, time.Duration) (Job, bool, error) {
	return r.job, r.found, r.claimErr
}
func (r observationRepo) Resolve(context.Context, Job, string, Resolution) error { return r.resolveErr }

type observationConsumer struct {
	resolution Resolution
	panicValue any
}

func (c observationConsumer) Execute(context.Context, Job, uuid.UUID) Resolution {
	if c.panicValue != nil {
		panic(c.panicValue)
	}
	return c.resolution
}
func (c observationConsumer) Reconcile(ctx context.Context, j Job, id uuid.UUID) Resolution {
	return c.Execute(ctx, j, id)
}
func TestMetricsWorkerOutcomesAndIsolation(t *testing.T) {
	sentinel := errors.New("private consumer error")
	for _, tt := range []struct {
		name    string
		repo    observationRepo
		res     Resolution
		want    Outcome
		wantErr bool
	}{
		{"idle", observationRepo{}, Resolution{}, OutcomeIdle, false},
		{"claim", observationRepo{claimErr: sentinel}, Resolution{}, OutcomeClaimFailed, true},
		{"resolve", observationRepo{job: Job{Action: ActionExecute}, found: true, resolveErr: sentinel}, Resolution{Kind: ResolutionSucceeded}, OutcomeResolveFailed, true},
		{"success", observationRepo{job: Job{Action: ActionExecute}, found: true}, Resolution{Kind: ResolutionSucceeded}, OutcomeSucceeded, false},
		{"retry", observationRepo{job: Job{Action: ActionExecute, Attempt: 1, MaxAttempts: 3}, found: true}, Resolution{Kind: ResolutionRetrySafe}, OutcomeRetry, false},
		{"dead", observationRepo{job: Job{Action: ActionExecute}, found: true}, Resolution{Kind: ResolutionTerminal}, OutcomeDead, false},
		{"exhausted", observationRepo{job: Job{Action: ActionExecute, Attempt: 3, MaxAttempts: 3}, found: true}, Resolution{Kind: ResolutionRetrySafe}, OutcomeDead, false},
		{"unknown", observationRepo{job: Job{Action: ActionExecute, ExternalEffect: true}, found: true}, Resolution{Kind: ResolutionUnknown}, OutcomeUnknown, false},
		{"invalid", observationRepo{job: Job{Action: ActionExecute}, found: true}, Resolution{Kind: "private"}, OutcomeInvalid, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, panicObserver := range []bool{false, true} {
				var got Event
				w := Worker{Repository: tt.repo, Consumer: observationConsumer{resolution: tt.res}, Owner: "private-owner", Lease: time.Second, Observer: func(e Event) {
					got = e
					if panicObserver {
						panic("observer failure")
					}
				}}
				_, err := w.RunOne(context.Background())
				if (err != nil) != tt.wantErr || got.Outcome != tt.want || got.Elapsed < 0 {
					t.Fatalf("event %+v err %v", got, err)
				}
				w.Observer = nil
				_, nilErr := w.RunOne(context.Background())
				if (nilErr != nil) != (err != nil) {
					t.Fatal("observer changed result")
				}
			}
		})
	}
	var got Event
	w := Worker{Observer: func(e Event) { got = e }}
	_, _ = w.RunOne(context.Background())
	if got.Outcome != OutcomeInvalid {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = Worker{Repository: observationRepo{claimErr: ctx.Err()}, Consumer: observationConsumer{}, Owner: "worker", Lease: time.Second, Observer: func(e Event) { got = e }}
	_, _ = w.RunOne(ctx)
	if got.Outcome != OutcomeCanceled {
		t.Fatal(got)
	}
	marker := &struct{}{}
	w = Worker{Repository: observationRepo{job: Job{Action: ActionExecute}, found: true}, Consumer: observationConsumer{panicValue: marker}, Owner: "worker", Lease: time.Second, Observer: func(e Event) { got = e; panic("observer panic") }}
	func() {
		defer func() {
			if p := recover(); p != marker {
				t.Fatalf("panic identity changed: %v", p)
			}
		}()
		_, _ = w.RunOne(context.Background())
		t.Fatal("consumer panic swallowed")
	}()
	if got.Outcome != OutcomePanic {
		t.Fatal(got)
	}
}
