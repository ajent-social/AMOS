package objects

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/policy"
	"github.com/google/uuid"
)

type testPrincipalResolver struct{ err error }

func (r testPrincipalResolver) ResolvePrincipal(context.Context) (identity.Principal, error) {
	return identity.Principal{}, r.err
}

type testAuthorizer struct {
	workspace  identity.ID
	last       policy.Resource
	permission string
	calls      int
	err        error
}

func (a *testAuthorizer) AuthorizeCurrent(_ context.Context, _ identity.Principal, resource policy.Resource, req policy.Requirements) error {
	a.calls++
	a.last = resource
	if len(req.Permissions) > 0 {
		a.permission = req.Permissions[0]
	}
	if resource.WorkspaceID != a.workspace {
		return ErrForbidden
	}
	return a.err
}

type storedObject struct {
	ref  Reference
	data []byte
}

type memoryStore struct {
	objects   map[identity.ID]storedObject
	opens     int
	headCalls int
}

func newMemoryStore() *memoryStore { return &memoryStore{objects: make(map[identity.ID]storedObject)} }

func (m *memoryStore) Put(_ context.Context, ref Reference, body io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(body, ref.Size+1))
	if err != nil || int64(len(data)) != ref.Size {
		return ErrInvalid
	}
	if _, exists := m.objects[ref.ID]; exists {
		return ErrInvalid
	}
	m.objects[ref.ID] = storedObject{ref: ref, data: data}
	return nil
}
func (m *memoryStore) Head(_ context.Context, id identity.ID) (Reference, error) {
	object, ok := m.objects[id]
	if !ok {
		return Reference{}, ErrNotFound
	}
	return object.ref, nil
}
func (m *memoryStore) Open(_ context.Context, ref Reference) (io.ReadCloser, error) {
	object, ok := m.objects[ref.ID]
	if !ok {
		return nil, ErrNotFound
	}
	m.opens++
	return io.NopCloser(bytes.NewReader(object.data)), nil
}
func (m *memoryStore) Delete(_ context.Context, ref Reference) error {
	if _, ok := m.objects[ref.ID]; !ok {
		return ErrNotFound
	}
	delete(m.objects, ref.ID)
	return nil
}

