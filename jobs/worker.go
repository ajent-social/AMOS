package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Repository is the narrow durable boundary used by a single consumer worker.
type Repository interface {
	Claim(context.Context, string, time.Duration) (Job, bool, error)
	Resolve(context.Context, Job, string, Resolution) error
}

// Consumer separates initial execution from reconciliation. It receives the
// immutable job UUID as its provider idempotency key for every execution attempt.
type Consumer interface {
	Execute(context.Context, Job, uuid.UUID) Resolution
	Reconcile(context.Context, Job, uuid.UUID) Resolution
}

type Worker struct {
	Repository Repository
	Consumer   Consumer
	Owner      string
	Lease      time.Duration
	Observer   Observer
}

// RunOne processes one available job. A lost response must return
// ResolutionUnknown so a later lease invokes Reconcile instead of Execute.
func (w Worker) RunOne(ctx context.Context) (processed bool, result error) {
	started := time.Now()
	event := OutcomeInvalid
	defer func() {
		if p := recover(); p != nil {
			observe(w.Observer, Event{Outcome: OutcomePanic, Elapsed: time.Since(started)})
			panic(p)
		}
		if ctx != nil && ctx.Err() != nil {
			event = OutcomeCanceled
		}
		observe(w.Observer, Event{Outcome: event, Elapsed: time.Since(started)})
	}()
	if ctx == nil || w.Repository == nil || w.Consumer == nil || w.Owner == "" || w.Lease <= 0 {
		return false, ErrInvalidIntent
	}
	job, ok, err := w.Repository.Claim(ctx, w.Owner, w.Lease)
	if err != nil || !ok {
		event = OutcomeIdle
		if err != nil {
			event = OutcomeClaimFailed
		}
		return ok, err
	}
	var outcome Resolution
	switch job.Action {
	case ActionExecute:
		outcome = w.Consumer.Execute(ctx, job, job.ID)
	case ActionReconcile:
		outcome = w.Consumer.Reconcile(ctx, job, job.ID)
	default:
		return true, ErrInvalidResolution
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if err := ValidateResolution(outcome); err != nil {
		return true, err
	}
	if outcome.Kind == ResolutionUnknown && !job.ExternalEffect {
		return true, errors.Join(ErrInvalidResolution, errors.New("non-external job cannot have an unknown remote outcome"))
	}
	if err := w.Repository.Resolve(ctx, job, w.Owner, outcome); err != nil {
		event = OutcomeResolveFailed
		return true, err
	}
	switch outcome.Kind {
	case ResolutionSucceeded:
		event = OutcomeSucceeded
	case ResolutionTerminal:
		event = OutcomeDead
	case ResolutionUnknown:
		event = OutcomeUnknown
	default:
		event = OutcomeRetry
		if job.Attempt >= job.MaxAttempts {
			event = OutcomeDead
		}
	}
	return true, nil
}
