package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ajent-social/amos/infra/backupschedule"
	"github.com/ajent-social/amos/internal/backup"
	"github.com/ajent-social/amos/internal/opsconfig"
)

const BackupAssetsVersion = "amos-backup-assets-v1"

type BackupTaskAssets = backupschedule.TaskAssets
type BackupManagedAssets = backupschedule.ManagedAssets
type BackupAssetsConfig struct {
	Version string               `json:"version"`
	Profile opsconfig.Profile    `json:"profile"`
	VM      *BackupVMAssets      `json:"vm,omitempty"`
	Managed *BackupManagedAssets `json:"managed,omitempty"`
}
type BackupVMAssets struct {
	Executable               string `json:"executable"`
	ExpectedExecutableSHA256 string `json:"expected_executable_sha256"`
	ConfigFile               string `json:"config_file"`
	ExpectedConfigSHA256     string `json:"expected_config_sha256"`
	WriteEnvironmentFile     string `json:"write_environment_file"`
	ReadEnvironmentFile      string `json:"read_environment_file"`
	RunnerUser               string `json:"runner_user"`
	RunnerGroup              string `json:"runner_group"`
	ReaderUser               string `json:"reader_user"`
	ReaderGroup              string `json:"reader_group"`
	RunService               string `json:"run_service"`
	RunTimer                 string `json:"run_timer"`
	CheckService             string `json:"check_service"`
	CheckTimer               string `json:"check_timer"`
}

// DecodeBackupAssets validates the exact closed profile union before decoding.
// JSON object aliases, duplicate decoded names and null are never accepted.
func DecodeBackupAssets(r io.Reader) (BackupAssetsConfig, error) {
	var out BackupAssetsConfig
	if r == nil {
		return out, ErrInvalid
	}
	b, e := io.ReadAll(io.LimitReader(r, MaxDefinitionBytes+1))
	if e != nil || len(b) == 0 || len(b) > MaxDefinitionBytes || !utf8.Valid(b) {
		return out, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return out, ErrInvalid
	}
	raw := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return out, ErrInvalid
		}
		key, ok := k.(string)
		if !ok || raw[key] != nil || (key != "version" && key != "profile" && key != "vm" && key != "managed") {
			return out, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return out, ErrInvalid
		}
		raw[key] = v
	}
	t, e = d.Token()
	if e != nil || t != json.Delim('}') || len(raw) != 3 {
		return out, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return out, ErrInvalid
	}
	if strict(raw["version"], &out.Version, MaxDefinitionBytes) != nil || strict(raw["profile"], &out.Profile, MaxDefinitionBytes) != nil || out.Version != BackupAssetsVersion {
		return out, ErrInvalid
	}
	switch out.Profile {
	case opsconfig.ProfileAWSVM:
		if raw["vm"] == nil || raw["managed"] != nil {
			return BackupAssetsConfig{}, ErrInvalid
		}
		out.VM = &BackupVMAssets{}
		if strict(raw["vm"], out.VM, MaxDefinitionBytes) != nil {
			return BackupAssetsConfig{}, ErrInvalid
		}
	case opsconfig.ProfileAWSManaged:
		if raw["managed"] == nil || raw["vm"] != nil {
			return BackupAssetsConfig{}, ErrInvalid
		}
		out.Managed = &BackupManagedAssets{}
		if strict(raw["managed"], out.Managed, MaxDefinitionBytes) != nil {
			return BackupAssetsConfig{}, ErrInvalid
		}
	default:
		return BackupAssetsConfig{}, ErrInvalid
	}
	if validateBackupAssets(out) != nil {
		return BackupAssetsConfig{}, ErrInvalid
	}
	return out, nil
}

var backupPOSIX = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var backupUnit = regexp.MustCompile(`^[a-z][a-z0-9_-]*\.(service|timer)$`)

