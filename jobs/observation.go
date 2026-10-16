package jobs

import "time"

// Outcome is a finite execution result; it contains no job or error material.
type Outcome string

const (
	OutcomeIdle          Outcome = "idle"
	OutcomeInvalid       Outcome = "invalid"
	OutcomeCanceled      Outcome = "canceled"
	OutcomeClaimFailed   Outcome = "claim_failed"
	OutcomeResolveFailed Outcome = "resolve_failed"
	OutcomeSucceeded     Outcome = "succeeded"
	OutcomeRetry         Outcome = "retry"
	OutcomeDead          Outcome = "dead"
	OutcomeUnknown       Outcome = "unknown"
	OutcomePanic         Outcome = "panic"
)

type Event struct {
	Outcome Outcome
	Elapsed time.Duration
}

// Observer is invoked synchronously. It must not perform external I/O. Panics are isolated.
type Observer func(Event)

// Snapshot is a read-only retained-row gauge, not lifetime telemetry.
type Snapshot struct {
	ObservedAt                                   time.Time
	Queued, Unknown, Leased, RetryAttempts, Dead uint64
	OldestQueuedAge                              time.Duration
	LastSuccess                                  time.Time
	LastSuccessKnown                             bool
}

func observe(observer Observer, e Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(e)
}
