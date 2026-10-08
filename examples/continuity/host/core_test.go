package host

import (
	"context"
	"errors"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
	"github.com/google/uuid"
	"testing"
)

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestPrivateCoreRejectsIncompleteConstruction(t *testing.T) {
	cfg := coreConfig{InstallationID: testID(t), ApplicationID: testID(t), EnvironmentID: testID(t), Origin: "https://reader.example.test"}
	//nolint:staticcheck // Nil admission must fail before any connection.
	if core, e := openReadCore(nil, cfg); core != nil || !errors.Is(e, errConfiguration) {
		t.Fatal("nil context admitted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if core, e := openReadCore(canceled, cfg); core != nil || e != errConfiguration {
		t.Fatal("canceled construction admitted")
	}
	bad := cfg
	bad.EnvironmentID = uuid.Nil
	if core, e := openReadCore(context.Background(), bad); core != nil || e != errConfiguration {
		t.Fatal("missing realm admitted")
	}
	if core, e := openReadCore(context.Background(), cfg); core != nil || e != errUnavailable {
		t.Fatal("invalid runtime configuration admitted")
	}
}
func TestPrivateSourceInputAndNoAdmission(t *testing.T) {
	c := &readCore{root: &aw.Root{}, sessions: &session.Service{}, workspaces: &workspacecontext.Resolver{}}
	for _, q := range []sourceQuery{{Limit: 0}, {Limit: 101}, {ID: testID(t).String(), Limit: 1}, {ID: "invalid"}, {Limit: 1, WorkspaceID: uuid.New()}} {
		if b, e := c.source(context.Background(), q); b != nil || e != errInvalid {
			t.Fatal("invalid selector reached authority")
		}
	}
	if b, e := c.source(context.Background(), sourceQuery{Limit: 10}); b != nil || e != errUnavailable {
		t.Fatal("unrooted composition disclosed bytes")
	}
	if e := (*readCore)(nil).close(); e != nil {
		t.Fatal(e)
	}
}
