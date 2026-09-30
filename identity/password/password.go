// Package password wraps maintained Argon2id with bounded resource policy.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalid     = errors.New("invalid password or verifier")
	ErrUnavailable = errors.New("password policy unavailable")
	ErrBusy        = errors.New("password verification capacity exhausted")
)

// Blocklist must be a locally loaded, versioned policy with documented provenance.
// The adapter must never transmit supplied passwords to an external service.
type Blocklist interface {
	Ready() bool
	ContainsNormalized(string) bool
}

// Budget applies caller-specific bounded rate limits before expensive work.
// Keys must be derived by trusted transport code, never from password contents.
type Budget interface {
	Allow(context.Context, string) error
}

var processSlots = make(chan struct{}, 2)

type Hasher struct {
	blocklist   Blocklist
	budget      Budget
	slots       chan struct{}
	dummy       string
	legacyDummy string
}

// New bounds concurrent verification operations for one application hasher.
// Instantiate one shared hasher per application process. A legacy upgrade may
// temporarily retain both 32 MiB and 64 MiB working allocations until garbage
// collection. Budget admission across replicas at the transport boundary.
func New(blocklist Blocklist, budget Budget, concurrency int) (*Hasher, error) {
	if blocklist == nil || !blocklist.Ready() || budget == nil || concurrency < 1 || concurrency > 2 {
		return nil, ErrUnavailable
	}
	h := &Hasher{blocklist: blocklist, budget: budget, slots: make(chan struct{}, concurrency)}
	// The dummy has an independent random salt and is never an account credential.
	var err error
	select {
	case processSlots <- struct{}{}:
		defer func() { <-processSlots }()
	default:
		return nil, ErrBusy
	}
	h.legacyDummy, err = derive("amos unknown account dummy credential", 32768, 2)
	if err != nil {
		return nil, err
	}
	h.dummy, err = derive("amos unknown account dummy credential", 65536, 3)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func normalized(secret string) (string, error) {
	if len(secret) > 512 || !utf8.ValidString(secret) {
		return "", ErrInvalid
	}
	secret = norm.NFC.String(secret)
	n := utf8.RuneCountInString(secret)
	if len(secret) > 512 || n < 15 || n > 128 {
		return "", ErrInvalid
	}
	return secret, nil
}
func (h *Hasher) acquire(ctx context.Context, key string) (func(), error) {
	if h == nil || h.budget == nil || key == "" {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := h.budget.Allow(ctx, key); err != nil {
		return nil, ErrBusy
	}
	select {
	case h.slots <- struct{}{}:
		select {
		case processSlots <- struct{}{}:
			return func() { <-processSlots; <-h.slots }, nil
		default:
			<-h.slots
			return nil, ErrBusy
		}
	default:
		return nil, ErrBusy
	}
}
func (h *Hasher) Hash(ctx context.Context, key, secret string) (string, error) {
	if h == nil || h.blocklist == nil || !h.blocklist.Ready() {
		return "", ErrUnavailable
	}
	s, err := normalized(secret)
	if err != nil {
		return "", err
	}
	if h.blocklist.ContainsNormalized(s) {
		return "", ErrInvalid
	}
	release, err := h.acquire(ctx, key)
	if err != nil {
		return "", err
	}
	defer release()
	return derive(s, 65536, 3)
}

type Result struct {
	Verified    bool
	NeedsRehash bool
	// RehashPersisted is false if an opportunistic update failed. Authentication
	// remains valid; callers may audit the failure without including the verifier.
	RehashPersisted bool
}

// PersistRehash must compare-and-swap the existing credential version in a
// transaction, so a concurrent password reset is never overwritten.
type PersistRehash func(context.Context, string, string) error

func (h *Hasher) Verify(ctx context.Context, key, secret, encoded string, persist PersistRehash) (Result, error) {
	s, err := normalized(secret)
	if err != nil {
		return Result{}, ErrInvalid
	}
	unknown := encoded == ""
	candidate := encoded
	if unknown {
		if h == nil {
			return Result{}, ErrUnavailable
		}
		candidate = h.dummy
	}
	p, err := parse(candidate)
	if err != nil {
		return Result{}, ErrInvalid
	}
	release, err := h.acquire(ctx, key)
	if err != nil {
		return Result{}, err
	}
	defer release()
	legacyDummy, _ := parse(h.legacyDummy)
	currentDummy, _ := parse(h.dummy)
	plan := verificationPlan(p, legacyDummy, currentDummy)
	legacy, current := plan[0], plan[1]
	legacyActual := argon2.IDKey([]byte(s), legacy.salt, 2, 32768, 1, 32)
	currentActual := argon2.IDKey([]byte(s), current.salt, 3, 65536, 1, 32)
	legacyMatch := subtle.ConstantTimeCompare(legacyActual, legacy.hash) == 1
	currentMatch := subtle.ConstantTimeCompare(currentActual, current.hash) == 1
	match := currentMatch
	if p.memory == 32768 {
		match = legacyMatch
	}
	if !match || unknown {
		return Result{}, nil
	}
	result := Result{Verified: true, NeedsRehash: p.memory != 65536 || p.iterations != 3}
	if result.NeedsRehash && persist != nil {
		replacement, e := derive(s, 65536, 3)
		if e == nil {
			result.RehashPersisted = persist(ctx, encoded, replacement) == nil
		}
	}
	return result, nil
}

type parameters struct {
	memory, iterations uint32
	salt, hash         []byte
}

// verificationPlan always executes the same ordered resource profiles. The
// candidate replaces only the corresponding dummy salt/hash, never the cost.
func verificationPlan(candidate, legacyDummy, currentDummy parameters) [2]parameters {
	plan := [2]parameters{legacyDummy, currentDummy}
	if candidate.memory == 32768 {
		plan[0] = candidate
	} else {
		plan[1] = candidate
	}
	return plan
}

func parse(encoded string) (parameters, error) {
	if len(encoded) > 256 {
		return parameters{}, ErrInvalid
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return parameters{}, ErrInvalid
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 || !strings.HasPrefix(fields[0], "m=") || !strings.HasPrefix(fields[1], "t=") || fields[2] != "p=1" {
		return parameters{}, ErrInvalid
	}
	m, e := strconv.ParseUint(strings.TrimPrefix(fields[0], "m="), 10, 32)
	if e != nil {
		return parameters{}, ErrInvalid
	}
	t, e := strconv.ParseUint(strings.TrimPrefix(fields[1], "t="), 10, 32)
	if e != nil {
		return parameters{}, ErrInvalid
	}
	// Only explicitly approved profiles are accepted, rather than trusting stored
	// arbitrary costs. The lower profile is verification-only for migration.
	if (m != 65536 || t != 3) && (m != 32768 || t != 2) {
		return parameters{}, ErrInvalid
	}
	salt, e := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return parameters{}, ErrInvalid
	}
	hash, e := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if e != nil || len(hash) != 32 {
		return parameters{}, ErrInvalid
	}
	canonical := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=1$%s$%s", m, t, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
	if canonical != encoded {
		return parameters{}, ErrInvalid
	}
	return parameters{uint32(m), uint32(t), salt, hash}, nil
}
func derive(secret string, memory, iterations uint32) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrUnavailable
	}
	out := argon2.IDKey([]byte(secret), salt, iterations, memory, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=1$%s$%s", memory, iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(out)), nil
}
