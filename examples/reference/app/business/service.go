// Package business is the reference, tenant-scoped todo service. It implements
// the typed handlers generated from examples/reference/api/business.openapi.yaml.
package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/reference/app/business/apigen"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput         = errors.New("invalid todo request")
	ErrUnauthenticated      = errors.New("verified person principal is required")
	ErrWorkspaceUnavailable = errors.New("workspace unavailable")
	ErrTodoUnavailable      = errors.New("todo unavailable")
	ErrRevisionConflict     = errors.New("todo revision conflict")
	ErrPersistence          = errors.New("todo persistence failed")
)

const maxListTodos = 1000

type Service struct{ db *storage.DB }

func New(db *storage.DB) (*Service, error) {
	if db == nil {
		return nil, ErrPersistence
	}
	return &Service{db: db}, nil
}

var (
	_ apigen.OpTodosCreateHandler = (*Service)(nil)
	_ apigen.OpTodosDeleteHandler = (*Service)(nil)
	_ apigen.OpTodosListHandler   = (*Service)(nil)
	_ apigen.OpTodosUpdateHandler = (*Service)(nil)
)

func (s *Service) HandleTodosCreate(ctx context.Context, request apigen.OpTodosCreateRequest) (apigen.OpTodosCreateResponse, error) {
	principal, workspaceID, err := s.authorize(ctx, string(request.Body.WorkspaceId))
	if err != nil {
		return apigen.OpTodosCreateResponse{}, err
	}
	if !validTitle(string(request.Body.Title)) {
		return apigen.OpTodosCreateResponse{}, ErrInvalidInput
	}
	id, err := uuid.NewV7()
	if err != nil || !validID(id) {
		return apigen.OpTodosCreateResponse{}, ErrPersistence
	}
	var todo apigen.ModelTodo
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := requireWorkspaceMember(ctx, tx, principal, workspaceID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `INSERT INTO reference_todos
			(id,installation_id,application_id,workspace_id,creator_person_id,title,completed,revision)
			VALUES ($1,$2,$3,$4,$5,$6,FALSE,1)
			RETURNING id,workspace_id,title,completed,revision`,
			id, principal.InstallationID(), principal.ApplicationID(), workspaceID, principal.PersonID(), string(request.Body.Title)).
			Scan(&todo.Id, &todo.WorkspaceId, &todo.Title, &todo.Completed, &todo.Revision)
	})
	if err != nil {
		return apigen.OpTodosCreateResponse{}, publicError(err)
	}
	return todo, nil
}

func (s *Service) HandleTodosList(ctx context.Context, request apigen.OpTodosListRequest) (apigen.OpTodosListResponse, error) {
	principal, workspaceID, err := s.authorize(ctx, string(request.Body.WorkspaceId))
	if err != nil {
		return apigen.OpTodosListResponse{}, err
	}
	response := apigen.OpTodosListResponse{Todos: make([]apigen.ModelTodo, 0)}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := requireWorkspaceMember(ctx, tx, principal, workspaceID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,workspace_id,title,completed,revision
			FROM reference_todos WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3
			ORDER BY created_at,id LIMIT $4`, principal.InstallationID(), principal.ApplicationID(), workspaceID, maxListTodos)
		if err != nil {
			return err
		}
		for rows.Next() {
			var todo apigen.ModelTodo
			if err := rows.Scan(&todo.Id, &todo.WorkspaceId, &todo.Title, &todo.Completed, &todo.Revision); err != nil {
				if closeErr := rows.Close(); closeErr != nil {
					return errors.Join(err, closeErr)
				}
				return err
			}
			response.Todos = append(response.Todos, todo)
		}
		if err := rows.Err(); err != nil {
			if closeErr := rows.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return apigen.OpTodosListResponse{}, publicError(err)
	}
	return response, nil
}

func (s *Service) HandleTodosUpdate(ctx context.Context, request apigen.OpTodosUpdateRequest) (apigen.OpTodosUpdateResponse, error) {
	principal, workspaceID, err := s.authorize(ctx, string(request.Body.WorkspaceId))
	if err != nil {
		return apigen.OpTodosUpdateResponse{}, err
	}
	todoID, err := parseID(string(request.Body.TodoId))
	if err != nil || request.Body.Revision < 1 || !validTitle(string(request.Body.Title)) {
		return apigen.OpTodosUpdateResponse{}, ErrInvalidInput
	}
	var todo apigen.ModelTodo
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := requireWorkspaceMember(ctx, tx, principal, workspaceID); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `UPDATE reference_todos
			SET title=$6,completed=$7,revision=revision+1,updated_at=transaction_timestamp()
			WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND id=$4 AND revision=$5
			RETURNING id,workspace_id,title,completed,revision`,
			principal.InstallationID(), principal.ApplicationID(), workspaceID, todoID, request.Body.Revision, string(request.Body.Title), request.Body.Completed).
			Scan(&todo.Id, &todo.WorkspaceId, &todo.Title, &todo.Completed, &todo.Revision)
		if errors.Is(err, sql.ErrNoRows) {
			return s.missingOrStale(ctx, tx, principal, workspaceID, todoID)
		}
		return err
	})
	if err != nil {
		return apigen.OpTodosUpdateResponse{}, publicError(err)
	}
	return todo, nil
}

func (s *Service) HandleTodosDelete(ctx context.Context, request apigen.OpTodosDeleteRequest) (apigen.OpTodosDeleteResponse, error) {
	principal, workspaceID, err := s.authorize(ctx, string(request.Body.WorkspaceId))
	if err != nil {
		return apigen.OpTodosDeleteResponse{}, err
	}
	todoID, err := parseID(string(request.Body.TodoId))
	if err != nil || request.Body.Revision < 1 {
		return apigen.OpTodosDeleteResponse{}, ErrInvalidInput
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := requireWorkspaceMember(ctx, tx, principal, workspaceID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM reference_todos
			WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND id=$4 AND revision=$5`,
			principal.InstallationID(), principal.ApplicationID(), workspaceID, todoID, request.Body.Revision)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			return nil
		}
		return s.missingOrStale(ctx, tx, principal, workspaceID, todoID)
	})
	if err != nil {
		return apigen.OpTodosDeleteResponse{}, publicError(err)
	}
	return apigen.OpTodosDeleteResponse{}, nil
}

