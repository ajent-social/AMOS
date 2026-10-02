package federation

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ajent-social/amos/migrations"
)

//go:embed schema.sql
var schemaFS embed.FS

// Fragment returns the additive flow schema at an integrator-assigned sequence.
func Fragment(sequence uint64) (migrations.Fragment, error) {
	if sequence == 0 {
		return migrations.Fragment{}, ErrInvalidInput
	}
	data, err := fs.ReadFile(schemaFS, "schema.sql")
	if err != nil {
		return migrations.Fragment{}, fmt.Errorf("read federation schema: %w", err)
	}
	return migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{
		Sequence: sequence, Namespace: "identity", Name: "federation_flows", SQL: strings.TrimSpace(string(data)),
	}}}, nil
}
