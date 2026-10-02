package devrunner

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"time"
)

// CleanupResult records the advisory plan and the actions that completed.
// A failed action stops execution; Completed therefore remains an accurate
// prefix if the caller needs to report partial progress.
type CleanupResult struct {
	Plan      CleanupPlan     `json:"plan"`
	Completed []CleanupAction `json:"completed"`
}

// ExecuteCleanup runs the verified local cleanup plan. The database volume is
// retained unless deleteData is explicitly true. Every destructive resource
// action rechecks the persisted project identity and the live resource labels
// immediately before operating on the immutable container or network ID.
func ExecuteCleanup(ctx context.Context, options StatusOptions, deleteData bool) (CleanupResult, error) {
	return executeCleanup(ctx, options, deleteData, defaultCleanupCommand, defaultCleanupSignal, defaultProcessProbe)
}

func executeCleanup(ctx context.Context, options StatusOptions, deleteData bool, command cleanupCommand, signal cleanupSignal, probe cleanupProbe) (CleanupResult, error) {
	plan, err := PlanCleanup(ctx, options, deleteData)
	result := CleanupResult{Plan: plan, Completed: []CleanupAction{}}
	if err != nil {
		return result, err
	}
	if plan.Identity.Project == "" || len(plan.Warnings) > 0 {
		return result, ErrProjectStateUnavailable
	}
	for _, action := range plan.Actions {
		if err := executeCleanupAction(ctx, options, plan.Identity, action, deleteData, command, signal, probe); err != nil {
			return result, err
		}
		result.Completed = append(result.Completed, action)
	}
	return result, nil
}

type cleanupCommand func(context.Context, string, ...string) ([]byte, error)
type cleanupSignal func(int) error
type cleanupProbe func(context.Context, int, string, string) (ProcessIdentity, error)

func executeCleanupAction(ctx context.Context, options StatusOptions, expected ProjectIdentity, action CleanupAction, deleteData bool, command cleanupCommand, signal cleanupSignal, probe cleanupProbe) error {
	if ctx == nil || ctx.Err() != nil || command == nil || signal == nil || probe == nil {
		return ErrProjectStateUnavailable
	}
	state, err := loadCleanupState(options)
	if err != nil || state.Identity.ID != expected.ID || state.Identity.Project != expected.Project {
		return ErrProjectIdentityMismatch
	}
	switch action.Operation {
	case "retain-volume":
		if action.Kind != ResourceDatabaseVolume || deleteData || action.Name != state.Resources[ResourceDatabaseVolume].Name {
			return ErrProjectIdentityMismatch
		}
		return nil
	case "stop-process":
		return stopOwnedProcess(ctx, options, state, action, signal, probe)
	case "remove-container":
		if action.Kind != ResourceDatabase || !podmanIDRE.MatchString(action.ID) {
			return ErrResourceConflict
		}
		labels, err := ResourceLabels(state.Identity, ResourceDatabase)
		if err != nil {
			return ErrProjectStateUnavailable
		}
		output, err := command(ctx, options.PodmanBinary, "inspect", "--format", "{{json .Config.Labels}}\t{{.State.Status}}\t{{.Name}}\t{{.Id}}", action.ID)
		if err != nil {
			return ErrResourceConflict
		}
		parts := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(parts) != 4 {
			return ErrResourceConflict
		}
		observed, decodeErr := decodeResourceLabels(parts[0])
		name := strings.TrimPrefix(parts[2], "/")
		id := strings.ToLower(parts[3])
		if decodeErr != nil || !exactLabels(labels, observed) || !validDatabaseContainerName(state.Identity.Project, name) || name != action.Name || id != strings.ToLower(action.ID) || !podmanIDRE.MatchString(id) || (containerState(parts[1]) != ResourceRunning && containerState(parts[1]) != ResourceStopped) {
			return ErrResourceConflict
		}
		if _, err := command(ctx, options.PodmanBinary, "rm", "--force", id); err != nil {
			return ErrResourceConflict
		}
		return nil
	case "remove-network":
		if action.Kind != ResourceDatabaseNetwork || action.Name != state.Resources[ResourceDatabaseNetwork].Name {
			return ErrResourceConflict
		}
		labels, err := ResourceLabels(state.Identity, ResourceDatabaseNetwork)
		if err != nil {
			return ErrProjectStateUnavailable
		}
		output, err := command(ctx, options.PodmanBinary, "network", "inspect", "--format", "{{json .Labels}}\t{{.ID}}", action.Name)
		if err != nil {
			return ErrResourceConflict
		}
		parts := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(parts) != 2 {
			return ErrResourceConflict
		}
		observed, decodeErr := decodeResourceLabels(parts[0])
		id := strings.ToLower(parts[1])
		if decodeErr != nil || !exactLabels(labels, observed) || !fullResourceIDRE.MatchString(id) {
			return ErrResourceConflict
		}
		if _, err := command(ctx, options.PodmanBinary, "network", "rm", id); err != nil {
			return ErrResourceConflict
		}
		return nil
	case "remove-volume":
		if !deleteData || action.Kind != ResourceDatabaseVolume || action.Name != state.Resources[ResourceDatabaseVolume].Name {
			return ErrProjectIdentityMismatch
		}
		labels, err := ResourceLabels(state.Identity, ResourceDatabaseVolume)
		if err != nil {
			return ErrProjectStateUnavailable
		}
		output, err := command(ctx, options.PodmanBinary, "volume", "inspect", "--format", "{{json .Labels}}", action.Name)
		if err != nil {
			return ErrResourceConflict
		}
		observed, decodeErr := decodeResourceLabels(string(output))
		if decodeErr != nil || !exactLabels(labels, observed) {
			return ErrResourceConflict
		}
		if _, err := command(ctx, options.PodmanBinary, "volume", "rm", action.Name); err != nil {
			return ErrResourceConflict
		}
		return nil
	default:
		return ErrResourceConflict
	}
}

