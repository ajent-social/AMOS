// Package jobs defines bounded durable intent and consumer outcomes.
package jobs

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	ErrInvalidIntent       = errors.New("invalid job intent")
	ErrIdempotencyConflict = errors.New("idempotency key was used with a different request")
	ErrNotFound            = errors.New("job not found")
	ErrLeaseLost           = errors.New("job lease was lost")
	ErrExpired             = errors.New("job deadline expired")
	ErrInvalidResolution   = errors.New("invalid job resolution")
)

type State string

const (
	StateQueued    State = "queued"
	StateLeased    State = "leased"
	StateSucceeded State = "succeeded"
	StateDead      State = "dead"
	StateUnknown   State = "unknown"
)

type Action string

const (
	ActionExecute   Action = "execute"
	ActionReconcile Action = "reconcile"
)

type Intent struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	Key            string
	Kind           string
	Payload        []byte
	ExternalEffect bool
	Deadline       time.Time
}

type Job struct {
	ID                        uuid.UUID
	InstallationID            uuid.UUID
	ApplicationID             uuid.UUID
	Key                       string
	Kind                      string
	Payload                   []byte
	ExternalEffect            bool
	RequestHash               [32]byte
	State                     State
	Action                    Action
	Attempt                   int
	MaxAttempts               int
	ReconciliationAttempt     int
	MaxReconciliationAttempts int
	Deadline                  time.Time
	LeaseOwner                string
	FenceToken                int64
	LeaseUntil                time.Time
	ManualReview              bool
	CreatedAt                 time.Time
}

type ResolutionKind string

const (
	ResolutionSucceeded ResolutionKind = "succeeded"
	ResolutionRetrySafe ResolutionKind = "retry_safe"
	ResolutionUnknown   ResolutionKind = "unknown"
	ResolutionTerminal  ResolutionKind = "terminal"
	ResolutionNoEffect  ResolutionKind = "no_effect"
)

type Resolution struct {
	Kind       ResolutionKind
	RetryAfter time.Duration
}

func ValidateIntent(in Intent, maxPayload int) error {
	if in.InstallationID == uuid.Nil || in.ApplicationID == uuid.Nil || in.Key == "" || len(in.Key) > 256 || strings.TrimSpace(in.Key) != in.Key || strings.IndexFunc(in.Key, unicode.IsControl) >= 0 || in.Kind == "" || len(in.Kind) > 128 || strings.TrimSpace(in.Kind) != in.Kind || strings.IndexFunc(in.Kind, unicode.IsControl) >= 0 || len(in.Payload) == 0 || maxPayload <= 0 || len(in.Payload) > maxPayload || in.Deadline.IsZero() {
		return ErrInvalidIntent
	}
	if !jsonPayload(in.Payload) {
		return ErrInvalidIntent
	}
	return nil
}

func RequestHash(in Intent) [32]byte {
	request := make([]byte, 0, len(in.Kind)+len(in.Payload)+32)
	request = append(request, in.Kind...)
	request = append(request, 0)
	request = append(request, in.Payload...)
	request = append(request, 0)
	request = strconv.AppendBool(request, in.ExternalEffect)
	request = append(request, 0)
	request = strconv.AppendInt(request, in.Deadline.UTC().UnixMicro(), 10)
	return sha256.Sum256(request)
}
func jsonPayload(value []byte) bool { return json.Valid(value) }

func ValidateResolution(r Resolution) error {
	if r.RetryAfter < 0 {
		return ErrInvalidResolution
	}
	switch r.Kind {
	case ResolutionSucceeded, ResolutionRetrySafe, ResolutionUnknown, ResolutionTerminal, ResolutionNoEffect:
		return nil
	default:
		return fmt.Errorf("%w: unsupported outcome", ErrInvalidResolution)
	}
}
