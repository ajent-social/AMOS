package doctor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const completeDevelopmentConfig = `{"mode":"development","profile":"aws_managed","database":{"urlRef":"env://AMOS_DATABASE_URL"},"providers":{"aws":{"state":"disabled"},"cloudflare":{"state":"disabled"},"email":{"state":"disabled"},"payments":{"state":"disabled"},"identity":{"state":"disabled"}},"development":{"localOnly":true,"bindAddress":"127.0.0.1"}}`

func TestLocalUnsupportedGoVersionAndStoppedPodmanMachine(t *testing.T) {
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.26.0 darwin/arm64\\n'\n")
	podman := writeCommand(t, "#!/bin/sh\ncase \"$*\" in\n  --version) printf 'podman version 5.8.0\\n' ;;\n  'machine list --format=json') printf '[{\"Running\":false,\"Default\":true}]' ;;\nesac\n")
	diagnostics := Check(context.Background(), baseOptions(goBin, podman))
	if !hasCode(diagnostics, "go.version.unsupported") {
		t.Fatalf("unsupported Go was not reported: %+v", diagnostics)
	}
	if runtime.GOOS == "darwin" && !hasCode(diagnostics, "podman.machine.stopped") {
		t.Fatalf("stopped Podman machine was not reported: %+v", diagnostics)
	}
	var machineDiagnostics []Diagnostic
	checkPodmanMachine(context.Background(), podman, DefaultProbeTimeout, &machineDiagnostics)
	if !hasCode(machineDiagnostics, "podman.machine.stopped") {
		t.Fatalf("stopped Podman machine state was not identified: %+v", machineDiagnostics)
	}
	if ExitCode(diagnostics) != 1 {
		t.Fatalf("ExitCode(%+v) = %d, want blocking exit", diagnostics, ExitCode(diagnostics))
	}
	oldPodman := writeCommand(t, "#!/bin/sh\nprintf 'podman version 4.9.0\\n'\n")
	diagnostics = Check(context.Background(), baseOptions(goBin, oldPodman))
	if !hasCode(diagnostics, "podman.version.unsupported") {
		t.Fatalf("unsupported Podman version was not reported: %+v", diagnostics)
	}
}

func TestLocalUnavailableBinaryAndProbeTimeout(t *testing.T) {
	podman := writeCommand(t, "#!/bin/sh\nprintf 'podman version 5.8.0\\n'\n")
	diagnostics := Check(context.Background(), baseOptions(filepath.Join(t.TempDir(), "missing-go"), podman))
	if !hasCode(diagnostics, "go.unavailable") {
		t.Fatalf("missing Go executable was not reported: %+v", diagnostics)
	}

	hangingGo := writeCommand(t, "#!/bin/sh\nexec sleep 5\n")
	options := baseOptions(hangingGo, podman)
	options.ProbeTimeout = 30 * time.Millisecond
	diagnostics = Check(context.Background(), options)
	if !hasCode(diagnostics, "go.probe.timeout") {
		t.Fatalf("bounded probe did not report timeout: %+v", diagnostics)
	}
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.27.1 darwin/arm64\\n'\n")
	hangingPodman := writeCommand(t, "#!/bin/sh\nexec sleep 5\n")
	options = baseOptions(goBin, hangingPodman)
	options.ProbeTimeout = 30 * time.Millisecond
	diagnostics = Check(context.Background(), options)
	if !hasCode(diagnostics, "podman.probe.timeout") {
		t.Fatalf("hanging Podman version probe did not identify timeout: %+v", diagnostics)
	}
}

func TestLocalPodmanProbeTimeoutIdentifiesPrerequisite(t *testing.T) {
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.27.1 darwin/arm64\\n'\n")
	hangingPodman := writeCommand(t, "#!/bin/sh\nexec sleep 5\n")
	options := baseOptions(goBin, hangingPodman)
	options.ProbeTimeout = 30 * time.Millisecond
	diagnostics := Check(context.Background(), options)
	if !hasCode(diagnostics, "podman.probe.timeout") {
		t.Fatalf("hanging Podman version probe did not identify timeout: %+v", diagnostics)
	}
}

