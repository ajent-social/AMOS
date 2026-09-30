// Package sqlstore persists audit events. The table is provisioned by the
// integrator's ordered migration registry; this package never mutates schema.
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ajent-social/amos/audit"
	"github.com/google/uuid"
)

var ErrInvalidQuery = errors.New("invalid audit query")

type unavailableError struct{ cause error }

func (e *unavailableError) Error() string        { return audit.ErrUnavailable.Error() }
func (e *unavailableError) Is(target error) bool { return target == audit.ErrUnavailable }

func unavailable(cause error) error {
	if cause == nil {
		return audit.ErrUnavailable
	}
	return &unavailableError{cause: cause}
}

// ReadAuthorizer must consult the current trusted principal, permissions, and
// workspace membership. A missing or unavailable authorization decision fails
// closed; no caller-provided header or identifier is an authorization source.
type ReadAuthorizer interface {
	AuthorizeAuditRead(context.Context, audit.Scope) error
}

// WriteAuthorizer validates that the current trusted principal may append into
// the exact installation/application/environment/workspace scope.
type WriteAuthorizer interface {
	AuthorizeAuditWrite(context.Context, audit.Scope) error
}

// ActorResolver derives attribution from the trusted authentication context;
// implementations must never treat request-supplied person IDs as verified.
type ActorResolver interface {
	ResolveAuditActor(context.Context) (audit.Actor, error)
}

type Store struct {
	db     *sql.DB
	reader ReadAuthorizer
	writer WriteAuthorizer
	actor  ActorResolver
}

func New(db *sql.DB, reader ReadAuthorizer, writer WriteAuthorizer, actor ActorResolver) (*Store, error) {
	if db == nil || reader == nil || writer == nil || actor == nil {
		return nil, ErrInvalidQuery
	}
	return &Store{db: db, reader: reader, writer: writer, actor: actor}, nil
}

// AppendTx appends the event in the caller's transaction so domain state and
// the corresponding security event commit or roll back together.
func (s *Store) AppendTx(ctx context.Context, tx *sql.Tx, event audit.Event) (audit.Record, error) {
	if s == nil || s.db == nil || ctx == nil || tx == nil || audit.Validate(event) != nil {
		return audit.Record{}, audit.ErrInvalidEvent
	}
	actor, err := s.actor.ResolveAuditActor(ctx)
	if err != nil {
		if errors.Is(err, audit.ErrForbidden) {
			return audit.Record{}, audit.ErrForbidden
		}
		return audit.Record{}, audit.ErrUnavailable
	}
	if !audit.ValidActor(actor) {
		return audit.Record{}, audit.ErrForbidden
	}
	if err := s.writer.AuthorizeAuditWrite(ctx, scopeForEvent(event)); err != nil {
		if errors.Is(err, audit.ErrForbidden) {
			return audit.Record{}, audit.ErrForbidden
		}
		return audit.Record{}, audit.ErrUnavailable
	}
	attributes := event.Attributes
	if attributes == nil {
		attributes = []audit.Attribute{}
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return audit.Record{}, audit.ErrInvalidEvent
	}
	id, err := uuid.NewV7()
	if err != nil {
		return audit.Record{}, unavailable(err)
	}
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO amos_security_audit_events
		(id, installation_id, application_id, environment_id, workspace_id, actor_kind, actor_id,
		 action, resource_type, resource_id, outcome, correlation_id, attributes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,transaction_timestamp())
		RETURNING created_at`,
		id, event.InstallationID, event.ApplicationID, event.EnvironmentID, event.WorkspaceID,
		actor.Kind, actor.ID, event.Action, event.ResourceType, event.ResourceID,
		event.Outcome, event.CorrelationID, string(encoded)).Scan(&createdAt)
	if err != nil {
		return audit.Record{}, unavailable(err)
	}
	return audit.Record{ID: id, Event: event, Actor: actor, CreatedAt: createdAt.UTC()}, nil
}

// List returns records only for the complete installation/application/
// environment/workspace scope. An unmatched scope returns an empty page and
// conveys no record-existence information. Callers must authorize the current
// principal and workspace membership before invoking List.
func (s *Store) List(ctx context.Context, scope audit.Scope, cursor *audit.Cursor, limit int) ([]audit.Record, error) {
	if s == nil || s.db == nil || ctx == nil || !validScope(scope) || limit < 1 || limit > audit.MaxPageSize || cursor != nil && (cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil || cursor.ID.Version() != 7) {
		return nil, ErrInvalidQuery
	}
	if s.reader == nil {
		return nil, audit.ErrForbidden
	}
	if err := s.reader.AuthorizeAuditRead(ctx, scope); err != nil {
		if errors.Is(err, audit.ErrForbidden) {
			return nil, audit.ErrForbidden
		}
		return nil, audit.ErrUnavailable
	}
	var rows *sql.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.QueryContext(ctx, selectRecords+` WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4
			ORDER BY created_at DESC, id DESC LIMIT $5`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.WorkspaceID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, selectRecords+` WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4
			AND (created_at,id) < ($5,$6) ORDER BY created_at DESC, id DESC LIMIT $7`,
			scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.WorkspaceID, cursor.CreatedAt.UTC(), cursor.ID, limit)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	result := make([]audit.Record, 0, limit)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, unavailable(scanErr)
		}
		result = append(result, record)
	}
	if rows.Err() != nil {
		return nil, unavailable(rows.Err())
	}
	return result, nil
}

func scopeForEvent(event audit.Event) audit.Scope {
	return audit.Scope{InstallationID: event.InstallationID, ApplicationID: event.ApplicationID, EnvironmentID: event.EnvironmentID, WorkspaceID: event.WorkspaceID}
}

const selectRecords = `SELECT id, installation_id, application_id, environment_id, workspace_id,
	actor_kind, actor_id, action, resource_type, resource_id, outcome, correlation_id, attributes::text, created_at
	FROM amos_security_audit_events`

type rowScanner interface{ Scan(...any) error }

func scanRecord(row rowScanner) (audit.Record, error) {
	var record audit.Record
	var actorKind, action, resourceType, outcome string
	var attributes string
	err := row.Scan(&record.ID, &record.InstallationID, &record.ApplicationID, &record.EnvironmentID,
		&record.WorkspaceID, &actorKind, &record.Actor.ID, &action, &resourceType,
		&record.ResourceID, &outcome, &record.CorrelationID, &attributes, &record.CreatedAt)
	if err != nil {
		return audit.Record{}, err
	}
	record.Actor.Kind = audit.ActorKind(actorKind)
	record.Action = audit.Action(action)
	record.ResourceType = audit.ResourceType(resourceType)
	record.Outcome = audit.Outcome(outcome)
	if err := json.Unmarshal([]byte(attributes), &record.Attributes); err != nil || audit.Validate(record.Event) != nil || !audit.ValidActor(record.Actor) {
		return audit.Record{}, errors.New("invalid persisted audit event")
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

func validScope(scope audit.Scope) bool {
	return scope.InstallationID != uuid.Nil && scope.InstallationID.Version() == 7 &&
		scope.ApplicationID != uuid.Nil && scope.ApplicationID.Version() == 7 &&
		scope.EnvironmentID != uuid.Nil && scope.EnvironmentID.Version() == 7 &&
		scope.WorkspaceID != uuid.Nil && scope.WorkspaceID.Version() == 7
}
