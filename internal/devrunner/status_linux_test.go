//go:build linux

package devrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/uuid"
)

func TestLinuxProcessProbeUsesStableBootAndStartIdentity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = canonicalExecutablePath(executable)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := defaultProcessProbe(context.Background(), os.Getpid(), executable, root)
	if err != nil {
		t.Fatalf("probe current process: %v", err)
	}
	if identity.PID != os.Getpid() || identity.StartTicks == 0 || uuid.Validate(identity.BootID) != nil || identity.StartSeconds != 0 || identity.StartMicroseconds != 0 || !sameProcessIdentity(identity, identity) {
		t.Fatalf("Linux identity lacks exact boot/start token: %+v", identity)
	}
	for name, changed := range map[string]ProcessIdentity{
		"reused PID":     func() ProcessIdentity { c := identity; c.StartTicks++; return c }(),
		"different boot": func() ProcessIdentity { c := identity; c.BootID = uuid.NewString(); return c }(),
	} {
		t.Run(name, func(t *testing.T) {
			if sameProcessIdentity(identity, changed) {
				t.Fatalf("changed Linux start identity matched original: %+v", changed)
			}
		})
	}
	if _, err := defaultProcessProbe(context.Background(), os.Getpid(), executable, filepath.Join(root, "not-this-process-root")); err == nil {
		t.Fatal("process was accepted for a different project root")
	}
}

func TestLinuxProcParsersRejectMalformedIdentity(t *testing.T) {
	validStat := func(pid int, ticks string) []byte {
		return []byte(fmt.Sprintf("%d (test process) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 %s\n", pid, ticks))
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "missing command delimiter", data: []byte("123 test S")},
		{name: "bad PID", data: validStat(1, "99")},
		{name: "truncated fields", data: []byte("123 (test) S 1 2")},
		{name: "bad start ticks", data: validStat(123, "x")},
		{name: "zero start ticks", data: validStat(123, "0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseLinuxProcStat(tc.data); err == nil {
				t.Fatalf("accepted malformed stat %q", tc.data)
			}
		})
	}
	if pid, ticks, err := parseLinuxProcStat(validStat(123, "456")); err != nil || pid != 123 || ticks != 456 {
		t.Fatalf("valid stat parse = %d %d %v", pid, ticks, err)
	}
	for _, input := range []string{"", "Name:\ttest\n", "Uid:\t1 2 3\n", "Uid:\tx 1 1 1\n"} {
		if _, err := parseLinuxProcUID([]byte(input)); err == nil {
			t.Errorf("accepted malformed uid status %q", input)
		}
	}
	if uid, err := parseLinuxProcUID([]byte("Name:\ttest\nUid:\t1000 1000 1000 1000\n")); err != nil || uid != 1000 {
		t.Fatalf("valid uid parse = %d %v", uid, err)
	}
	for _, input := range [][]byte{nil, []byte("not-a-boot-id"), []byte("00000000-0000-0000-0000-000000000000 extra")} {
		if _, err := parseLinuxBootID(input); err == nil {
			t.Errorf("accepted malformed boot ID %q", input)
		}
	}
	if bootID, err := parseLinuxBootID([]byte("550E8400-E29B-41D4-A716-446655440000\n")); err != nil || bootID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("valid boot ID parse = %q %v", bootID, err)
	}
}

func TestLinuxProbeRejectsForeignUIDProcess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to start a test-owned process under a different UID")
	}
	command := exec.Command("/bin/sleep", "30")
	config := &syscall.Credential{Uid: 65534, Gid: 65534}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: config}
	if err := command.Start(); err != nil {
		t.Skipf("cannot start foreign-UID test process: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	if _, err := defaultProcessProbe(context.Background(), command.Process.Pid, "/bin/sleep", "/tmp"); err == nil {
		t.Fatal("process owned by another UID was accepted")
	}
}

func TestLinuxPrivateStateRejectsForeignOwnedRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create a foreign-owned test directory")
	}
	project := t.TempDir()
	if _, err := InitializeProject(context.Background(), project, "foreign-owner-fixture"); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(project, ".amos", "devrunner")
	if err := os.Chown(stateRoot, 65534, 65534); err != nil {
		t.Skipf("cannot change ownership of isolated test directory: %v", err)
	}
	store, err := newPrivateStateStore(project, "foreign-owner-fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatalf("foreign-owned private state root was accepted: %v", err)
	}
}
