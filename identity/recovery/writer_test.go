package recovery

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"testing"
)

func TestRecoveryWriterConstruction(t *testing.T) {
	cfg := recoveryTxConfig(t)
	cfg.Outbox = nil
	cfg.Materials = nil
	cfg.Policy = &recoveryWriterTestPolicy{}
	root, sessions, outbox, materials := &aw.Root{}, &session.Service{}, &sqlstore.TxWriter{}, &materialstore.Writer{}
	if s, e := NewWithWriter(root, sessions, outbox, materials, cfg); e != nil || s == nil {
		t.Fatal("pool-free writer rejected without SQL")
	}
	for _, tc := range []struct {
		name string
		edit func(*TxConfig)
	}{
		{"missing completion contract", func(c *TxConfig) { c.Policy = allowRecoveryPolicy{} }},
		{"typed nil completion policy", func(c *TxConfig) { c.Policy = (*recoveryWriterTestPolicy)(nil) }},
		{"legacy outbox", func(c *TxConfig) { c.Outbox = &sqlstore.Store{} }},
		{"legacy material capability", func(c *TxConfig) { c.Materials = recoveryNoMaterial{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			tc.edit(&c)
			if s, e := NewWithWriter(root, sessions, outbox, materials, c); s != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatal("invalid writer configuration accepted")
			}
		})
	}
	for _, call := range []func() (*Service, error){func() (*Service, error) { return NewWithWriter(nil, sessions, outbox, materials, cfg) }, func() (*Service, error) { return NewWithWriter(root, nil, outbox, materials, cfg) }, func() (*Service, error) { return NewWithWriter(root, sessions, nil, materials, cfg) }, func() (*Service, error) { return NewWithWriter(root, sessions, outbox, nil, cfg) }} {
		if s, e := call(); s != nil || e != ErrConfiguration {
			t.Fatal("missing participant admitted")
		}
	}
}
func TestRecoveryWriterNoPublicPrincipalAdmission(t *testing.T) {
	cfg := recoveryTxConfig(t)
	cfg.Outbox = nil
	cfg.Materials = nil
	cfg.Policy = &recoveryWriterTestPolicy{}
	svc, e := NewWithWriter(&aw.Root{}, &session.Service{}, &sqlstore.TxWriter{}, &materialstore.Writer{}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e := svc.changeWriter(context.Background(), identity.Principal{}, oldPassword, newPassword); !errors.Is(e, ErrChallengeUnavailable) && !errors.Is(e, ErrUnavailable) {
		t.Fatal("unadmitted principal reached password work")
	}
}

var _ interface {
	AuthorizePasswordChangeCompletion(context.Context, *sql.Tx, identity.Principal, int64) error
} = (*recoveryWriterTestPolicy)(nil)
