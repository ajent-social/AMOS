package magiclink

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestNativeWriterPureStoredChallengeBoundsAndBinding(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := magicChallenge{id: id(), contact: magicContact{person: id(), email: id(), epoch: 1, state: "active", key: "person@example.test", address: "Person@example.test", verified: sql.NullTime{Time: created.Add(-time.Hour), Valid: true}}, digest: make([]byte, 32), browser: make([]byte, 32), created: created, expires: created.Add(DefaultChallengeLifetime)}
	c.digest[0] = 1
	c.browser[0] = 2
	if !c.shape() || !c.same(c) {
		t.Fatal("valid challenge rejected")
	}
	for _, ttl := range []time.Duration{0, 30 * time.Second, 30*time.Minute + time.Second, 61*time.Second + time.Nanosecond} {
		copy := c
		copy.expires = created.Add(ttl)
		if copy.shape() {
			t.Fatal("invalid persisted request age accepted")
		}
	}
	copy := c
	copy.browser = append([]byte(nil), c.browser...)
	copy.browser[0]++
	if c.same(copy) {
		t.Fatal("different browser binding compared equal")
	}
	copy = c
	copy.contact.verified.Time = copy.contact.verified.Time.Add(time.Microsecond)
	if c.same(copy) {
		t.Fatal("changed verified contact compared equal")
	}
	copy = c
	copy.consumed = sql.NullTime{Time: created, Valid: true}
	if c.same(copy) {
		t.Fatal("consumed challenge compared equal")
	}
	copy = c
	copy.expires = c.expires.Add(time.Second)
	if c.same(copy) {
		t.Fatal("refreshed challenge expiry compared equal")
	}
}

func TestNativeWriterPureMagicConstructionCustody(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	root, outbox, materials, sessions := &aw.Root{}, &sqlstore.TxWriter{}, &materialstore.Writer{}, &session.Service{}
	cfg := Config{Renderer: renderer, Sessions: sessions, Policy: allowMagicPolicy{}, InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), ApplicationOrigin: "https://example.test"}
	got, err := NewWithWriter(root, outbox, materials, cfg)
	if err != nil || got.root != root || got.writerOutbox != outbox || got.writerMaterials != materials || got.writerSessions != sessions || got.cfg.DB != nil {
		t.Fatal("native magic constructor changed dependency custody")
	}
	cfg.ChallengeLifetime = time.Minute
	if got.cfg.ChallengeLifetime != DefaultChallengeLifetime {
		t.Fatal("native magic retained mutable configuration")
	}
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"legacy database", func(c *Config) { c.DB = &storage.DB{} }},
		{"legacy outbox", func(c *Config) { c.Outbox = &sqlstore.Store{} }},
		{"legacy material", func(c *Config) { c.Materials = &materialstore.Store{} }},
		{"typed nil session", func(c *Config) { c.Sessions = (*session.Service)(nil) }},
		{"typed nil policy", func(c *Config) { c.Policy = (*allowMagicPolicy)(nil) }},
		{"nil renderer", func(c *Config) { c.Renderer = nil }},
		{"foreign ID shape", func(c *Config) { c.EnvironmentID = uuid.New() }},
		{"fractional lifetime", func(c *Config) { c.ChallengeLifetime = time.Minute + time.Nanosecond }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := cfg
			tc.mutate(&copy)
			if got, err := NewWithWriter(root, outbox, materials, copy); got != nil || !errors.Is(err, ErrConfiguration) {
				t.Fatal("invalid native magic dependency admitted")
			}
		})
	}
	if got, err := NewWithWriter(nil, outbox, materials, cfg); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil root admitted")
	}
	if got, err := NewWithWriter(root, nil, materials, cfg); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil native outbox admitted")
	}
	if got, err := NewWithWriter(root, outbox, nil, cfg); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil native material writer admitted")
	}
}
