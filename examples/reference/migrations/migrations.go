// Package migrations supplies the reference application's business fragment.
package migrations

import (
	_ "embed"
	core "github.com/ajent-social/amos/migrations"
)

//go:embed todos.sql
var todos string

// Fragment follows the immutable seven-entry foundation registry.
func Fragment() core.Fragment {
	return core.Fragment{Namespace: "reference", Migrations: []core.Migration{{Sequence: 8, Name: "todos", SQL: todos}}}
}
