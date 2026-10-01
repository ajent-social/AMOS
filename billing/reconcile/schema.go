package reconcile

import (
	"embed"
	"fmt"

	"github.com/ajent-social/amos/migrations"
)

//go:embed schema.sql
var schemaFiles embed.FS

// Schema returns the additive reconciliation schema at the caller's global
// migration sequence. The application integrator owns sequence allocation.
func Schema(sequence uint64) (migrations.Fragment, error) {
	if sequence == 0 {
		return migrations.Fragment{}, fmt.Errorf("reconciliation schema requires a positive sequence")
	}
	sql, err := schemaFiles.ReadFile("schema.sql")
	if err != nil {
		return migrations.Fragment{}, fmt.Errorf("read reconciliation schema: %w", err)
	}
	return migrations.Fragment{Namespace: "billing_reconcile", Migrations: []migrations.Migration{{Sequence: sequence, Name: "reconciliation_work", SQL: string(sql)}}}, nil
}
