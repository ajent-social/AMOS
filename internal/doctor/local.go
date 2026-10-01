// Package doctor reports local prerequisites without changing the machine.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultProbeTimeout  = 2 * time.Second
	MaxProbeOutput       = 4096
	MinimumGoVersion     = "1.27.0"
	MinimumPodmanVersion = "5.0.0"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is safe for structured presentation: it contains no raw command
// output, configuration values, or environment variable values.
type Diagnostic struct {
	Severity          Severity `json:"severity"`
	Code              string   `json:"code"`
	Message           string   `json:"message"`
	CorrectiveCommand string   `json:"corrective_command,omitempty"`
}

type Port struct {
	Host string
	Port int
}

type Options struct {
	GoBinary      string
	PodmanBinary  string
	ProbeTimeout  time.Duration
	MinimumGo     string
	MinimumPodman string
	RequiredPorts []Port
	LocalVolumes  []string
	Config        json.RawMessage
}

// HasBlocking reports whether any prerequisite blocks local operation.
func HasBlocking(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// ExitCode is suitable for a local doctor command: blocking prerequisites
// return 1, while warnings remain visible with a successful exit status.
func ExitCode(diagnostics []Diagnostic) int {
	if HasBlocking(diagnostics) {
		return 1
	}
	return 0
}

// Check runs bounded, read-only prerequisite probes. It never installs tools,
// starts containers, binds persistent services, or emits supplied config data.
func Check(ctx context.Context, options Options) []Diagnostic {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := options.ProbeTimeout
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}
	if timeout < 10*time.Millisecond || timeout > 30*time.Second {
		return []Diagnostic{{Severity: SeverityError, Code: "doctor.invalid.timeout", Message: "The local probe timeout is outside the supported range.", CorrectiveCommand: "Use a timeout from 10ms through 30s."}}
	}
	goBin := options.GoBinary
	if goBin == "" {
		goBin = "go"
	}
	podmanBin := options.PodmanBinary
	if podmanBin == "" {
		podmanBin = "podman"
	}
	goMinimum := options.MinimumGo
	if goMinimum == "" {
		goMinimum = MinimumGoVersion
	}
	podmanMinimum := options.MinimumPodman
	if podmanMinimum == "" {
		podmanMinimum = MinimumPodmanVersion
	}
	out := make([]Diagnostic, 0, 8)
	_ = checkVersion(ctx, goBin, "version", goMinimum, "go", timeout, &out)
	if err := checkVersion(ctx, podmanBin, "--version", podmanMinimum, "podman", timeout, &out); err == nil && runtime.GOOS == "darwin" {
		checkPodmanMachine(ctx, podmanBin, timeout, &out)
	}
	for _, required := range options.RequiredPorts {
		checkPort(required, &out)
	}
	for _, volume := range options.LocalVolumes {
		checkVolume(volume, &out)
	}
	if len(options.Config) == 0 {
		out = append(out, Diagnostic{Severity: SeverityError, Code: "config.missing", Message: "The local configuration is missing or empty.", CorrectiveCommand: "Create a complete local configuration from the versioned schema."})
	} else if err := checkConfigCompleteness(options.Config, &out); err != nil {
		out = append(out, Diagnostic{Severity: SeverityError, Code: "config.invalid", Message: "The local configuration is not valid JSON.", CorrectiveCommand: "Validate the configuration against config/schema.json."})
	}
	return out
}

type version struct{ major, minor, patch int }

var versionPattern = regexp.MustCompile(`^(?:go)?(\d+)\.(\d+)(?:\.(\d+))?(?:\s|$)`)

