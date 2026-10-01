//go:build darwin

package devrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func defaultProcessProbe(ctx context.Context, pid int, executable, ownedRoot string) (ProcessIdentity, error) {
	if ctx == nil || ctx.Err() != nil || pid < 2 {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	before, beforeUID, err := darwinProcessStart(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if beforeUID != uint32(os.Getuid()) {
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
	observedRoot, textPaths, err := probeProcessPaths(ctx, pid)
	if err != nil || observedRoot != root {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	after, afterUID, err := darwinProcessStart(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if !sameDarwinProcessProbe(before, beforeUID, after, afterUID, uint32(os.Getuid())) {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	foundExecutable := false
	for _, path := range textPaths {
		if resolved, resolveErr := canonicalExecutablePath(path); resolveErr == nil && resolved == executable {
			foundExecutable = true
			break
		}
	}
	if !foundExecutable {
		return ProcessIdentity{}, errProcessProbeUnavailable
	}
	return ProcessIdentity{PID: pid, StartSeconds: before.StartSeconds, StartMicroseconds: before.StartMicroseconds, Executable: executable, OwnedRoot: root}, nil
}

func darwinProcessStart(pid int) (processStartIdentity, uint32, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EINVAL) {
			return processStartIdentity{}, 0, errProcessNotFound
		}
		return processStartIdentity{}, 0, errProcessProbeUnavailable
	}
	start := proc.Proc.P_starttime
	if int(proc.Proc.P_pid) != pid || start.Sec <= 0 || start.Usec < 0 || start.Usec >= 1_000_000 {
		return processStartIdentity{}, 0, errProcessProbeUnavailable
	}
	return processStartIdentity{PID: pid, StartSeconds: start.Sec, StartMicroseconds: int64(start.Usec)}, proc.Eproc.Ucred.Uid, nil
}

func probeProcessPaths(ctx context.Context, pid int) (string, []string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-d", "cwd,txt", "-Ffn")
	var output boundedProbeBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || probeCtx.Err() != nil {
		return "", nil, errProcessProbeUnavailable
	}
	field := ""
	cwd := ""
	var textPaths []string
	for _, line := range strings.Split(output.String(), "\n") {
		if line == "fcwd" {
			field = "cwd"
			continue
		}
		if line == "ftxt" {
			field = "txt"
			continue
		}
		if !strings.HasPrefix(line, "n") || len(line) < 2 {
			continue
		}
		path := strings.TrimPrefix(line, "n")
		if strings.Contains(path, "\x00") {
			return "", nil, errProcessProbeUnavailable
		}
		switch field {
		case "cwd":
			cwd = path
		case "txt":
			textPaths = append(textPaths, path)
		}
	}
	if cwd == "" || len(textPaths) == 0 {
		return "", nil, errProcessProbeUnavailable
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", nil, errProcessProbeUnavailable
	}
	return filepath.Clean(resolvedCWD), textPaths, nil
}