func (s *Service) authorize(ctx context.Context, workspaceText string) (identity.Principal, uuid.UUID, error) {
	if s == nil || s.db == nil || ctx == nil {
		return identity.Principal{}, uuid.Nil, ErrPersistence
	}
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok || principal.Actor().Kind() != "person" || principal.PersonID() == uuid.Nil || principal.InstallationID() == uuid.Nil || principal.ApplicationID() == uuid.Nil {
		return identity.Principal{}, uuid.Nil, ErrUnauthenticated
	}
	workspaceID, err := parseID(workspaceText)
	if err != nil {
		return identity.Principal{}, uuid.Nil, ErrInvalidInput
	}
	return principal, workspaceID, nil
}

// requireWorkspaceMember treats the request's workspace ID as a selector only.
// It derives tenant and person identities from the verified principal, then
// locks current rows so concurrent revocation waits until this operation ends.
func requireWorkspaceMember(ctx context.Context, tx *sql.Tx, principal identity.Principal, workspaceID uuid.UUID) error {
	var found uuid.UUID
	// Follow person -> workspace -> membership lock order, matching identity
	// disablement and membership activation transactions.
	err := tx.QueryRowContext(ctx, `SELECT id FROM identity_persons
		WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND state='active' FOR SHARE`,
		principal.PersonID(), principal.InstallationID(), principal.ApplicationID()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	var kind string
	var owner uuid.NullUUID
	err = tx.QueryRowContext(ctx, `SELECT id,kind,personal_owner_id FROM workspaces
		WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND state='active' FOR SHARE`,
		workspaceID, principal.InstallationID(), principal.ApplicationID()).Scan(&found, &kind, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	if kind == "personal" {
		if !owner.Valid || owner.UUID != principal.PersonID() {
			return ErrWorkspaceUnavailable
		}
		return nil
	}
	if kind != "organization" {
		return ErrWorkspaceUnavailable
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM workspace_memberships
		WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4 AND state='active' FOR SHARE`,
		principal.InstallationID(), principal.ApplicationID(), workspaceID, principal.PersonID()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	return nil
}

func (s *Service) missingOrStale(ctx context.Context, tx *sql.Tx, principal identity.Principal, workspaceID, todoID uuid.UUID) error {
	var found bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reference_todos WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND id=$4)`, principal.InstallationID(), principal.ApplicationID(), workspaceID, todoID).Scan(&found)
	if err != nil {
		return err
	}
	if found {
		return ErrRevisionConflict
	}
	return ErrTodoUnavailable
}

func publicError(err error) error {
	switch {
	case errors.Is(err, ErrWorkspaceUnavailable), errors.Is(err, ErrTodoUnavailable), errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrInvalidInput), errors.Is(err, ErrUnauthenticated):
		return err
	default:
		return ErrPersistence
	}
}

func parseID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || !validID(id) || id.String() != value {
		return uuid.Nil, ErrInvalidInput
	}
	return id, nil
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == strings.ToLower(id.String())
}

func validTitle(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 200 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