func checkVersion(ctx context.Context, binary, arg, minimum, prefix string, timeout time.Duration, out *[]Diagnostic) error {
	min, ok := parseVersion(minimum)
	if !ok {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: prefix + ".minimum.invalid", Message: "The configured minimum tool version is invalid."})
		return errors.New("invalid minimum version")
	}
	stdout, err := runBounded(ctx, timeout, binary, arg)
	if err != nil {
		code, message, command := prefix+".unavailable", prefix+" is not available.", installCommand(prefix)
		if errors.Is(err, context.DeadlineExceeded) {
			code = prefix + ".probe.timeout"
			message = prefix + " version probe timed out."
			command = "Retry the version probe after checking local process load."
		}
		if ctx.Err() != nil {
			code = prefix + ".probe.canceled"
			message = prefix + " version probe was canceled."
			command = "Rerun the local prerequisite check."
		}
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: code, Message: message, CorrectiveCommand: command})
		return err
	}
	actual, ok := parseVersion(string(stdout))
	if !ok {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: prefix + ".version.unknown", Message: prefix + " returned an unsupported version format.", CorrectiveCommand: "Install a supported version and ensure its executable is on PATH."})
		return errors.New("unknown version")
	}
	if compareVersion(actual, min) < 0 {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: prefix + ".version.unsupported", Message: prefix + " is older than the minimum supported version.", CorrectiveCommand: upgradeCommand(prefix)})
		return errors.New("unsupported version")
	}
	return nil
}

func runBounded(parent context.Context, timeout time.Duration, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	stdout := &boundedBuffer{limit: MaxProbeOutput}
	stderr := &boundedBuffer{limit: MaxProbeOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("probe output exceeded limit")
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		remaining := b.limit - b.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		b.exceeded = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func parseVersion(raw string) (version, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "go version ")
	raw = strings.TrimPrefix(raw, "podman version ")
	m := versionPattern.FindStringSubmatch(raw)
	if len(m) != 4 {
		return version{}, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return version{}, false
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return version{}, false
	}
	patch := 0
	if m[3] != "" {
		patch, err = strconv.Atoi(m[3])
		if err != nil {
			return version{}, false
		}
	}
	return version{major: major, minor: minor, patch: patch}, true
}
func compareVersion(a, b version) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch < b.patch {
		return -1
	}
	if a.patch > b.patch {
		return 1
	}
	return 0
}

func installCommand(name string) string {
	if name == "go" {
		if runtime.GOOS == "darwin" {
			return "brew install go"
		}
		return "Install Go 1.27 or newer from https://go.dev/dl/."
	}
	if runtime.GOOS == "darwin" {
		return "brew install podman"
	}
	return "Install Podman 5.0 or newer from https://podman.io/getting-started/installation."
}
func upgradeCommand(name string) string {
	if name == "go" {
		return "Install Go 1.27 or newer from https://go.dev/dl/."
	}
	if runtime.GOOS == "darwin" {
		return "brew upgrade podman"
	}
	return "Upgrade Podman to 5.0 or newer using your system package manager."
}

func checkPodmanMachine(ctx context.Context, binary string, timeout time.Duration, out *[]Diagnostic) {
	data, err := runBounded(ctx, timeout, binary, "machine", "list", "--format=json")
	if err != nil {
		code := "podman.machine.probe.failed"
		message := "Podman machine state could not be determined."
		command := "Run podman machine list and resolve its reported error."
		if errors.Is(err, context.DeadlineExceeded) {
			code = "podman.machine.probe.timeout"
			message = "Podman machine state probe timed out."
			command = "Retry podman machine list after checking local process load."
		}
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: code, Message: message, CorrectiveCommand: command})
		return
	}
	var machines []struct {
		Running bool `json:"Running"`
		Default bool `json:"Default"`
	}
	if err := json.Unmarshal(data, &machines); err != nil {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "podman.machine.state.unknown", Message: "Podman machine state could not be parsed safely.", CorrectiveCommand: "Run podman machine list and resolve its state."})
		return
	}
	if len(machines) == 0 {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "podman.machine.not_configured", Message: "No Podman machine is configured.", CorrectiveCommand: "Run podman machine init, then podman machine start."})
		return
	}

	selected, defaults := false, 0
	for _, machine := range machines {
		if machine.Default {
			defaults++
			selected = machine.Running
		}
	}
	if defaults != 1 {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "podman.machine.state.ambiguous", Message: "Podman machine selection is ambiguous.", CorrectiveCommand: "Select exactly one default Podman machine."})
		return
	}
	if !selected {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "podman.machine.stopped", Message: "The default Podman machine is stopped.", CorrectiveCommand: "Run podman machine start."})
	}

}