func TestLocalOccupiedPortIsBlocking(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close occupied-port fixture: %v", err)
		}
	}()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.27.1 darwin/arm64\\n'\n")
	podman := writeCommand(t, "#!/bin/sh\ncase \"$*\" in\n  --version) printf 'podman version 5.8.0\\n' ;;\n  'machine list --format=json') printf '[{\"Running\":true,\"Default\":true}]' ;;\nesac\n")
	options := baseOptions(goBin, podman)
	options.RequiredPorts = []Port{{Host: "127.0.0.1", Port: port}}
	diagnostics := Check(context.Background(), options)
	if !hasCode(diagnostics, "local.port.in.use") {
		t.Fatalf("occupied port was not reported: %+v", diagnostics)
	}
}

func TestLocalConfigCompletenessAndDiagnosticRedaction(t *testing.T) {
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.27.1 darwin/arm64\\n'\n")
	podman := writeCommand(t, "#!/bin/sh\ncase \"$*\" in\n  --version) printf 'podman version 5.8.0\\n' ;;\n  'machine list --format=json') printf '[{\"Running\":true,\"Default\":true}]' ;;\nesac\n")
	options := baseOptions(goBin, podman)
	options.Config = json.RawMessage(`{"mode":"development","profile":"aws_managed","database":{"password":"NEVER-ECHO-THIS"},"providers":{}}`)
	diagnostics := Check(context.Background(), options)
	if !hasCode(diagnostics, "config.field.missing") {
		t.Fatalf("incomplete config not reported: %+v", diagnostics)
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "NEVER-ECHO-THIS") {
		t.Fatalf("diagnostics exposed a config value: %s", encoded)
	}

	options.Config = json.RawMessage(completeDevelopmentConfig)
	diagnostics = Check(context.Background(), options)
	if hasCode(diagnostics, "config.field.missing") || hasCode(diagnostics, "config.invalid") || hasCode(diagnostics, "config.field.invalid") {
		t.Fatalf("complete local config was rejected: %+v", diagnostics)
	}
}

func TestLocalVolumeOwnershipAndWarningExit(t *testing.T) {
	goBin := writeCommand(t, "#!/bin/sh\nprintf 'go version go1.27.1 darwin/arm64\\n'\n")
	podman := writeCommand(t, "#!/bin/sh\nprintf 'podman version 5.8.0\\n'\n")
	options := baseOptions(goBin, podman)
	options.LocalVolumes = []string{t.TempDir()}
	diagnostics := Check(context.Background(), options)
	if hasCode(diagnostics, "local.volume.owner.mismatch") {
		t.Fatalf("current-user temporary directory reported as mismatched: %+v", diagnostics)
	}
	if ExitCode([]Diagnostic{{Severity: SeverityWarning, Code: "example.warning", Message: "warning"}}) != 0 {
		t.Fatal("warning should not produce blocking exit")
	}
}

func TestLocalVersionParserRejectsPrereleaseAndUnrelatedOutput(t *testing.T) {
	for _, raw := range []string{"go version go1.27rc1 darwin/arm64", "podman version 5.0.0-rc1", "error: expected version 5.0.0"} {
		if _, ok := parseVersion(raw); ok {
			t.Fatalf("parseVersion(%q) accepted unsupported output", raw)
		}
	}
	for _, raw := range []string{"go version go1.27.1 darwin/arm64", "podman version 5.8.0", "1.27"} {
		if _, ok := parseVersion(raw); !ok {
			t.Fatalf("parseVersion(%q) rejected valid version", raw)
		}
	}
}

func baseOptions(goBin, podman string) Options {
	return Options{GoBinary: goBin, PodmanBinary: podman, Config: json.RawMessage(completeDevelopmentConfig)}
}
func writeCommand(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe")
	if err := os.WriteFile(path, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
