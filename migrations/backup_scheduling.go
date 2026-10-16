package migrations

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// BackupScheduling is opt-in for a separate operational database. It must not
// be appended implicitly to an application's existing migration registry.
func BackupScheduling(sequence uint64) (Fragment, error) {
	if sequence == 0 || sequence > math.MaxInt64 {
		return Fragment{}, ErrInvalidRegistry
	}
	source, err := fragments.ReadFile("fragments/backup-scheduling.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "backup", Migrations: []Migration{{Sequence: sequence, Name: "scheduled_publications", SQL: string(source)}}}, nil
}

var ErrBackupSchedulingGrants = errors.New("invalid backup scheduling role grants")
var backupRoleName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// BackupSchedulingGrants returns owner-applied table grants for existing distinct
// roles in the dedicated operational database. It creates no database or role,
// grants no application-data access, and never applies itself during a run.
func BackupSchedulingGrants(writer, reader string) (string, error) {
	valid := func(name string) bool {
		return backupRoleName.MatchString(name) && name != "public" && name != "postgres" && !strings.HasPrefix(name, "pg_")
	}
	if !valid(writer) || !valid(reader) || writer == reader {
		return "", ErrBackupSchedulingGrants
	}
	return fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO "%s", "%s";
GRANT SELECT ON amos_schema_migrations, amos_backup_schedules, amos_backup_attempts TO "%s", "%s";
GRANT INSERT ON amos_backup_schedules, amos_backup_attempts TO "%s";
GRANT UPDATE (fence, retained_attempts, skipped_slots, last_slot_at, clock_watermark, state, failure_class, active_attempt_id, lease_until, last_attempt_id, last_attempted_at, last_attempt_completed_at, last_attempt_duration_ms, last_success_attempt_id, last_published_at, last_snapshot_at) ON amos_backup_schedules TO "%s";
GRANT UPDATE (finished_at, duration_ms, state, failure_class, snapshot_at, published_at, archive_version_id, manifest_version_id) ON amos_backup_attempts TO "%s";
`, writer, reader, writer, reader, writer, writer, writer), nil
}
