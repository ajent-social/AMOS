package email

import (
	"context"
	"errors"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestEmailWriterPureRealmAndNoDelivery(t *testing.T) {
	cfg := Config{InstallationID: uuid.Must(uuid.NewV7()), ApplicationID: uuid.Must(uuid.NewV7()), EnvironmentID: uuid.Must(uuid.NewV7()), ApplicationOrigin: "https://example.test", ChallengeLifetime: 30 * time.Minute}
	if _, e := NewWithWriter(nil, nil, nil, nil, cfg); e == nil {
		t.Fatal("nil root admitted")
	}
	copy := cfg
	copy.EnvironmentID = uuid.Nil
	if _, e := NewWithWriter(&aw.Root{}, nil, nil, nil, copy); e == nil {
		t.Fatal("missing writer environment admitted")
	}
	s, e := NewWithWriter(&aw.Root{}, nil, nil, nil, cfg)
	if e != nil {
		t.Fatal(e)
	}
	cfg.EnvironmentID = uuid.Must(uuid.NewV7())
	if s.writerRealm().Environment == cfg.EnvironmentID {
		t.Fatal("realm aliased")
	}
	if _, e = s.IssueVerification(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())); !errors.Is(e, ErrUnavailable) {
		t.Fatal("unconfigured delivery admitted")
	}
	if e = s.QueueExistingChallengeWriter(context.Background(), &aw.Attempt{}, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, ""); !errors.Is(e, ErrUnavailable) {
		t.Fatal("unrooted queue admitted")
	}
}
