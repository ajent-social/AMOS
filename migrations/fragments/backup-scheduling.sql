CREATE TABLE amos_backup_schedules (
 schedule_id uuid PRIMARY KEY,
 installation_id uuid NOT NULL,
 application_id uuid NOT NULL,
 environment text NOT NULL CHECK(environment IN ('development','staging','production')),
 config_digest bytea NOT NULL CHECK(octet_length(config_digest)=32),
 fence bigint NOT NULL DEFAULT 0 CHECK(fence>=0),
 retained_attempts integer NOT NULL DEFAULT 0 CHECK(retained_attempts BETWEEN 0 AND 4096),
 skipped_slots bigint NOT NULL DEFAULT 0 CHECK(skipped_slots>=0),
 last_slot_at timestamptz,
 clock_watermark timestamptz NOT NULL DEFAULT clock_timestamp(),
 state text NOT NULL DEFAULT 'idle' CHECK(state IN ('idle','running','completed','failed','unknown','capacity','clock_invalid')),
 failure_class text NOT NULL DEFAULT '' CHECK(failure_class IN ('','dump','upload','integrity','identity','cancelled','lease_lost','unknown','database','capacity','clock')),
 active_attempt_id uuid,
 lease_until timestamptz,
 last_attempt_id uuid,
 last_attempted_at timestamptz,
 last_attempt_completed_at timestamptz,
 last_attempt_duration_ms bigint NOT NULL DEFAULT 0 CHECK(last_attempt_duration_ms>=0),
 last_success_attempt_id uuid,
 last_published_at timestamptz,
 last_snapshot_at timestamptz,
 UNIQUE(installation_id,application_id,environment),
 CHECK((active_attempt_id IS NULL)=(lease_until IS NULL)),
 CHECK(state<>'running' OR active_attempt_id IS NOT NULL),
 CHECK((last_attempt_id IS NULL)=(last_attempted_at IS NULL)),
 CHECK((last_success_attempt_id IS NULL)=(last_published_at IS NULL)),
 CHECK((last_success_attempt_id IS NULL)=(last_snapshot_at IS NULL)),
 CHECK(last_snapshot_at IS NULL OR last_snapshot_at<=last_published_at)
);
CREATE TABLE amos_backup_attempts (
 attempt_id uuid PRIMARY KEY,
 schedule_id uuid NOT NULL REFERENCES amos_backup_schedules(schedule_id),
 slot_at timestamptz NOT NULL,
 fence bigint NOT NULL CHECK(fence>0),
 started_at timestamptz NOT NULL,
 finished_at timestamptz,
 duration_ms bigint NOT NULL DEFAULT 0 CHECK(duration_ms>=0),
 state text NOT NULL CHECK(state IN ('running','completed','failed','unknown')),
 failure_class text NOT NULL DEFAULT '' CHECK(failure_class IN ('','dump','upload','integrity','identity','cancelled','lease_lost','unknown','database','clock')),
 snapshot_at timestamptz,
 published_at timestamptz,
 archive_version_id text CHECK(archive_version_id IS NULL OR octet_length(archive_version_id) BETWEEN 1 AND 1024),
 manifest_version_id text CHECK(manifest_version_id IS NULL OR octet_length(manifest_version_id) BETWEEN 1 AND 1024),
 UNIQUE(schedule_id,slot_at),
 CHECK(finished_at IS NULL OR finished_at>=started_at),
 CHECK(state<>'completed' OR (finished_at IS NOT NULL AND snapshot_at IS NOT NULL AND published_at IS NOT NULL AND archive_version_id IS NOT NULL AND manifest_version_id IS NOT NULL AND snapshot_at<=published_at)),
 CHECK(pg_column_size(archive_version_id)+pg_column_size(manifest_version_id)<4096 OR archive_version_id IS NULL OR manifest_version_id IS NULL)
);
-- FK identity includes schedule_id, so a retained schedule pointer cannot name
-- another stream's attempt. Unique index supports these immutable foreign keys.
ALTER TABLE amos_backup_attempts ADD CONSTRAINT amos_backup_attempt_schedule_key UNIQUE(schedule_id,attempt_id);
ALTER TABLE amos_backup_schedules ADD CONSTRAINT amos_backup_active_fk FOREIGN KEY(schedule_id,active_attempt_id) REFERENCES amos_backup_attempts(schedule_id,attempt_id);
ALTER TABLE amos_backup_schedules ADD CONSTRAINT amos_backup_last_fk FOREIGN KEY(schedule_id,last_attempt_id) REFERENCES amos_backup_attempts(schedule_id,attempt_id);
ALTER TABLE amos_backup_schedules ADD CONSTRAINT amos_backup_success_fk FOREIGN KEY(schedule_id,last_success_attempt_id) REFERENCES amos_backup_attempts(schedule_id,attempt_id);

ALTER TABLE amos_backup_attempts ADD CONSTRAINT amos_backup_attempt_fence_key UNIQUE(schedule_id,fence);
ALTER TABLE amos_backup_attempts ADD CONSTRAINT amos_backup_completed_clean CHECK(state <> 'completed' OR failure_class='');
ALTER TABLE amos_backup_schedules ADD CONSTRAINT amos_backup_completed_success CHECK(state <> 'completed' OR last_success_attempt_id IS NOT NULL);
REVOKE ALL ON amos_backup_schedules, amos_backup_attempts FROM PUBLIC;
