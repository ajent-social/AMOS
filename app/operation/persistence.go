package operation

import (
	"context"
	"database/sql"

	"github.com/ajent-social/amos/audit"
	"github.com/ajent-social/amos/identity"
)

// AuditOutcome preserves the existing finite audit vocabulary and type identity.
type AuditOutcome = audit.Outcome

const (
	AuditSucceeded   = audit.OutcomeSucceeded
	AuditDenied      = audit.OutcomeDenied
	AuditUnavailable = audit.OutcomeUnavailable
)

// InvocationAuditWriter persists finite attribution in the owner's transaction.
// It neither authorizes the attempted operation nor commits the event.
type InvocationAuditWriter interface {
	AppendInvocationTx(context.Context, *sql.Tx, identity.ID, string, AuditOutcome, identity.ID) error
}

// Scope is a persistence selector. It must be derived by the executor after
// current-authority checks, never accepted as request-provided authority.
type Scope struct {
	InstallationID, ApplicationID, EnvironmentID, WorkspaceID identity.ID
	ActorKind                                                 string
	ActorID                                                   identity.ID
	OperationID                                               string
}

type KeyDigest [32]byte
type RequestHash [32]byte

type ReplayContract struct {
	OperationRevision                                       string
	DescriptorDigest, InputSchemaDigest, OutputSchemaDigest [32]byte
}

type Invocation struct {
	ID                                                                    identity.ID
	Scope                                                                 Scope
	RequestHash                                                           RequestHash
	OperationRevision                                                     string
	DescriptorDigest, InputSchemaDigest, OutputSchemaDigest, ResultSHA256 [32]byte
	Result                                                                CachedResult
}

type ClaimKind string

const (
	ClaimNew                 ClaimKind = "new"
	ClaimReplay              ClaimKind = "replay"
	ClaimConflict            ClaimKind = "conflict"
	ClaimCapacityUnavailable ClaimKind = "capacity_unavailable"
)

// TransactionStore is storage mechanics only. Authenticated execution retains
// the current-authority Root transaction and passes that same transaction to
// ClaimTx and CompleteTx; WithTx is not an authority runner or dispatch API.
// Replay results remain private until current authority and codecs are checked.
type TransactionStore interface {
	WithTx(context.Context, func(*sql.Tx) error) error
	ClaimTx(context.Context, *sql.Tx, identity.ID, Scope, KeyDigest, RequestHash, ReplayContract, int) (Invocation, ClaimKind, error)
	CompleteTx(context.Context, *sql.Tx, identity.ID, CachedResult) error
}
