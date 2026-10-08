package magiclink

import (
	"database/sql"
	"testing"
	"time"

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
