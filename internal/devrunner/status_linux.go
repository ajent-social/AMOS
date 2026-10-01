//go:build linux

package devrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const maxProcFileBytes = 32 << 10

type linuxProcessSnapshot struct {
	PID        int
	StartTicks uint64
	UID        uint32
	BootID     string
	Executable string
	CWD        string
}

func defaultProcessProbe(ctx context.Context, pid int, executable, ownedRoot string) (ProcessIdentity, error) {
	if ctx == nil || ctx.Err() != nil || pid < 2 {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	root, err := canonicalPath(ownedRoot)
	if err != nil {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	executable, err = canonicalExecutablePath(executable)
	if err != nil {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	before, err := readLinuxProcessSnapshot(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if before.UID != uint32(os.Getuid()) || before.Executable != executable || before.CWD != root {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	if ctx.Err() != nil {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	after, err := readLinuxProcessSnapshot(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if before != after || after.UID != uint32(os.Getuid()) {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	return ProcessIdentity{PID: pid, StartTicks: before.StartTicks, BootID: before.BootID, Executable: executable, OwnedRoot: root}, nil
}

func readLinuxProcessSnapshot(pid int) (linuxProcessSnapshot, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := readProcFile(filepath.Join(base, "stat"))
	if err != nil {
		return linuxProcessSnapshot{}, processReadError(err)
	}
	statPID, ticks, err := parseLinuxProcStat(stat)
	if err != nil || statPID != pid {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	status, err := readProcFile(filepath.Join(base, "status"))
	if err != nil {
		return linuxProcessSnapshot{}, processReadError(err)
	}
	uid, err := parseLinuxProcUID(status)
	if err != nil {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	bootRaw, err := readProcFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	bootID, err := parseLinuxBootID(bootRaw)
	if err != nil {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return linuxProcessSnapshot{}, processReadError(err)
	}
	cwd, err := os.Readlink(filepath.Join(base, "cwd"))
	if err != nil {
		return linuxProcessSnapshot{}, processReadError(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return linuxProcessSnapshot{}, errProcessProbeUnavailable
	}
	return linuxProcessSnapshot{PID: statPID, StartTicks: ticks, UID: uid, BootID: bootID, Executable: filepath.Clean(executable), CWD: filepath.Clean(cwd)}, nil
}

func parseLinuxBootID(raw []byte) (string, error) {
	bootID := strings.ToLower(strings.TrimSpace(string(raw)))
	if uuid.Validate(bootID) != nil {
		return "", fmt.Errorf("invalid proc boot ID")
	}
	return bootID, nil
}

func readProcFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, readErr := io.ReadAll(io.LimitReader(file, maxProcFileBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if len(data) > maxProcFileBytes {
		return nil, errProcessProbeUnavailable
	}
	return data, nil
}

func processReadError(err error) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrProcessDone) {
		return errProcessNotFound
	}
	return errProcessProbeUnavailable
}

func parseLinuxProcStat(raw []byte) (int, uint64, error) {
	line := strings.TrimSpace(string(raw))
	close := strings.LastIndexByte(line, ')')
	open := strings.IndexByte(line, '(')
	if open <= 0 || close <= open || close+1 >= len(line) {
		return 0, 0, fmt.Errorf("malformed proc stat")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:open]))
	if err != nil || pid < 2 {
		return 0, 0, fmt.Errorf("malformed proc pid")
	}
	fields := strings.Fields(line[close+1:])
	if len(fields) <= 19 || len(fields[0]) != 1 {
		return 0, 0, fmt.Errorf("truncated proc stat")
	}
	switch fields[0] {
	case "R", "S", "D", "Z", "T", "t", "X", "x", "K", "W", "P", "I":
	default:
		return 0, 0, fmt.Errorf("invalid proc state")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || ticks == 0 {
		return 0, 0, fmt.Errorf("invalid proc start ticks")
	}
	return pid, ticks, nil
}

func parseLinuxProcUID(raw []byte) (uint32, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) != 4 {
			return 0, fmt.Errorf("malformed proc uid")
		}
		uid, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid proc uid")
		}
		return uint32(uid), nil
	}
	return 0, fmt.Errorf("missing proc uid")
}
