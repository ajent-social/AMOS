package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/google/uuid"
)

func backupAssetsFixture() BackupAssetsConfig {
	return BackupAssetsConfig{Version: BackupAssetsVersion, Profile: opsconfig.ProfileAWSVM, VM: &BackupVMAssets{Executable: "/opt/amos", ExpectedExecutableSHA256: strings.Repeat("a", 64), ConfigFile: "/etc/amos/backup.json", ExpectedConfigSHA256: strings.Repeat("b", 64), WriteEnvironmentFile: "/etc/amos/write.env", ReadEnvironmentFile: "/etc/amos/read.env", RunnerUser: "backup_run", RunnerGroup: "backup_run", ReaderUser: "backup_read", ReaderGroup: "backup_read", RunService: "amos-backup-run.service", RunTimer: "amos-backup-run.timer", CheckService: "amos-backup-check.service", CheckTimer: "amos-backup-check.timer"}}
}
func backupScheduleFixture(t *testing.T) backup.ScheduleConfig {
	t.Helper()
	id := func() uuid.UUID {
		u, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	installation := id()
	targets := fixtureConfig().Targets
	targets.Activated = false
	return backup.ScheduleConfig{Version: backup.ScheduleConfigVersion, ScheduleID: id(), InstallationID: installation, ApplicationID: id(), Environment: "development", Targets: targets, Database: backup.ScheduleDatabaseConfig{LedgerWriteRef: "env://BACKUP_LEDGER_WRITE", LedgerReadRef: "env://BACKUP_LEDGER_READ", DumpRef: "env://BACKUP_DUMP", LedgerWriteRole: "backup_writer", LedgerReadRole: "backup_reader", DumpRole: "backup_dump", LedgerHost: "database.example", LedgerDatabase: "backup_ops", LedgerRootCAFile: "/etc/amos/ca.pem", DumpHost: "database.example", DumpDatabase: "application", DumpRootCAFile: "/etc/amos/ca.pem", LedgerPort: 5432, DumpPort: 5432}, AWS: backup.ScheduleAWSConfig{CredentialKind: "imds_v2", AccountID: "123456789012", Region: "us-east-1", RuntimeRoleARN: "arn:aws:iam::123456789012:role/backup-run", BucketAccountID: "123456789012", Bucket: "example-backup-objects", Prefix: "installations/" + installation.String() + "/development", KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/00000000-0000-4000-8000-000000000001"}, Create: backup.ScheduleCreateConfig{StagingDir: "/var/lib/amos-backup", PGDumpPath: "/usr/bin/pg_dump", PGDumpVersion: "16.15", ModuleVersions: map[string]string{"core": "1"}, MaxBytes: 1 << 20, DumpTimeout: time.Minute}, UploadTimeout: time.Minute, RunTimeout: 3 * time.Minute, MaxRetainedAttempts: 100, MaxStagingArtifacts: 100, MaxStagingBytes: 1 << 30}
}
func TestBackupAssetsStrictUnion(t *testing.T) {
	a := backupAssetsFixture()
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeBackupAssets(bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	cases := map[string][]byte{"duplicate": bytes.Replace(b, []byte(`"version":`), []byte(`"version":"amos-backup-assets-v1","version":`), 1), "alias": bytes.Replace(b, []byte(`"profile"`), []byte(`"Profile"`), 1), "null": bytes.Replace(b, []byte(`"vm":{`), []byte(`"managed":null,"vm":{`), 1), "trailing": append(append([]byte{}, b...), []byte(` {}`)...), "both_profiles": bytes.Replace(b, []byte(`"vm":{`), []byte(`"managed":{},"vm":{`), 1), "wrong_scalar": bytes.Replace(b, []byte(`"runner_user":"backup_run"`), []byte(`"runner_user":7`), 1), "nested_duplicate": bytes.Replace(b, []byte(`"runner_user":`), []byte(`"runner_user":"backup_run","runner_user":`), 1), "oversized": bytes.Repeat([]byte(" "), MaxDefinitionBytes+1)}
	for n, b := range cases {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeBackupAssets(bytes.NewReader(b)); e == nil {
				t.Fatal("invalid assets accepted")
			}
		})
	}
	for n, change := range map[string]func(*BackupVMAssets){"root": func(v *BackupVMAssets) { v.RunnerUser = "root" }, "shared_identity": func(v *BackupVMAssets) { v.ReaderUser = v.RunnerUser }, "shared_secret_file": func(v *BackupVMAssets) { v.ReadEnvironmentFile = v.WriteEnvironmentFile }, "injection": func(v *BackupVMAssets) { v.ConfigFile = "/etc/backup\nUser=root" }, "unpaired_units": func(v *BackupVMAssets) { v.CheckTimer = "other.timer" }, "traversal": func(v *BackupVMAssets) { v.ConfigFile = "/etc/../backup" }, "specifier": func(v *BackupVMAssets) { v.ConfigFile = "/etc/%h/backup" }} {
		t.Run(n, func(t *testing.T) {
			a := backupAssetsFixture()
			change(a.VM)
			b, _ := json.Marshal(a)
			if _, e := DecodeBackupAssets(bytes.NewReader(b)); e == nil {
				t.Fatal("unsafe installation declaration")
			}
		})
	}
}
func TestBackupVMActualRenderedUnits(t *testing.T) {
	c := backupScheduleFixture(t)
	a := backupAssetsFixture()
	b, e := RenderBackupAssets(c, a)
	if e != nil {
		t.Fatal(e)
	}
	again, e := RenderBackupAssets(c, a)
	if e != nil || !bytes.Equal(b, again) {
		t.Fatal("nondeterministic")
	}
	var bundle backupAssetBundle
	if json.Unmarshal(b, &bundle) != nil {
		t.Fatal("invalid output")
	}
	if bundle.Activated || len(b) > MaxDefinitionBytes || !strings.Contains(bundle.Qualification, "unverified") {
		t.Fatal("qualified or activated by rendering")
	}
	run, check := bundle.Files[a.VM.RunService], bundle.Files[a.VM.CheckService]
	if !strings.Contains(run, "ExecStart=/opt/amos backup run --config /etc/amos/backup.json\n") || !strings.Contains(check, "ExecStart=/opt/amos backup check --config /etc/amos/backup.json\n") || !strings.Contains(run, "EnvironmentFile=/etc/amos/write.env\n") || !strings.Contains(check, "EnvironmentFile=/etc/amos/read.env\n") || strings.Contains(check, "write.env") || strings.Contains(check, "ReadWritePaths=") || strings.Contains(run, "read.env") || !strings.Contains(run, "TimeoutStartSec=195\n") {
		t.Fatal("incorrect service invocations")
	}
	for _, s := range []string{run, check} {
		if strings.Contains(s, "[Install]") || !strings.Contains(s, "UMask=0077\n") || !strings.Contains(s, "NoNewPrivileges=yes\n") || !strings.Contains(s, "PrivateTmp=no\n") || !strings.Contains(s, "ProtectHome=read-only\n") {
			t.Fatal("service hardening/activation")
		}
	}
	if !strings.Contains(bundle.Files[a.VM.RunTimer], "Unit="+a.VM.RunService+"\n") || !strings.Contains(bundle.Files[a.VM.CheckTimer], "Unit="+a.VM.CheckService+"\n") {
		t.Fatal("incorrect timer target")
	}
	c.Targets.Profile = opsconfig.ProfileAWSManaged
	if _, e := RenderBackupAssets(c, a); e == nil {
		t.Fatal("profile mismatch accepted")
	}
	if _, e := ReadBackupObservation(context.Background(), nil); e == nil {
		t.Fatal("nil reader became authority")
	}
}

func TestBackupManagedRenderedRecipeAndBinding(t *testing.T) {
	c := backupScheduleFixture(t)
	c.Targets.Profile = opsconfig.ProfileAWSManaged
	c.AWS.CredentialKind = "ecs_task"
	role := func(s string) string { return "arn:aws:iam::123456789012:role/" + s }
	task := func(s string) BackupTaskAssets {
		return BackupTaskAssets{TaskDefinitionARN: "arn:aws:ecs:us-east-1:123456789012:task-definition/" + s + ":7", ContainerName: s, ExpectedImage: "registry.example/backup@sha256:" + strings.Repeat("a", 64), Executable: "/opt/amos", ConfigFile: "/etc/amos/backup.json", ExpectedConfigSHA256: strings.Repeat("b", 64), TaskRoleARN: role(s + "-task"), ExecutionRoleARN: role(s + "-execution"), EnvironmentFileARN: "arn:aws:s3:::example-backup-env/" + s + ".env"}
	}
	m := BackupManagedAssets{ClusterARN: "arn:aws:ecs:us-east-1:123456789012:cluster/example", ScheduleGroupARN: "arn:aws:scheduler:us-east-1:123456789012:schedule-group/backup", RunScheduleName: "backup-run", CheckScheduleName: "backup-check", RunSchedulerRoleARN: role("run-invoke"), CheckSchedulerRoleARN: role("check-invoke"), SubnetIDs: []string{"subnet-01234567"}, SecurityGroupIDs: []string{"sg-01234567"}, Run: task("run"), Check: task("check")}
	c.AWS.RuntimeRoleARN = m.Run.TaskRoleARN
	a := BackupAssetsConfig{Version: BackupAssetsVersion, Profile: opsconfig.ProfileAWSManaged, Managed: &m}
	raw, _ := json.Marshal(a)
	decoded, e := DecodeBackupAssets(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	body, e := RenderBackupAssets(c, decoded)
	if e != nil {
		t.Fatal(e)
	}
	var out backupAssetBundle
	if json.Unmarshal(body, &out) != nil {
		t.Fatal("invalid managed bundle")
	}
	source := out.Files["backup_schedule.go"]
	if !strings.Contains(source, `"github.com/ajent-social/amos/infra/backupschedule"`) || strings.Contains(source, "/internal/") || !strings.Contains(source, "provider *aws.Provider") || !strings.Contains(source, `c, false, provider)`) {
		t.Fatal("recipe not public, explicit-provider and disabled")
	}
	if out.Declaration == nil || !strings.Contains(out.Declaration.RunTrust, m.ScheduleGroupARN) {
		t.Fatal("missing exact group trust declaration")
	}
	c.AWS.RuntimeRoleARN = role("different-runtime")
	if _, e := RenderBackupAssets(c, a); e == nil {
		t.Fatal("runtime/task role mismatch accepted")
	}
}

func TestBackupVMDoesNotHideSelectedProtectedPaths(t *testing.T) {
	for _, base := range []string{"/tmp/private-backup", "/var/tmp/private-backup", path.Join("/", "home", "example", "backup"), "/root/backup", "/run/user/1001/backup"} {
		t.Run(base, func(t *testing.T) {
			c := backupScheduleFixture(t)
			a := backupAssetsFixture()
			a.VM.Executable = base + "/amos"
			a.VM.ConfigFile = base + "/config.json"
			c.Create.StagingDir = base + "/staging"
			c.Create.PGDumpPath = base + "/pg_dump"
			c.Database.LedgerRootCAFile = base + "/ca.pem"
			c.Database.DumpRootCAFile = base + "/ca.pem"
			body, err := RenderBackupAssets(c, a)
			if err != nil {
				t.Fatal(err)
			}
			var b backupAssetBundle
			if json.Unmarshal(body, &b) != nil {
				t.Fatal("invalid bundle")
			}
			s := b.Files[a.VM.RunService]
			if strings.Contains(s, "PrivateTmp=yes") || strings.Contains(s, "ProtectHome=yes") || !strings.Contains(s, "ReadWritePaths="+c.Create.StagingDir+"\n") {
				t.Fatal("selected paths hidden by namespace settings")
			}
		})
	}
}
