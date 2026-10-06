// Package policy defines the shared operation decision contract.
//
// These types do not evaluate or grant authority. An installation must supply
// a qualified evaluator and current-state transaction adapter before operation
// execution is enabled. Missing dependencies must fail unavailable.
package policy

import (
	"context"
	"time"

	"github.com/ajent-social/amos/identity"
)

// Decision distinguishes permission denial from unavailable policy state.
// Only the three declared result types implement this contract.
type Decision interface{ isDecision() }

// Resource identifies a resource within the trusted selected workspace.
type Resource struct {
	Type        string
	ID          identity.ID
	WorkspaceID identity.ID
}

// Allowed describes a current decision, never a reusable authorization grant.
// Consumers must recheck authority and expiry within the invocation transaction.
type Allowed struct {
	PolicyRevision string
	EvaluatedAt    time.Time
	ExpiresAt      time.Time
}

func (Allowed) isDecision() {}

// Denied describes an established policy denial with a safe, stable code.
type Denied struct {
	Code           string
	PolicyRevision string
	EvaluatedAt    time.Time
}

func (Denied) isDecision() {}

// Unavailable means a current decision could not be established. It must not
// be converted to either permission or a definite policy denial.
type Unavailable struct {
	Code       string
	Retryable  bool
	RetryAfter time.Duration
}

func (Unavailable) isDecision() {}

// MCPExposure is the frozen operation exposure classification.
type MCPExposure string

const (
	MCPNever     MCPExposure = "never"
	MCPEligible  MCPExposure = "eligible"
	MCPChallenge MCPExposure = "challenge"
)

// Requirements are declarative metadata, not evidence of permission. Registry
// construction validates and copies these slices before accepting a definition.
type Requirements struct {
	Permissions  []string
	Entitlements []string
	Assurance    identity.AssuranceLevel
	MCPExposure  MCPExposure
}

// Evaluator consumes a verified principal and current authoritative facts
// supplied by the owning transaction adapter. No implementation or default
// allow behavior is provided by this package.
type Evaluator interface {
	Evaluate(context.Context, identity.Principal, Resource, Requirements) Decision
}
