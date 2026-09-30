package migrations

import (
	"errors"
	"testing"
)

func TestNewRegistryOrdersNamespacedFragments(t *testing.T) {
	registry, err := NewRegistry(
		Fragment{Namespace: "workspace", Migrations: []Migration{{Sequence: 2, Name: "add_members", SQL: "CREATE TABLE members(id text);"}}},
		Fragment{Namespace: "identity", Migrations: []Migration{{Sequence: 1, Name: "create_people", SQL: "CREATE TABLE people(id text);"}}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	got := registry.Migrations()
	if len(got) != 2 {
		t.Fatalf("got %d migrations, want 2", len(got))
	}
	if got[0].Sequence != 1 || got[0].ID() != "000001.identity.create_people" {
		t.Fatalf("first migration = %#v, want sequence 1 identity.create_people", got[0])
	}
	if got[1].Sequence != 2 || got[1].ID() != "000002.workspace.add_members" {
		t.Fatalf("second migration = %#v, want sequence 2 workspace.add_members", got[1])
	}
	got[0].Name = "mutated"
	if registry.Migrations()[0].Name != "create_people" {
		t.Fatal("Migrations() exposed the registry's backing slice")
	}
}

func TestNewRegistryRejectsInvalidSequences(t *testing.T) {
	tests := []struct {
		name      string
		fragments []Fragment
	}{
		{name: "gap", fragments: []Fragment{{Namespace: "identity", Migrations: []Migration{{Sequence: 1, Name: "one", SQL: "SELECT 1"}, {Sequence: 3, Name: "three", SQL: "SELECT 3"}}}}},
		{name: "duplicate sequence", fragments: []Fragment{{Namespace: "identity", Migrations: []Migration{{Sequence: 1, Name: "one", SQL: "SELECT 1"}}}, {Namespace: "workspace", Migrations: []Migration{{Sequence: 1, Name: "other", SQL: "SELECT 2"}}}}},
		{name: "duplicate name", fragments: []Fragment{{Namespace: "identity", Migrations: []Migration{{Sequence: 1, Name: "same", SQL: "SELECT 1"}, {Sequence: 2, Name: "same", SQL: "SELECT 2"}}}}},
		{name: "invalid namespace", fragments: []Fragment{{Namespace: "Identity", Migrations: []Migration{{Sequence: 1, Name: "one", SQL: "SELECT 1"}}}}},
		{name: "empty SQL", fragments: []Fragment{{Namespace: "identity", Migrations: []Migration{{Sequence: 1, Name: "one", SQL: "  "}}}}},
		{name: "sequence exceeds PostgreSQL bigint", fragments: []Fragment{{Namespace: "identity", Migrations: []Migration{{Sequence: ^uint64(0), Name: "one", SQL: "SELECT 1"}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRegistry(test.fragments...)
			if !errors.Is(err, ErrInvalidRegistry) {
				t.Fatalf("NewRegistry() error = %v, want ErrInvalidRegistry", err)
			}
		})
	}
}
