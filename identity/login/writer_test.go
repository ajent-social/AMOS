package login

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func TestNativeWriterPureConfigurationAndExactPasswordTuple(t *testing.T) {
	cfg := loginTxConfig(t)
	root := &aw.Root{}
	s, err := NewWithWriter(root, cfg)
	if err != nil || s.root != root || s.db != nil || s.cfg.Passwords != cfg.Passwords || s.cfg.Sessions != cfg.Sessions {
		t.Fatal("writer constructor changed dependency custody")
	}
	cfg.ChallengeLifetime = time.Hour
	if s.cfg.ChallengeLifetime != DefaultChallengeLifetime {
		t.Fatal("configuration was not copied")
	}
	if got, err := NewWithWriter(root, cfg); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("registration lifetime widened")
	}
	if got, err := NewWithWriter(nil, loginTxConfig(t)); got != nil || err == nil {
		t.Fatal("nil root admitted")
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	original := passwordRow{person: newID(t), contact: newID(t), credentialID: newID(t), epoch: 2, state: "active", key: "person@example.test", hash: "bounded-encoding", verified: sql.NullTime{Time: at, Valid: true}}
	if !original.shape() || !original.same(original) {
		t.Fatal("valid held tuple rejected")
	}
	cases := []struct {
		name   string
		change func(*passwordRow)
	}{{"same epoch hash", func(v *passwordRow) { v.hash = "replacement" }}, {"contact replacement", func(v *passwordRow) { v.contact = newID(t) }}, {"verification time", func(v *passwordRow) { v.verified.Time = at.Add(time.Microsecond) }}, {"unverified", func(v *passwordRow) { v.verified.Valid = false }}, {"credentialID ID", func(v *passwordRow) { v.credentialID = newID(t) }}, {"epoch", func(v *passwordRow) { v.epoch++ }}, {"person state", func(v *passwordRow) { v.state = "disabled" }}, {"comparison key", func(v *passwordRow) { v.key = "changed@example.test" }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := original
			tc.change(&changed)
			if original.same(changed) {
				t.Fatal("changed final credentialID tuple accepted")
			}
		})
	}
}
