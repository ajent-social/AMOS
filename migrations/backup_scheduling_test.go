package migrations

import (
	"strings"
	"testing"
)

func TestBackupSchedulingOptInRegistry(t *testing.T) {
	f, e := BackupScheduling(1)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewRegistry(f)
	if e != nil {
		t.Fatal(e)
	}
	m := r.Migrations()
	if len(m) != 1 || m[0].ID() != "000001.backup.scheduled_publications" {
		t.Fatal("wrong operational registry")
	}
	for _, sequence := range []uint64{0, ^uint64(0)} {
		if _, e := BackupScheduling(sequence); e == nil {
			t.Fatal("invalid sequence accepted")
		}
	}
}
func TestBackupGrantsRejectRoleInjection(t *testing.T) {
	for _, roles := range [][2]string{{"public", "backup_reader"}, {"pg_admin", "backup_reader"}, {"postgres", "backup_reader"}, {"backup_writer", "backup_writer"}, {"writer\";DROP ROLE example;--", "backup_reader"}, {"backup_writer", ""}} {
		if _, e := BackupSchedulingGrants(roles[0], roles[1]); e == nil {
			t.Fatal("unsafe role accepted")
		}
	}
	s, e := BackupSchedulingGrants("backup_writer", "backup_reader")
	if e != nil {
		t.Fatal(e)
	}
	for _, forbidden := range []string{"GRANT ALL", "DELETE", "TRUNCATE", "CREATE ROLE", "GRANT UPDATE ON", "GRANT UPDATE (schedule_id", "GRANT UPDATE (config_digest"} {
		if strings.Contains(s, forbidden) {
			t.Fatal("excess grant", forbidden)
		}
	}
}

func TestBackupGrantsAcceptShortOrdinaryRoles(t *testing.T) {
	if _, err := BackupSchedulingGrants("a", "bc"); err != nil {
		t.Fatal("ordinary short roles rejected")
	}
}