func TestCreateAndOpenRequireCurrentWorkspaceAndRequestState(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	workspace := testID(t)
	requestID := testID(t)
	store := newMemoryStore()
	evaluator := &testAuthorizer{workspace: workspace}
	service, err := New(Config{
		Store: store, Authorizer: evaluator, Principals: testPrincipalResolver{},
		MaxBytes: 1024, MaxLifetime: 2 * time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("synthetic lifecycle export")
	ref, err := service.Create(context.Background(), CreateRequest{
		WorkspaceID: workspace, RequestID: requestID, ContentType: "application/json",
		Size: int64(len(content)), ExpiresAt: now.Add(time.Hour), Body: bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("create export: %v", err)
	}
	if ref.ID == uuid.Nil || ref.ID.Version() != 7 || ref.WorkspaceID != workspace || ref.RequestID != requestID {
		t.Fatalf("invalid opaque reference: %+v", ref)
	}
	if evaluator.last.ID != requestID || evaluator.last.WorkspaceID != workspace || evaluator.permission != "lifecycle.export.create" {
		t.Fatalf("create did not bind current lifecycle authority: %+v %q", evaluator.last, evaluator.permission)
	}

	openedRef, body, err := service.Open(context.Background(), ref.ID)
	if err != nil {
		t.Fatalf("open authorized export: %v", err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("read export: %q %v %v", got, readErr, closeErr)
	}
	if openedRef.RequestID != requestID || evaluator.permission != "lifecycle.export.download" {
		t.Fatalf("download did not recheck lifecycle authority: %+v %q", openedRef, evaluator.permission)
	}

	otherWorkspace := testID(t)
	evaluator.workspace = otherWorkspace
	if _, body, err = service.Open(context.Background(), ref.ID); !errors.Is(err, ErrNotFound) || body != nil {
		t.Fatalf("cross-workspace object access: body=%v err=%v", body, err)
	}
	if store.opens != 1 {
		t.Fatalf("denied request reached object body: open calls=%d", store.opens)
	}

	evaluator.workspace = workspace
	now = now.Add(2 * time.Hour)
	if _, body, err = service.Open(context.Background(), ref.ID); !errors.Is(err, ErrExpired) || body != nil {
		t.Fatalf("expired export access: body=%v err=%v", body, err)
	}
	if store.opens != 1 {
		t.Fatalf("expired request reached object body: open calls=%d", store.opens)
	}
}

func TestCreateAndOpenFailClosedWhenAuthorityOrStorageIsUnavailable(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	workspace, requestID := testID(t), testID(t)
	evaluator := &testAuthorizer{workspace: workspace, err: ErrPolicyUnavailable}
	store := newMemoryStore()
	service, err := New(Config{Store: store, Authorizer: evaluator, Principals: testPrincipalResolver{},
		MaxBytes: 1024, MaxLifetime: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), CreateRequest{WorkspaceID: workspace, RequestID: requestID,
		ContentType: "application/json", Size: 1, ExpiresAt: now.Add(time.Minute), Body: bytes.NewReader([]byte("x"))})
	if !errors.Is(err, ErrPolicyUnavailable) {
		t.Fatalf("unavailable current policy result: %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatal("export persisted while authorization was unavailable")
	}

	defaultResolverService, err := New(Config{Store: store, Authorizer: &testAuthorizer{workspace: workspace},
		MaxBytes: 1024, MaxLifetime: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = defaultResolverService.Create(context.Background(), CreateRequest{WorkspaceID: workspace, RequestID: requestID,
		ContentType: "application/json", Size: 1, ExpiresAt: now.Add(time.Minute), Body: bytes.NewReader([]byte("x"))})
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing authenticated principal: %v", err)
	}
}

func TestCreateRejectsOversizeAndOutOfPolicyExpiry(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	workspace, requestID := testID(t), testID(t)
	store := newMemoryStore()
	service, err := New(Config{Store: store, Authorizer: &testAuthorizer{workspace: workspace}, Principals: testPrincipalResolver{},
		MaxBytes: 2, MaxLifetime: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []CreateRequest{
		{WorkspaceID: workspace, RequestID: requestID, ContentType: "application/json", Size: 3, ExpiresAt: now.Add(time.Minute), Body: bytes.NewReader([]byte("abc"))},
		{WorkspaceID: workspace, RequestID: requestID, ContentType: "application/json", Size: 1, ExpiresAt: now.Add(2 * time.Hour), Body: bytes.NewReader([]byte("x"))},
		{WorkspaceID: workspace, RequestID: requestID, ContentType: "application/json", Size: 1, ExpiresAt: now, Body: bytes.NewReader([]byte("x"))},
	} {
		if _, err = service.Create(context.Background(), request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid request accepted: %v", err)
		}
	}
	if len(store.objects) != 0 {
		t.Fatal("invalid exports were persisted")
	}
}

func testID(t *testing.T) identity.ID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOpenMissingObjectAndDeleteRecheckWorkspace(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	workspace, requestID := testID(t), testID(t)
	store := newMemoryStore()
	evaluator := &testAuthorizer{workspace: workspace}
	service, err := New(Config{Store: store, Authorizer: evaluator, Principals: testPrincipalResolver{},
		MaxBytes: 1024, MaxLifetime: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	missingID := testID(t)
	if _, body, err := service.Open(context.Background(), missingID); !errors.Is(err, ErrNotFound) || body != nil {
		t.Fatalf("missing object access: body=%v err=%v", body, err)
	}

	content := []byte("synthetic export")
	ref, err := service.Create(context.Background(), CreateRequest{WorkspaceID: workspace, RequestID: requestID,
		ContentType: "application/json", Size: int64(len(content)), ExpiresAt: now.Add(time.Minute), Body: bytes.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	evaluator.workspace = testID(t)
	if err = service.Delete(context.Background(), ref.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace delete: %v", err)
	}
	if _, err = store.Head(context.Background(), ref.ID); err != nil {
		t.Fatalf("denied delete removed object: %v", err)
	}

	evaluator.workspace = workspace
	if err = service.Delete(context.Background(), ref.ID); err != nil {
		t.Fatalf("authorized retention delete: %v", err)
	}
	if evaluator.permission != "lifecycle.export.delete" {
		t.Fatalf("delete did not check current permission: %q", evaluator.permission)
	}
	if _, err = store.Head(context.Background(), ref.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted export remains available: %v", err)
	}
}

func TestOpenResolvesPrincipalBeforeObjectLookup(t *testing.T) {
	store := newMemoryStore()
	service, err := New(Config{Store: store, Authorizer: &testAuthorizer{}, Principals: testPrincipalResolver{err: errors.New("missing principal")},
		MaxBytes: 1024, MaxLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, body, err := service.Open(context.Background(), testID(t)); err == nil || body != nil {
		t.Fatalf("unauthenticated open: body=%v err=%v", body, err)
	}
	if store.headCalls != 0 {
		t.Fatalf("unauthenticated request reached object lookup: calls=%d", store.headCalls)
	}
}

func TestNormalizeStoreErrorPreservesReconciliationRequired(t *testing.T) {
	if got := normalizeStoreError(ErrReconciliationRequired); got != ErrReconciliationRequired {
		t.Fatalf("reconciliation signal normalized to %v", got)
	}
}