func validateBackupAssets(c BackupAssetsConfig) error {
	if c.Version != BackupAssetsVersion {
		return ErrInvalid
	}
	if c.Profile == opsconfig.ProfileAWSManaged {
		if c.VM != nil || c.Managed == nil {
			return ErrInvalid
		}
		return backupschedule.Validate(*c.Managed)
	}
	if c.Profile != opsconfig.ProfileAWSVM || c.Managed != nil || c.VM == nil {
		return ErrInvalid
	}
	v := c.VM
	for _, s := range []string{v.Executable, v.ConfigFile, v.WriteEnvironmentFile, v.ReadEnvironmentFile} {
		if !backupschedule.PathValid(s) {
			return ErrInvalid
		}
	}
	// Aliased executable/config/secret paths cannot satisfy file separation.
	seen := map[string]bool{}
	for _, s := range []string{v.Executable, v.ConfigFile, v.WriteEnvironmentFile, v.ReadEnvironmentFile} {
		if seen[s] {
			return ErrInvalid
		}
		seen[s] = true
	}
	if !backupschedule.SHA256Valid(v.ExpectedExecutableSHA256) || !backupschedule.SHA256Valid(v.ExpectedConfigSHA256) {
		return ErrInvalid
	}
	for _, s := range []string{v.RunnerUser, v.RunnerGroup, v.ReaderUser, v.ReaderGroup} {
		if !backupPOSIX.MatchString(s) || s == "root" {
			return ErrInvalid
		}
	}
	if v.RunnerUser == v.ReaderUser || v.RunnerGroup == v.ReaderGroup {
		return ErrInvalid
	}
	seen = map[string]bool{}
	for i, s := range []string{v.RunService, v.RunTimer, v.CheckService, v.CheckTimer} {
		suffix := ".service"
		if i%2 == 1 {
			suffix = ".timer"
		}
		if len(s) > 64 || !backupUnit.MatchString(s) || !strings.HasSuffix(s, suffix) || seen[s] {
			return ErrInvalid
		}
		seen[s] = true
	}
	if strings.TrimSuffix(v.RunService, ".service") != strings.TrimSuffix(v.RunTimer, ".timer") || strings.TrimSuffix(v.CheckService, ".service") != strings.TrimSuffix(v.CheckTimer, ".timer") {
		return ErrInvalid
	}
	return nil
}

type backupAssetBundle struct {
	Version       string                      `json:"version"`
	Profile       opsconfig.Profile           `json:"profile"`
	Activated     bool                        `json:"activated"`
	Qualification string                      `json:"qualification"`
	Files         map[string]string           `json:"files"`
	Managed       *BackupManagedAssets        `json:"managed,omitempty"`
	Declaration   *backupschedule.Declaration `json:"declaration,omitempty"`
}

const backupQualification = "Declared, unverified installation references. No credentials resolved, network calls, installation, enabling or deployment performed. Owner must verify executable/image/config digests, entrypoint, identities, protected files, secret-file separation, effective IAM/trust and private routes before installation. Live provider, trigger delivery and restore qualification remain separate."