func stopOwnedProcess(ctx context.Context, options StatusOptions, state privateProjectState, action CleanupAction, signal cleanupSignal, probe cleanupProbe) error {
	if action.Kind != "application" || action.Process == nil || state.Process == nil || !sameProcessIdentity(state.Process.Identity, *action.Process) {
		return ErrProcessIdentityMismatch
	}
	expected := *action.Process
	actual, err := probe(ctx, expected.PID, expected.Executable, expected.OwnedRoot)
	if err != nil || !sameProcessIdentity(expected, actual) {
		return ErrProcessIdentityMismatch
	}
	if err := signal(expected.PID); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return ErrProcessIdentityMismatch
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		actual, err = probe(ctx, expected.PID, expected.Executable, expected.OwnedRoot)
		if errors.Is(err, errProcessNotFound) || (err == nil && !sameProcessIdentity(expected, actual)) {
			return ClearOwnedProcess(ctx, options.WorkingDir, options.Project, expected)
		}
		if errors.Is(err, errProcessProbeUnavailable) {
			if !cleanupProcessExists(expected.PID) {
				return ClearOwnedProcess(ctx, options.WorkingDir, options.Project, expected)
			}
		} else if err != nil {
			return ErrProcessIdentityMismatch
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("owned local application did not stop within the cleanup window")
		case <-ticker.C:
		}
	}
}

func loadCleanupState(options StatusOptions) (privateProjectState, error) {
	store, err := newPrivateStateStore(options.WorkingDir, options.Project, false)
	if err != nil {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	state, err := store.load()
	if err != nil {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	return state, nil
}

func defaultCleanupCommand(ctx context.Context, binary string, args ...string) ([]byte, error) {
	if binary == "" {
		binary = "podman"
	}
	return runPodmanStatus(ctx, binary, args...)
}

func defaultCleanupSignal(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return ErrProcessIdentityMismatch
	}
	return process.Signal(syscall.SIGTERM)
}

func cleanupProcessExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
