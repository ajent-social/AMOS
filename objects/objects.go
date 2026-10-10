// Package objects stores short-lived, tenant-owned lifecycle exports without
// exposing provider keys or presigned URLs to callers.
package objects

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/policy"
	"github.com/google/uuid"
)

var (
	ErrInvalid            = errors.New("invalid object request")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrForbidden          = errors.New("object access denied")
	ErrPolicyUnavailable  = errors.New("current authorization unavailable")
	ErrStorageUnavailable = errors.New("object storage unavailable")
	ErrNotFound           = errors.New("object not found")
	ErrExpired            = errors.New("object expired")
	ErrTooLarge           = errors.New("object exceeds configured limit")
)

const resourceType = "lifecycle_export"

type Reference struct {
	ID          identity.ID
	WorkspaceID identity.ID
	RequestID   identity.ID
	ContentType string
	Size        int64
	CreatedAt   time.Time
	ExpiresAt   time.Time
	VersionID   string `json:"-"`
}

type CreateRequest struct {
	WorkspaceID identity.ID
	RequestID   identity.ID
	ContentType string
	Size        int64
	ExpiresAt   time.Time
	Body        io.Reader
}

type Store interface {
	Put(context.Context, Reference, io.Reader) error
	Head(context.Context, identity.ID) (Reference, error)
	Open(context.Context, Reference) (io.ReadCloser, error)
	Delete(context.Context, Reference) error
}

type PrincipalResolver interface {
	ResolvePrincipal(context.Context) (identity.Principal, error)
}

type ContextPrincipalResolver struct{}

func (ContextPrincipalResolver) ResolvePrincipal(ctx context.Context) (identity.Principal, error) {
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return identity.Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

// CurrentAuthorizer performs a fresh current-state authorization for every
// object operation. Implementations bind owner, workspace, lifecycle-request,
// credential and policy-expiry checks to authoritative transaction time; callers
// must not cache policy decisions. Immediate revocation of an already-open body
// remains an integration responsibility.
type CurrentAuthorizer interface {
	AuthorizeCurrent(context.Context, identity.Principal, policy.Resource, policy.Requirements) error
}

type Service struct {
	store       Store
	authorizer  CurrentAuthorizer
	principals  PrincipalResolver
	maxBytes    int64
	maxLifetime time.Duration
	now         func() time.Time
}

type Config struct {
	Store       Store
	Authorizer  CurrentAuthorizer
	Principals  PrincipalResolver
	MaxBytes    int64
	MaxLifetime time.Duration
	Now         func() time.Time
}

func New(config Config) (*Service, error) {
	if config.Store == nil || config.Authorizer == nil || config.MaxBytes <= 0 || config.MaxLifetime <= 0 {
		return nil, ErrInvalid
	}
	if config.Principals == nil {
		config.Principals = ContextPrincipalResolver{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{
		store: config.Store, authorizer: config.Authorizer, principals: config.Principals,
		maxBytes: config.MaxBytes, maxLifetime: config.MaxLifetime, now: config.Now,
	}, nil
}

// Create persists an export only after the caller's current workspace authority
// has been established. RequestID is the lifecycle request whose state the
// policy evaluator must recheck.
func (s *Service) Create(ctx context.Context, request CreateRequest) (Reference, error) {
	if s == nil || request.Body == nil || request.Size <= 0 || request.Size > s.maxBytes ||
		!validID(request.WorkspaceID) || !validID(request.RequestID) ||
		!validContentType(request.ContentType) {
		return Reference{}, ErrInvalid
	}
	now := s.now().UTC()
	expiry := request.ExpiresAt.UTC()
	if expiry.IsZero() || !expiry.After(now) || expiry.Sub(now) > s.maxLifetime {
		return Reference{}, ErrInvalid
	}
	principal, err := s.principals.ResolvePrincipal(ctx)
	if err != nil {
		return Reference{}, err
	}
	ref, err := newReference(request, now, expiry)
	if err != nil {
		return Reference{}, err
	}
	if err = s.authorize(ctx, principal, ref, "lifecycle.export.create"); err != nil {
		return Reference{}, err
	}
	if err = s.store.Put(ctx, ref, request.Body); err != nil {
		return Reference{}, err
	}
	return ref, nil
}

// Open checks object metadata and current policy before asking the store for
// bytes. The returned stream is server-proxied; callers never receive a
// provider URL or object key.
func (s *Service) Open(ctx context.Context, id identity.ID) (Reference, io.ReadCloser, error) {
	if s == nil || !validID(id) {
		return Reference{}, nil, ErrInvalid
	}
	principal, err := s.principals.ResolvePrincipal(ctx)
	if err != nil {
		return Reference{}, nil, err
	}
	ref, err := s.store.Head(ctx, id)
	if err != nil {
		return Reference{}, nil, normalizeStoreError(err)
	}
	if ref.ID != id || !validID(ref.RequestID) || !validID(ref.WorkspaceID) {
		return Reference{}, nil, ErrNotFound
	}
	if err = s.authorize(ctx, principal, ref, "lifecycle.export.download"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return Reference{}, nil, ErrNotFound
		}
		return Reference{}, nil, err
	}
	if !s.now().Before(ref.ExpiresAt) {
		return Reference{}, nil, ErrExpired
	}
	body, err := s.store.Open(ctx, ref)
	if err != nil {
		return Reference{}, nil, normalizeStoreError(err)
	}
	return ref, body, nil
}

func (s *Service) Delete(ctx context.Context, id identity.ID) error {
	if s == nil || !validID(id) {
		return ErrInvalid
	}
	principal, err := s.principals.ResolvePrincipal(ctx)
	if err != nil {
		return err
	}
	ref, err := s.store.Head(ctx, id)
	if err != nil {
		return normalizeStoreError(err)
	}
	if err = s.authorize(ctx, principal, ref, "lifecycle.export.delete"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return ErrNotFound
		}
		return err
	}
	if err = s.store.Delete(ctx, ref); err != nil {
		return normalizeStoreError(err)
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, principal identity.Principal, ref Reference, permission string) error {
	err := s.authorizer.AuthorizeCurrent(ctx, principal, policy.Resource{
		Type: resourceType, ID: ref.RequestID, WorkspaceID: ref.WorkspaceID,
	}, policy.Requirements{Permissions: []string{permission}})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrForbidden):
		return ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrPolicyUnavailable
	}
}

func newReference(request CreateRequest, now, expiry time.Time) (Reference, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Reference{}, ErrInvalid
	}
	return Reference{
		ID: id, WorkspaceID: request.WorkspaceID,
		RequestID: request.RequestID, ContentType: request.ContentType, Size: request.Size,
		CreatedAt: now, ExpiresAt: expiry,
	}, nil
}

func validID(id identity.ID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func validContentType(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
}

func normalizeStoreError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrStorageUnavailable
}
