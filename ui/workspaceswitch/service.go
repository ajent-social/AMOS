package workspaceswitch

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// Config fixes the installation, application and environment served by this
// workspace switcher. These values come from validated host configuration.
type Config struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
}

// SQLService reads selectable workspaces from current PostgreSQL state for
// each operation. It never treats a selector or cached principal snapshot as
// authority.
type SQLService struct {
	db  *storage.DB
	cfg Config
}

// NewSQLService creates a fail-closed workspace selection service.
func NewSQLService(db *storage.DB, cfg Config) (*SQLService, error) {
	if db == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrUnavailable
	}
	return &SQLService{db: db, cfg: cfg}, nil
}

// Choices returns the currently allowed workspaces and a current selection.
// A supplied stale or unauthorized hint is denied with empty DTOs. Without a
// hint, the person's active personal workspace is shown as the safe
// remediation choice; no cookie or business authority is created by this
// display fallback.
func (s *SQLService) Choices(ctx context.Context, principal identity.Principal, hint uuid.UUID) (Workspace, []Workspace, error) {
	if s == nil || s.db == nil || ctx == nil {
		return Workspace{}, nil, ErrUnavailable
	}
	if hint != uuid.Nil && !validID(hint) {
		return Workspace{}, nil, ErrInvalidSelector
	}
	if !s.validPersonPrincipal(principal) {
		return Workspace{}, nil, ErrDenied
	}
	items := make([]Workspace, 0, 4)
	err := s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT w.id, w.kind
			FROM workspaces w
			JOIN identity_persons p
			  ON p.id = $3 AND p.installation_id = w.installation_id
			 AND p.application_id = w.application_id AND p.state = 'active'
			 AND p.security_epoch = $4
			LEFT JOIN workspace_memberships m
			  ON m.installation_id = w.installation_id
			 AND m.application_id = w.application_id AND m.workspace_id = w.id
			 AND m.person_id = p.id AND m.state = 'active'
			WHERE w.installation_id = $1 AND w.application_id = $2
			  AND w.state = 'active'
			  AND ((w.kind = 'personal' AND w.personal_owner_id = p.id)
			       OR (w.kind = 'organization' AND m.id IS NOT NULL))
			ORDER BY CASE WHEN w.kind = 'personal' THEN 0 ELSE 1 END, w.id`,
			s.cfg.InstallationID, s.cfg.ApplicationID,
			principal.PersonID(), principal.SecurityEpoch())
		if err != nil {
			return err
		}
		for rows.Next() {
			var item Workspace
			if err := rows.Scan(&item.ID, &item.Kind); err != nil {
				return err
			}
			item.Name = generatedWorkspaceName(item.ID, item.Kind)
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return errors.Join(err, rows.Close())
		}
		return rows.Close()
	})
	if err != nil {
		return Workspace{}, nil, ErrUnavailable
	}
	if len(items) == 0 {
		return Workspace{}, nil, ErrDenied
	}
	selectedID := hint
	if selectedID == uuid.Nil {
		selectedID = items[0].ID
		if items[0].Kind != "personal" {
			return Workspace{}, nil, ErrDenied
		}
	}
	for _, item := range items {
		if item.ID == selectedID {
			return item, items, nil
		}
	}
	return Workspace{}, nil, ErrDenied
}

// Select confirms a submitted workspace against current person, workspace and
// membership state. Callers persist the returned ID only as an untrusted hint.
func (s *SQLService) Select(ctx context.Context, principal identity.Principal, selectedID uuid.UUID) (Workspace, error) {
	if s == nil || s.db == nil || ctx == nil {
		return Workspace{}, ErrUnavailable
	}
	if !validID(selectedID) {
		return Workspace{}, ErrInvalidSelector
	}
	if !s.validPersonPrincipal(principal) {
		return Workspace{}, ErrDenied
	}
	var selected Workspace
	err := s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			SELECT w.id, w.kind
			FROM workspaces w
			JOIN identity_persons p
			  ON p.id = $3 AND p.installation_id = w.installation_id
			 AND p.application_id = w.application_id AND p.state = 'active'
			 AND p.security_epoch = $4
			LEFT JOIN workspace_memberships m
			  ON m.installation_id = w.installation_id
			 AND m.application_id = w.application_id AND m.workspace_id = w.id
			 AND m.person_id = p.id AND m.state = 'active'
			WHERE w.id = $5 AND w.installation_id = $1
			  AND w.application_id = $2 AND w.state = 'active'
			  AND ((w.kind = 'personal' AND w.personal_owner_id = p.id)
			       OR (w.kind = 'organization' AND m.id IS NOT NULL))`,
			s.cfg.InstallationID, s.cfg.ApplicationID,
			principal.PersonID(), principal.SecurityEpoch(), selectedID).Scan(&selected.ID, &selected.Kind)
	})
	if err == sql.ErrNoRows {
		return Workspace{}, ErrDenied
	}
	if err != nil {
		return Workspace{}, ErrUnavailable
	}
	selected.Name = generatedWorkspaceName(selected.ID, selected.Kind)
	return selected, nil
}

func (s *SQLService) validPersonPrincipal(principal identity.Principal) bool {
	actor := principal.Actor()
	return actor.Kind() == "person" && validID(principal.PersonID()) && actor.PersonID() == principal.PersonID() &&
		principal.InstallationID() == s.cfg.InstallationID && principal.ApplicationID() == s.cfg.ApplicationID &&
		principal.EnvironmentID() == s.cfg.EnvironmentID && principal.SecurityEpoch() >= 0
}

func generatedWorkspaceName(id uuid.UUID, kind string) string {
	switch kind {
	case "personal":
		return "Personal workspace"
	case "organization":
		// UUIDv7 prefixes encode time, so nearby workspaces commonly share them.
		return "Organization " + strings.ToLower(id.String()[28:])
	default:
		return ""
	}
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

var _ Service = (*SQLService)(nil)