// RenderBackupAssets emits a bounded deterministic stdout bundle. No file or
// environment reference is resolved and no resource is registered here.
func RenderBackupAssets(c backup.ScheduleConfig, a BackupAssetsConfig) ([]byte, error) {
	if backup.ValidateScheduleConfig(c) != nil || validateBackupAssets(a) != nil || c.Targets.Profile != a.Profile {
		return nil, ErrInvalid
	}
	bundle := backupAssetBundle{Version: BackupAssetsVersion, Profile: a.Profile, Activated: c.Targets.Activated, Qualification: backupQualification, Files: map[string]string{}}
	if a.Profile == opsconfig.ProfileAWSVM {
		v := a.VM
		deadline := int64((c.RunTimeout + 15*time.Second + time.Second - 1) / time.Second)
		service := func(command, user, group, env string) string {
			body := fmt.Sprintf("[Unit]\nDescription=AMOS backup %s\n\n[Service]\nType=oneshot\nUser=%s\nGroup=%s\nEnvironmentFile=%s\nExecStart=%s backup %s --config %s\nTimeoutStartSec=%d\nTimeoutStopSec=15\nUMask=0077\nNoNewPrivileges=yes\nPrivateTmp=no\nProtectHome=read-only\nProtectSystem=strict\n", command, user, group, env, v.Executable, command, v.ConfigFile, deadline)
			if command == "run" {
				body += "ReadWritePaths=" + c.Create.StagingDir + "\n"
			}
			return body
		}
		bundle.Files[v.RunService] = service("run", v.RunnerUser, v.RunnerGroup, v.WriteEnvironmentFile)
		bundle.Files[v.CheckService] = service("check", v.ReaderUser, v.ReaderGroup, v.ReadEnvironmentFile)
		timer := func(unit string) string {
			return "[Unit]\nDescription=AMOS backup wakeup\n\n[Timer]\nOnCalendar=*-*-* *:*:00\nAccuracySec=1s\nUnit=" + unit + "\n\n[Install]\nWantedBy=timers.target\n"
		}
		bundle.Files[v.RunTimer] = timer(v.RunService)
		bundle.Files[v.CheckTimer] = timer(v.CheckService)
		// Include the digests as installation declarations, never verification claims.
		b, _ := json.Marshal(v)
		bundle.Files["installation.json"] = string(b)
	} else {
		m := a.Managed
		if m.Run.TaskRoleARN != c.AWS.RuntimeRoleARN {
			return nil, ErrInvalid
		}
		d, e := backupschedule.Describe(*m)
		if e != nil || d.ScopeAccount != c.AWS.AccountID || d.ScopeRegion != c.AWS.Region {
			return nil, ErrInvalid
		}
		bundle.Managed = m
		bundle.Declaration = &d
		b, e := json.Marshal(m)
		if e != nil {
			return nil, ErrInvalid
		}
		bundle.Files["backup_schedule.go"] = fmt.Sprintf("// Owner integrates this function with an explicit selected provider.\npackage backupassets\n\nimport (\n \"encoding/json\"\n aws \"github.com/pulumi/pulumi-aws/sdk/v7/go/aws\"\n \"github.com/pulumi/pulumi/sdk/v3/go/pulumi\"\n \"github.com/ajent-social/amos/infra/backupschedule\"\n)\n\nfunc Register(ctx *pulumi.Context, provider *aws.Provider) error {\n var c backupschedule.ManagedAssets\n if err := json.Unmarshal([]byte(%q), &c); err != nil { return err }\n _, err := backupschedule.New(ctx, \"amos-backup\", c, %t, provider)\n return err\n}\n", string(b), c.Targets.Activated)
	}
	b, e := json.MarshalIndent(bundle, "", "  ")
	if e != nil || len(b)+1 > MaxDefinitionBytes {
		return nil, ErrInvalid
	}
	return append(b, '\n'), nil
}

// ReadBackupObservation derives freshness only from a successful fenced
// publication's snapshot clock; publication acknowledgement is not the RPO clock.
func ReadBackupObservation(ctx context.Context, r *backup.ScheduleReader) (TimeObservation, error) {
	if r == nil {
		return TimeObservation{}, ErrInvalid
	}
	s, e := r.Snapshot(ctx)
	if e != nil {
		return TimeObservation{}, e
	}
	o := TimeObservation{At: s.ObservedAt}
	if s.PublicationKnown && !s.LastSnapshotAt.IsZero() && !s.LastPublishedAt.IsZero() && !s.ObservedAt.IsZero() && !s.LastPublishedAt.Before(s.LastSnapshotAt) && !s.ObservedAt.Before(s.LastPublishedAt) {
		o.Available = true
		o.Value = s.LastSnapshotAt
	}
	return o, nil
}
