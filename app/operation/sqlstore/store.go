// Package sqlstore persists operation claims in the caller's transaction.
// It neither establishes authority nor permits disclosure of replayed output.
package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	ErrInvalid     = errors.New("invalid operation persistence input")
	ErrUnavailable = errors.New("operation persistence unavailable")
)

const maxResultBytes = 65536

var operationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

// Store retains an existing transaction runner without owning its pool.
type Store struct{ runner storage.TxRunner }

var _ operation.TransactionStore = (*Store)(nil)

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return NewWithRunner(&poolRunner{db: db})
}

func NewWithRunner(runner storage.TxRunner) (*Store, error) {
	if nilRunner(runner) {
		return nil, ErrInvalid
	}
	return &Store{runner: runner}, nil
}

func nilRunner(runner storage.TxRunner) bool {
	if runner == nil {
		return true
	}
	v := reflect.ValueOf(runner)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// WithTx delegates once at READ COMMITTED. It is not the authority Root runner.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if s == nil || nilRunner(s.runner) || ctx == nil || fn == nil {
		return ErrInvalid
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	var callbackErr error
	marker := new(callbackFailure)
	err := s.runner.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		if tx == nil {
			callbackErr = ErrUnavailable
			return marker
		}
		callbackErr = fn(tx)
		if callbackErr != nil {
			return marker
		}
		return nil
	})
	if err != nil {
		if callbackErr != nil && err == marker {
			return callbackErr
		}
		return ErrUnavailable
	}
	if callbackErr != nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}

// callbackFailure is unique per call and carries no caller diagnostic. Only
// its exact unchanged return proves the runner reported clean rollback.
type callbackFailure byte

func (*callbackFailure) Error() string { return "operation callback failed" }

type poolRunner struct{ db *sql.DB }

