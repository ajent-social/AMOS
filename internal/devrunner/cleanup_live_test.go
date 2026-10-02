package devrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Required-service checks create uniquely identified resources in this session.
// Teardown targets only the IDs and names returned by these successful creates.
func TestExecuteCleanupPodmanOwnershipAndRetainedData(t *testing.T) {
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatal("Podman is required for real cleanup qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	authDir := t.TempDir()
	authFile := filepath.Join(authDir, "config.json")
	if err := os.WriteFile(authFile, []byte(`{"auths":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) string {
		t.Helper()
		if args[0] == "create" || args[0] == "run" {
			args = append([]string{args[0], "--authfile", authFile}, args[1:]...)
		}
		child := exec.CommandContext(ctx, podman, args...)
		child.Env = append(os.Environ(), "DOCKER_CONFIG="+authDir, "REGISTRY_AUTH_FILE="+authFile)
		out, err := child.CombinedOutput()
		if err != nil {
			_ = out
			t.Fatal("required owned Podman operation failed")
		}
		return strings.TrimSpace(string(out))
	}
	command("info", "--format", "{{.Host.Arch}}")
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	project := "cleanup-live-" + suffix
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, project)
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupState(StatusOptions{Project: project, WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	labels := func(kind ResourceKind) []string {
		values, err := ResourceLabels(identity, kind)
		if err != nil {
			t.Fatal(err)
		}
		var args []string
		for key, value := range values {
			args = append(args, "--label", key+"="+value)
		}
		return args
	}
	networkName := state.Resources[ResourceDatabaseNetwork].Name
	args := append([]string{"network", "create"}, labels(ResourceDatabaseNetwork)...)
	args = append(args, networkName)
	networkID := command(args...)
	t.Cleanup(func() { _ = exec.Command(podman, "network", "rm", networkID).Run() })
	volumeName := state.Resources[ResourceDatabaseVolume].Name
	args = append([]string{"volume", "create"}, labels(ResourceDatabaseVolume)...)
	args = append(args, volumeName)
	createdVolume := command(args...)
	if createdVolume != volumeName {
		t.Fatal("created volume identity differs")
	}
	t.Cleanup(func() { _ = exec.Command(podman, "volume", "rm", createdVolume).Run() })
	containerName := project + "-db-" + suffix
	args = []string{"create", "--pull=never", "--name", containerName, "--network", networkID, "--volume", volumeName + ":/owned"}
	args = append(args, labels(ResourceDatabase)...)
	args = append(args, "docker.io/library/alpine:latest", "sh", "-c", "sleep 60")
	containerID := command(args...)
	t.Cleanup(func() { _ = exec.Command(podman, "rm", "--force", containerID).Run() })
	command("run", "--rm", "--pull=never", "--volume", volumeName+":/owned", "docker.io/library/alpine:latest", "sh", "-c", "printf preserved > /owned/marker")
	options := StatusOptions{Project: project, WorkingDir: root, PodmanBinary: podman}
	result, err := ExecuteCleanup(ctx, options, false)
	if err != nil {
		t.Fatal("real owned cleanup failed")
	}
	if len(result.Completed) != 3 || result.Completed[2].Operation != "retain-volume" {
		t.Fatal("cleanup did not preserve data by default")
	}
	if exec.Command(podman, "container", "exists", containerID).Run() == nil {
		t.Fatal("owned container remains after cleanup")
	}
	if exec.Command(podman, "network", "exists", networkID).Run() == nil {
		t.Fatal("owned network remains after cleanup")
	}
	if got := command("run", "--rm", "--pull=never", "--volume", volumeName+":/owned:ro", "docker.io/library/alpine:latest", "cat", "/owned/marker"); got != "preserved" {
		t.Fatal("retained data changed")
	}
	// A foreign identity under the same project slug must block cleanup and remain.
	foreignLabels, _ := ResourceLabels(identity, ResourceDatabase)
	foreignLabels[projectIDLabel] = uuid.NewString()
	args = []string{"create", "--pull=never", "--name", project + "-db-112233445566"}
	for key, value := range foreignLabels {
		args = append(args, "--label", key+"="+value)
	}
	args = append(args, "docker.io/library/alpine:latest", "true")
	foreignID := command(args...)
	t.Cleanup(func() { _ = exec.Command(podman, "rm", "--force", foreignID).Run() })
	if _, err := ExecuteCleanup(ctx, options, false); err == nil {
		t.Fatal("foreign project identity was accepted")
	}
	if exec.Command(podman, "container", "exists", foreignID).Run() != nil {
		t.Fatal("foreign identity container was removed")
	}
	// This test created that exact foreign fixture and owns its teardown.
	command("rm", "--force", foreignID)
	result, err = ExecuteCleanup(ctx, options, true)
	if err != nil {
		t.Fatal("explicit owned data cleanup failed")
	}
	if len(result.Completed) != 1 || result.Completed[0].Operation != "remove-volume" {
		t.Fatal("explicit data cleanup did not target the retained volume")
	}
	if exec.Command(podman, "volume", "exists", volumeName).Run() == nil {
		t.Fatal("owned volume remains after explicit data cleanup")
	}
}
