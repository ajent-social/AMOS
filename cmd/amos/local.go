package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ajent-social/amos/internal/devrunner"
	"os"
	"os/signal"
	"syscall"
)

func runLocal(command string, args []string) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	project := flags.String("project", "", "generated project slug")
	dir := flags.String("dir", "", "generated application directory")
	podman := flags.String("podman", "podman", "Podman executable")
	var binary, ready *string
	var plan, execute, deleteData *bool
	if command == "dev" {
		binary = flags.String("binary", ".amos/bin/app", "native application executable")
		ready = flags.String("ready", "http://127.0.0.1:8080/readyz", "application readiness URL")
	}
	if command == "clean" {
		plan = flags.Bool("plan", false, "show the verified cleanup plan")
		execute = flags.Bool("execute", false, "execute cleanup of exactly owned resources")
		deleteData = flags.Bool("delete-data", false, "explicitly include owned data volume deletion")
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 || *project == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "amos: explicit project and directory are required")
		return 2
	}
	if command == "clean" && *plan == *execute {
		fmt.Fprintln(os.Stderr, "amos clean: select exactly one of --plan or --execute")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch command {
	case "dev":
		if devrunner.Run(ctx, devrunner.Options{Project: *project, WorkingDir: *dir, AppBinary: *binary, ReadinessURL: *ready, PodmanBinary: *podman, Output: os.Stdout}) != nil {
			fmt.Fprintln(os.Stderr, "amos dev: local application lifecycle failed")
			return 1
		}
		return 0
	case "status":
		result, err := devrunner.InspectProject(ctx, devrunner.StatusOptions{Project: *project, WorkingDir: *dir, PodmanBinary: *podman})
		if err != nil {
			fmt.Fprintln(os.Stderr, "amos status: private project state unavailable")
			return 1
		}
		if json.NewEncoder(os.Stdout).Encode(result) != nil {
			fmt.Fprintln(os.Stderr, "amos status: output unavailable")
			return 1
		}
		return 0
	case "clean":
		if *execute {
			result, err := devrunner.ExecuteCleanup(ctx, devrunner.StatusOptions{Project: *project, WorkingDir: *dir, PodmanBinary: *podman}, *deleteData)
			if json.NewEncoder(os.Stdout).Encode(result) != nil {
				fmt.Fprintln(os.Stderr, "amos clean: output unavailable")
				return 1
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "amos clean: exact ownership verification or cleanup failed")
				return 1
			}
			return 0
		}
		result, err := devrunner.PlanCleanup(ctx, devrunner.StatusOptions{Project: *project, WorkingDir: *dir, PodmanBinary: *podman}, *deleteData)
		if err != nil {
			fmt.Fprintln(os.Stderr, "amos clean: private project state unavailable")
			return 1
		}
		fmt.Fprintln(os.Stderr, "amos clean: plan only; use --execute to apply it")
		if json.NewEncoder(os.Stdout).Encode(result) != nil {
			fmt.Fprintln(os.Stderr, "amos clean: output unavailable")
			return 1
		}
		return 0
	}
	return 2
}
