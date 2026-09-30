package receipt

import (
	"time"

	"github.com/google/uuid"
)

type State uint8

const (
	Unknown State = iota
	Accepted
	Rejected
)

// Receipt has no exported fields so callers cannot alter a provider outcome.
type Receipt struct {
	state      State
	messageID  string
	code       string
	retryable  bool
	retryAfter time.Duration
}

type VerifiedEvent struct {
	jobID    uuid.UUID
	receipt  Receipt
	verified bool
}

func (r Receipt) State() State              { return r.state }
func (r Receipt) MessageID() string         { return r.messageID }
func (r Receipt) Code() string              { return r.code }
func (r Receipt) Retryable() bool           { return r.retryable }
func (r Receipt) RetryAfter() time.Duration { return r.retryAfter }

func (e VerifiedEvent) JobID() uuid.UUID { return e.jobID }
func (e VerifiedEvent) Receipt() Receipt { return e.receipt }
func (e VerifiedEvent) Verified() bool   { return e.verified }

func UnknownResult(code string) Receipt { return Receipt{state: Unknown, code: code} }
func AcceptedResult(messageID string) Receipt {
	if messageID == "" {
		return UnknownResult("provider.receipt_missing")
	}
	return Receipt{state: Accepted, messageID: messageID}
}
func RejectedResult(code string, retryable bool, retryAfter time.Duration) Receipt {
	return Receipt{state: Rejected, code: code, retryable: retryable, retryAfter: retryAfter}
}

// VerifiedProviderEvent can only be constructed inside the email delivery tree.
func VerifiedProviderEvent(jobID uuid.UUID, result Receipt, verified bool) VerifiedEvent {
	return VerifiedEvent{jobID: jobID, receipt: result, verified: verified}
}