func (r *poolRunner) WithTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) (result error) {
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return ErrUnavailable
	}
	defer func() {
		// Rollback is also attempted on panic. It does not hide the panic or close
		// the caller's pool; ErrTxDone after a successful commit is expected.
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = ErrUnavailable
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func validID(id identity.ID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validScope(s operation.Scope) bool {
	return validID(s.InstallationID) && validID(s.ApplicationID) && validID(s.EnvironmentID) && validID(s.WorkspaceID) && validID(s.ActorID) && s.ActorKind == "person" && len(s.OperationID) >= 3 && len(s.OperationID) <= 120 && operationIDPattern.MatchString(s.OperationID)
}
func validRevision(v string) bool {
	if !utf8.ValidString(v) || len(v) < 1 || len(v) > 160 || strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validContract(c operation.ReplayContract) bool {
	return validRevision(c.OperationRevision) && c.DescriptorDigest != [32]byte{} && c.InputSchemaDigest != [32]byte{} && c.OutputSchemaDigest != [32]byte{}
}
func validResult(r operation.CachedResult) bool {
	switch r.Kind {
	case operation.ResultSucceeded, operation.ResultCreated, operation.ResultAccepted, operation.ResultNoContent:
	default:
		return false
	}
	return len(r.CanonicalJSON) >= 1 && len(r.CanonicalJSON) <= maxResultBytes && utf8.Valid(r.CanonicalJSON) && json.Valid(r.CanonicalJSON)
}
func (s *Store) transaction(ctx context.Context, tx *sql.Tx) error {
	if s == nil || nilRunner(s.runner) || ctx == nil || tx == nil {
		return ErrInvalid
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	var isolation string
	if err := tx.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		return ErrUnavailable
	}
	if isolation != "read committed" {
		return ErrInvalid
	}
	return nil
}

type capacity struct{ actorLimit, actorUsed, workspaceLimit, workspaceUsed int64 }

func lockCapacity(ctx context.Context, tx *sql.Tx, scope operation.Scope) (capacity, error) {
	var c capacity
	a := actorArgs(scope)
	if err := tx.QueryRowContext(ctx, actorCapacitySQL, a...).Scan(&c.actorLimit, &c.actorUsed); err != nil {
		return capacity{}, ErrUnavailable
	}
	if err := tx.QueryRowContext(ctx, workspaceCapacitySQL, append(a, scope.WorkspaceID)...).Scan(&c.workspaceLimit, &c.workspaceUsed); err != nil {
		return capacity{}, ErrUnavailable
	}
	if c.actorLimit < 1 || c.actorUsed < 0 || c.actorUsed > c.actorLimit || c.workspaceLimit < 1 || c.workspaceLimit > c.actorLimit || c.workspaceUsed < 0 || c.workspaceUsed > c.workspaceLimit || c.workspaceUsed > c.actorUsed {
		return capacity{}, ErrUnavailable
	}
	return c, nil
}
func actorArgs(s operation.Scope) []any {
	return []any{s.InstallationID, s.ApplicationID, s.EnvironmentID, s.ActorKind, s.ActorID}
}
func scopeArgs(s operation.Scope) []any { return append(actorArgs(s), s.WorkspaceID, s.OperationID) }

// ClaimTx locks actor, workspace, then the complete scoped key. Failure after a
// write requires rollback by the caller; this method never retries or commits.
func (s *Store) ClaimTx(ctx context.Context, tx *sql.Tx, id identity.ID, scope operation.Scope, key operation.KeyDigest, request operation.RequestHash, contract operation.ReplayContract, maximum int) (operation.Invocation, operation.ClaimKind, error) {
	if !validID(id) || !validScope(scope) || key == (operation.KeyDigest{}) || request == (operation.RequestHash{}) || !validContract(contract) || maximum < 1 || maximum > maxResultBytes {
		return operation.Invocation{}, "", ErrInvalid
	}
	if err := s.transaction(ctx, tx); err != nil {
		return operation.Invocation{}, "", err
	}
	c, err := lockCapacity(ctx, tx, scope)
	if err != nil {
		return operation.Invocation{}, "", err
	}
	stored, err := scanInvocation(tx.QueryRowContext(ctx, selectInvocation+` WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5 AND workspace_id=$6 AND operation_id=$7 AND key_digest=$8 FOR UPDATE`, append(scopeArgs(scope), key[:])...))
	if err == nil {
		// Corruption is unavailable even when the caller's binding differs.
		if !stored.completed() || stored.inv.Scope != scope || stored.key != key || stored.reserved > c.actorUsed || stored.reserved > c.workspaceUsed {
			return operation.Invocation{}, "", ErrUnavailable
		}
		if stored.inv.RequestHash != request || stored.inv.OperationRevision != contract.OperationRevision || stored.inv.DescriptorDigest != contract.DescriptorDigest || stored.inv.InputSchemaDigest != contract.InputSchemaDigest || stored.inv.OutputSchemaDigest != contract.OutputSchemaDigest {
			return operation.Invocation{}, operation.ClaimConflict, nil
		}
		stored.inv.Result.CanonicalJSON = append([]byte(nil), stored.inv.Result.CanonicalJSON...)
		return stored.inv, operation.ClaimReplay, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return operation.Invocation{}, "", ErrUnavailable
	}
	reservation := int64(maximum)
	if reservation > c.actorLimit-c.actorUsed || reservation > c.workspaceLimit-c.workspaceUsed {
		return operation.Invocation{}, operation.ClaimCapacityUnavailable, nil
	}
	if err := updateUsage(ctx, tx, scope, c.actorUsed+reservation, c.workspaceUsed+reservation); err != nil {
		return operation.Invocation{}, "", err
	}
	args := append([]any{id}, scopeArgs(scope)...)
	args = append(args, key[:], request[:], contract.OperationRevision, contract.DescriptorDigest[:], contract.InputSchemaDigest[:], contract.OutputSchemaDigest[:], reservation)
	if err := execOne(ctx, tx, insertInvocation, args...); err != nil {
		return operation.Invocation{}, "", err
	}
	return operation.Invocation{ID: id, Scope: scope, RequestHash: request, OperationRevision: contract.OperationRevision, DescriptorDigest: contract.DescriptorDigest, InputSchemaDigest: contract.InputSchemaDigest, OutputSchemaDigest: contract.OutputSchemaDigest}, operation.ClaimNew, nil
}

// CompleteTx completes only a pending claim in the same caller transaction.
// The deferred database guard prevents a pending claim from becoming visible in
// a later transaction. The trusted caller supplies transaction provenance.
func (s *Store) CompleteTx(ctx context.Context, tx *sql.Tx, id identity.ID, result operation.CachedResult) error {
	if !validID(id) || !validResult(result) {
		return ErrInvalid
	}
	if err := s.transaction(ctx, tx); err != nil {
		return err
	}
	stored, err := scanInvocation(tx.QueryRowContext(ctx, selectInvocation+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil || !stored.valid() {
		return ErrUnavailable
	}
	if stored.state != "pending" {
		return ErrInvalid
	}
	if stored.kind.Valid || stored.inv.Result.CanonicalJSON != nil || stored.digest != nil || stored.completedAt.Valid {
		return ErrUnavailable
	}
	if len(result.CanonicalJSON) > int(stored.reserved) {
		return ErrInvalid
	}
	c, err := lockCapacity(ctx, tx, stored.inv.Scope)
	if err != nil {
		return err
	}
	if stored.reserved > c.actorUsed || stored.reserved > c.workspaceUsed {
		return ErrUnavailable
	}
	// Both capacities were already locked by ClaimNew in this transaction.
	actual := int64(len(result.CanonicalJSON))
	release := stored.reserved - actual
	if err := updateUsage(ctx, tx, stored.inv.Scope, c.actorUsed-release, c.workspaceUsed-release); err != nil {
		return err
	}
	body := append([]byte(nil), result.CanonicalJSON...)
	digest := sha256.Sum256(body)
	return execOne(ctx, tx, completeInvocation, id, string(result.Kind), body, digest[:], actual)
}

func updateUsage(ctx context.Context, tx *sql.Tx, scope operation.Scope, actor, workspace int64) error {
	if err := execOne(ctx, tx, updateActorSQL, append(actorArgs(scope), actor)...); err != nil {
		return err
	}
	return execOne(ctx, tx, updateWorkspaceSQL, append(append(actorArgs(scope), scope.WorkspaceID), workspace)...)
}
func execOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return ErrUnavailable
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return ErrUnavailable
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }
type storedInvocation struct {
	inv                    operation.Invocation
	key                    operation.KeyDigest
	reserved               int64
	state                  string
	kind                   sql.NullString
	digest                 []byte
	createdAt, completedAt sql.NullTime
}

func scanInvocation(row rowScanner) (storedInvocation, error) {
	var s storedInvocation
	var key, request, descriptor, input, output []byte
	err := row.Scan(&s.inv.ID, &s.inv.Scope.InstallationID, &s.inv.Scope.ApplicationID, &s.inv.Scope.EnvironmentID, &s.inv.Scope.ActorKind, &s.inv.Scope.ActorID, &s.inv.Scope.WorkspaceID, &s.inv.Scope.OperationID, &key, &request, &s.inv.OperationRevision, &descriptor, &input, &output, &s.reserved, &s.state, &s.kind, &s.inv.Result.CanonicalJSON, &s.digest, &s.createdAt, &s.completedAt)
	if err != nil {
		return storedInvocation{}, err
	}
	for _, digest := range [][]byte{key, request, descriptor, input, output} {
		if len(digest) != 32 {
			return storedInvocation{}, ErrUnavailable
		}
	}
	copy(s.key[:], key)
	copy(s.inv.RequestHash[:], request)
	copy(s.inv.DescriptorDigest[:], descriptor)
	copy(s.inv.InputSchemaDigest[:], input)
	copy(s.inv.OutputSchemaDigest[:], output)
	s.inv.Result.Kind = operation.ResultKind(s.kind.String)
	return s, nil
}
func (s storedInvocation) valid() bool {
	return validID(s.inv.ID) && validScope(s.inv.Scope) && s.key != (operation.KeyDigest{}) && s.inv.RequestHash != (operation.RequestHash{}) && validContract(operation.ReplayContract{OperationRevision: s.inv.OperationRevision, DescriptorDigest: s.inv.DescriptorDigest, InputSchemaDigest: s.inv.InputSchemaDigest, OutputSchemaDigest: s.inv.OutputSchemaDigest}) && s.reserved >= 1 && s.reserved <= maxResultBytes && s.createdAt.Valid && !s.createdAt.Time.IsZero()
}
func (s *storedInvocation) completed() bool {
	if !s.valid() || s.state != "completed" || !s.kind.Valid || !s.completedAt.Valid || s.completedAt.Time.IsZero() || !validResult(s.inv.Result) || int64(len(s.inv.Result.CanonicalJSON)) != s.reserved || len(s.digest) != 32 {
		return false
	}
	copy(s.inv.ResultSHA256[:], s.digest)
	return s.inv.ResultSHA256 == sha256.Sum256(s.inv.Result.CanonicalJSON)
}
