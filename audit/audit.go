// Package audit defines the bounded security-event contract used by trusted
// application services. It contains no request bodies, arbitrary details, or
// provider diagnostics. Records currently persist indefinitely; a bounded
// retention/erasure workflow requires an owner-configured, legal-hold-aware
// policy and is not implemented by this append-only sink.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidEvent = errors.New("invalid security audit event")
	ErrUnavailable  = errors.New("security audit store unavailable")
	ErrForbidden    = errors.New("security audit access denied")
)

const (
	MaxAttributes = 4
	MaxEventBytes = 4096
	MaxPageSize   = 100
)

type ActorKind string

const (
	ActorPerson  ActorKind = "person"
	ActorMachine ActorKind = "machine"
)

type Action string

const (
	ActionMaterialCreated  Action = "material.created"
	ActionMaterialUpdated  Action = "material.updated"
	ActionMaterialRevoked  Action = "material.revoked"
	ActionSessionIssued    Action = "session.issued"
	ActionSessionRevoked   Action = "session.revoked"
	ActionMembershipChange Action = "workspace.membership_changed"
	ActionGrantChanged     Action = "policy.grant_changed"
	ActionAccessDenied     Action = "security.access_denied"
)

type ResourceType string

const (
	ResourceMaterial   ResourceType = "material"
	ResourceSession    ResourceType = "session"
	ResourceWorkspace  ResourceType = "workspace"
	ResourceGrant      ResourceType = "grant"
	ResourceCredential ResourceType = "credential"
)

type Outcome string

const (
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeDenied      Outcome = "denied"
	OutcomeUnavailable Outcome = "unavailable"
)

// Attribute is a small, enumerated metadata pair. Values are intentionally
// enums rather than free-form text so callers cannot smuggle private data.
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Actor is resolved from trusted request context by sqlstore. It is returned
// on stored records but is never accepted as part of an append request.
type Actor struct {
	Kind ActorKind
	ID   uuid.UUID
}

// Event is an append request. The database assigns ID and CreatedAt.
type Event struct {
	InstallationID uuid.UUID    `json:"installation_id"`
	ApplicationID  uuid.UUID    `json:"application_id"`
	EnvironmentID  uuid.UUID    `json:"environment_id"`
	WorkspaceID    uuid.UUID    `json:"workspace_id"`
	Action         Action       `json:"action"`
	ResourceType   ResourceType `json:"resource_type"`
	ResourceID     uuid.UUID    `json:"resource_id"`
	Outcome        Outcome      `json:"outcome"`
	CorrelationID  uuid.UUID    `json:"correlation_id"`
	Attributes     []Attribute  `json:"attributes"`
}

// Record is a durable audit event. CreatedAt comes from PostgreSQL transaction
// time and is always UTC.
type Record struct {
	ID uuid.UUID
	Event
	Actor     Actor
	CreatedAt time.Time
}

type Scope struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
	WorkspaceID    uuid.UUID
}

// Cursor is exclusive and is returned from a page's final record.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// DecodeEvent accepts only the event schema. Unknown fields, including fields
// such as token, request_body, and provider_error, are rejected without being
// copied into the returned error.
func DecodeEvent(raw []byte) (Event, error) {
	if len(raw) == 0 || len(raw) > MaxEventBytes {
		return Event{}, ErrInvalidEvent
	}
	var event Event
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return Event{}, ErrInvalidEvent
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Event{}, ErrInvalidEvent
	}
	if Validate(event) != nil {
		return Event{}, ErrInvalidEvent
	}
	return event, nil
}

func Validate(event Event) error {
	if !validID(event.InstallationID) || !validID(event.ApplicationID) || !validID(event.EnvironmentID) || !validID(event.WorkspaceID) || !validID(event.ResourceID) || !validID(event.CorrelationID) {
		return ErrInvalidEvent
	}
	if !validAction(event.Action) || !validResource(event.ResourceType) || !validOutcome(event.Outcome) {
		return ErrInvalidEvent
	}
	if len(event.Attributes) > MaxAttributes {
		return ErrInvalidEvent
	}
	seen := make(map[string]struct{}, len(event.Attributes))
	for _, attribute := range event.Attributes {
		if !validAttribute(attribute) {
			return ErrInvalidEvent
		}
		if _, duplicate := seen[attribute.Key]; duplicate {
			return ErrInvalidEvent
		}
		seen[attribute.Key] = struct{}{}
	}
	return nil
}

func ValidActor(actor Actor) bool {
	return validID(actor.ID) && (actor.Kind == ActorPerson || actor.Kind == ActorMachine)
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func validAction(value Action) bool {
	switch value {
	case ActionMaterialCreated, ActionMaterialUpdated, ActionMaterialRevoked,
		ActionSessionIssued, ActionSessionRevoked, ActionMembershipChange,
		ActionGrantChanged, ActionAccessDenied:
		return true
	default:
		return false
	}
}

func validResource(value ResourceType) bool {
	switch value {
	case ResourceMaterial, ResourceSession, ResourceWorkspace, ResourceGrant, ResourceCredential:
		return true
	default:
		return false
	}
}

func validOutcome(value Outcome) bool {
	return value == OutcomeSucceeded || value == OutcomeDenied || value == OutcomeUnavailable
}

func validAttribute(attribute Attribute) bool {
	switch attribute.Key {
	case "factor":
		return attribute.Value == "password" || attribute.Value == "webauthn" || attribute.Value == "totp" || attribute.Value == "api_key"
	case "category":
		return attribute.Value == "identity" || attribute.Value == "workspace" || attribute.Value == "billing" || attribute.Value == "secret"
	case "state":
		return attribute.Value == "created" || attribute.Value == "updated" || attribute.Value == "revoked" || attribute.Value == "enabled" || attribute.Value == "disabled" || attribute.Value == "denied"
	default:
		return false
	}
}
