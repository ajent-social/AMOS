package restore

import (
	"github.com/ajent-social/amos/internal/backup"
	"testing"
)

func TestPlanMigrationCompatibility(t *testing.T) {
	current := []backup.MigrationVersion{{Sequence: 1, ID: "000001.base", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Sequence: 2, ID: "000002.next", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	if !migrationPrefix(current[:1], current) {
		t.Fatal("accepted historical prefix rejected")
	}
	if migrationPrefix(current[:1], current[1:]) {
		t.Fatal("non-prefix migration accepted")
	}
	broken := append([]backup.MigrationVersion(nil), current...)
	broken[0].SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if migrationPrefix(broken, current) {
		t.Fatal("changed migration checksum accepted")
	}
}
func TestPlanExactRestoredMigrationLedger(t *testing.T) {
	current := []backup.MigrationVersion{{Sequence: 1, ID: "000001.base", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Sequence: 2, ID: "000002.next", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	if !equalMigrations(current, current) {
		t.Fatal("identical restored migration ledger rejected")
	}
	if equalMigrations(current[:1], current) {
		t.Fatal("short restored migration ledger accepted")
	}
	changed := append([]backup.MigrationVersion(nil), current...)
	changed[1].SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if equalMigrations(changed, current) {
		t.Fatal("changed restored migration checksum accepted")
	}
}

func TestPlanModuleCompatibility(t *testing.T) {
	a := []backup.ModuleVersion{{ID: "identity", Version: "1.0"}, {ID: "workspace", Version: "2.0"}}
	b := []backup.ModuleVersion{{ID: "workspace", Version: "2.0"}, {ID: "identity", Version: "1.0"}}
	if !sameModules(a, b) {
		t.Fatal("module order changed compatibility")
	}
	b[0].Version = "3.0"
	if sameModules(a, b) {
		t.Fatal("changed module version accepted")
	}
}

func TestPlanChildEnvironment(t *testing.T) {
	got := cleanEnv([]string{"PATH=/bin", "PGDATABASE=wrong", "PGHOST=remote", "PGSERVICE=wrong", "SAFE=value"})
	if len(got) != 2 || got[0] != "PATH=/bin" || got[1] != "SAFE=value" {
		t.Fatalf("child environment retained PostgreSQL overrides: %v", got)
	}
}
