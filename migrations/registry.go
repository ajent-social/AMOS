// Package migrations defines the immutable, globally ordered migration registry.
// Domain teams contribute namespaced fragments; the integrator assigns sequence
// numbers and assembles the registry.
package migrations

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

var (
	// ErrInvalidRegistry identifies a registry with invalid names or ordering.
	ErrInvalidRegistry = errors.New("invalid migration registry")
	namespacePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	namePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)
)

// Migration is one transactional SQL migration. Sequence is assigned by the
// integrator and is contiguous across all namespaces.
type Migration struct {
	Sequence  uint64
	Namespace string
	Name      string
	SQL       string
}

// ID returns the stable ID persisted in the migration checksum ledger.
func (m Migration) ID() string {
	return fmt.Sprintf("%06d.%s.%s", m.Sequence, m.Namespace, m.Name)
}

// Fragment groups migrations owned by one domain. Sequence values remain
// globally assigned by the integrator, not locally allocated by a fragment.
type Fragment struct {
	Namespace  string
	Migrations []Migration
}

// Registry is a validated, immutable ordering of domain migration fragments.
type Registry struct {
	migrations []Migration
}

// NewRegistry validates and copies fragments into global sequence order.
// Sequences must start at 1 and be contiguous; duplicate sequences and
// duplicate names within a namespace are rejected.
func NewRegistry(fragments ...Fragment) (Registry, error) {
	var all []Migration
	seenNames := make(map[string]struct{})
	for _, fragment := range fragments {
		if !namespacePattern.MatchString(fragment.Namespace) {
			return Registry{}, fmt.Errorf("%w: invalid namespace %q", ErrInvalidRegistry, fragment.Namespace)
		}
		for _, migration := range fragment.Migrations {
			if migration.Namespace != "" && migration.Namespace != fragment.Namespace {
				return Registry{}, fmt.Errorf("%w: migration namespace differs from fragment %q", ErrInvalidRegistry, fragment.Namespace)
			}
			migration.Namespace = fragment.Namespace
			if migration.Sequence == 0 || migration.Sequence > math.MaxInt64 || !namePattern.MatchString(migration.Name) || strings.TrimSpace(migration.SQL) == "" {
				return Registry{}, fmt.Errorf("%w: migration requires a positive sequence, valid name, and non-empty SQL", ErrInvalidRegistry)
			}
			nameKey := migration.Namespace + "." + migration.Name
			if _, exists := seenNames[nameKey]; exists {
				return Registry{}, fmt.Errorf("%w: duplicate migration name %q", ErrInvalidRegistry, nameKey)
			}
			seenNames[nameKey] = struct{}{}
			all = append(all, migration)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Sequence < all[j].Sequence })
	for index, migration := range all {
		want := uint64(index + 1)
		if migration.Sequence != want {
			return Registry{}, fmt.Errorf("%w: expected sequence %d, found %d", ErrInvalidRegistry, want, migration.Sequence)
		}
	}
	return Registry{migrations: all}, nil
}

// Migrations returns a copy of the validated registry entries.
func (r Registry) Migrations() []Migration {
	return append([]Migration(nil), r.migrations...)
}