func checkPort(port Port, out *[]Diagnostic) {
	if port.Port < 1 || port.Port > 65535 {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.port.invalid", Message: "A required local port is outside the valid range.", CorrectiveCommand: "Choose a TCP port from 1 through 65535."})
		return
	}
	host := port.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if host != "localhost" && net.ParseIP(host) == nil {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.port.host.invalid", Message: "A required local port host is invalid.", CorrectiveCommand: "Use localhost, 127.0.0.1, or ::1."})
		return
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port.Port)))
	if err != nil {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.port.in.use", Message: fmt.Sprintf("Required TCP port %d is already in use.", port.Port), CorrectiveCommand: portOwnerCommand(port.Port)})
		return
	}
	_ = listener.Close()
}
func portOwnerCommand(port int) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		return fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN", port)
	}
	return fmt.Sprintf("Find and stop the process listening on TCP port %d.", port)
}

func checkVolume(path string, out *[]Diagnostic) {
	if strings.TrimSpace(path) == "" {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.volume.path.missing", Message: "A required local volume path is missing.", CorrectiveCommand: "Provide the local volume directory path."})
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		code, command := "local.volume.unavailable", "Check that the local volume directory exists and is accessible."
		if errors.Is(err, os.ErrNotExist) {
			code = "local.volume.missing"
			absolute, absErr := filepath.Abs(path)
			if absErr == nil {
				command = "mkdir -p -- " + shellQuote(absolute)
			} else {
				command = "Create the configured local volume directory, then rerun the check."
			}
		}
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: code, Message: "A required local volume directory is unavailable.", CorrectiveCommand: command})
		return
	}
	if !info.IsDir() {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.volume.not_directory", Message: "A required local volume path is not a directory.", CorrectiveCommand: "Configure a dedicated local volume directory."})
		return
	}
	owner, ok := ownerUID(info)
	if !ok {
		*out = append(*out, Diagnostic{Severity: SeverityWarning, Code: "local.volume.owner.unknown", Message: "The local volume owner could not be verified on this platform.", CorrectiveCommand: "Check the directory owner with your operating system's file ownership tools."})
		return
	}
	if uid, ok := currentUID(); ok && owner != uid {
		clean, err := filepath.Abs(path)
		if err != nil {
			clean = path
		}
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "local.volume.owner.mismatch", Message: "A required local volume directory is not owned by the current user.", CorrectiveCommand: "sudo chown \"$(id -u):$(id -g)\" -- " + shellQuote(clean)})
	}
}
func ownerUID(info os.FileInfo) (uint64, bool) {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0, false
	}
	f := v.FieldByName("Uid")
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return f.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f.Int() >= 0 {
			return uint64(f.Int()), true
		}
	}
	return 0, false
}
func currentUID() (uint64, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	return uint64(os.Getuid()), true
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func checkConfigCompleteness(raw json.RawMessage, out *[]Diagnostic) error {
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("configuration must be an object")
	}
	missing := func(path string) {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "config.field.missing", Message: "A required configuration field is missing: " + path + ".", CorrectiveCommand: "Add the missing field using config/schema.json as the reference."})
	}
	invalid := func(path string) {
		*out = append(*out, Diagnostic{Severity: SeverityError, Code: "config.field.invalid", Message: "A configuration field has an unsupported shape or choice: " + path + ".", CorrectiveCommand: "Correct the field using config/schema.json as the reference."})
	}
	for _, key := range []string{"mode", "profile", "database", "providers"} {
		if _, ok := value[key]; !ok {
			missing(key)
		}
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("configuration must contain one JSON object")
	}
	allowedRoot := map[string]bool{"mode": true, "profile": true, "database": true, "providers": true, "development": true}
	for key := range value {
		if !allowedRoot[key] {
			invalid("configuration")
		}
	}
	mode, modeIsString := value["mode"].(string)
	if _, exists := value["mode"]; exists && (!modeIsString || mode != "development" && mode != "production") {
		invalid("mode")
	}
	profile, profileIsString := value["profile"].(string)
	if _, exists := value["profile"]; exists && (!profileIsString || profile != "aws_managed" && profile != "aws_vm") {
		invalid("profile")
	}
	if rawDB, ok := value["database"]; ok {
		database, ok := rawDB.(map[string]any)
		if !ok {
			invalid("database")
		} else {
			for key := range database {
				if key != "urlRef" {
					invalid("database")
				}
			}
			if _, exists := database["urlRef"]; !exists {
				missing("database.urlRef")
			} else if !completeSecretRef(database["urlRef"]) {
				invalid("database.urlRef")
			}
		}
	}
	if rawProviders, ok := value["providers"]; ok {
		providers, ok := rawProviders.(map[string]any)
		if !ok {
			invalid("providers")
		} else {
			for name := range providers {
				if name != "aws" && name != "cloudflare" && name != "email" && name != "payments" && name != "identity" {
					invalid("providers")
				}
			}
			for _, name := range []string{"aws", "cloudflare", "email", "payments", "identity"} {
				provider, exists := providers[name]
				if !exists {
					missing("providers." + name)
					continue
				}
				checkProvider(name, provider, missing, invalid)
			}
			if mode == "production" {
				for _, name := range []string{"aws", "cloudflare"} {
					if provider, ok := providers[name].(map[string]any); ok {
						if state, exists := provider["state"]; exists && state != "required" {
							invalid("providers." + name + ".state")
						}
					}
				}
			}
		}
	}
	switch mode {
	case "development":
		rawDev, ok := value["development"]
		if !ok {
			missing("development")
		} else {
			dev, ok := rawDev.(map[string]any)
			if !ok {
				invalid("development")
			} else {
				for key := range dev {
					if key != "localOnly" && key != "bindAddress" {
						invalid("development")
					}
				}
				if v, exists := dev["localOnly"]; !exists {
					missing("development.localOnly")
				} else if v != true {
					invalid("development.localOnly")
				}
				if v, exists := dev["bindAddress"]; !exists {
					missing("development.bindAddress")
				} else if addr, ok := v.(string); !ok || (addr != "localhost" && addr != "127.0.0.1" && addr != "::1") {
					invalid("development.bindAddress")
				}
			}
		}
	case "production":
		if _, exists := value["development"]; exists {
			invalid("development")
		}
	}
	return nil
}
func checkProvider(name string, raw any, missing, invalid func(string)) {
	provider, ok := raw.(map[string]any)
	if !ok {
		invalid("providers." + name)
		return
	}
	for key := range provider {
		if key != "state" && key != "credentialRef" && key != "proxyMode" {
			invalid("providers." + name)
		}
	}
	state, exists := provider["state"]
	if !exists {
		missing("providers." + name + ".state")
		return
	}
	stateValue, ok := state.(string)
	if !ok || (stateValue != "required" && stateValue != "disabled") {
		invalid("providers." + name + ".state")
		return
	}
	if ref, exists := provider["credentialRef"]; exists && !completeSecretRef(ref) {
		invalid("providers." + name + ".credentialRef")
	}
	if name == "cloudflare" {
		if proxy, exists := provider["proxyMode"]; exists {
			if mode, ok := proxy.(string); !ok || (mode != "dns_only" && mode != "proxied") {
				invalid("providers.cloudflare.proxyMode")
			}
		}
	}
	if stateValue == "required" {
		if _, exists := provider["credentialRef"]; !exists {
			missing("providers." + name + ".credentialRef")
		}
		if name == "cloudflare" {
			if _, exists := provider["proxyMode"]; !exists {
				missing("providers.cloudflare.proxyMode")
			}
		}
	}
}
func completeSecretRef(value any) bool {
	if text, ok := value.(string); ok {
		return envReference.MatchString(text) || fileReference.MatchString(text)
	}
	ref, ok := value.(map[string]any)
	if !ok {
		return false
	}
	provider, pok := ref["provider"].(string)
	name, nok := ref["name"].(string)
	if !pok || !nok || name == "" || len(name) > 256 || (provider != "aws_secrets_manager" && provider != "cloudflare_secret_store") {
		return false
	}
	for key := range ref {
		if key != "provider" && key != "name" {
			return false
		}
	}
	return true
}

var envReference = regexp.MustCompile(`^env://[A-Z][A-Z0-9_]{0,127}$`)
var fileReference = regexp.MustCompile(`^file:///[^\s]+$`)
