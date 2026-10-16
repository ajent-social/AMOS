package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/ajent-social/amos/internal/scaffold/ops"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var errBackupCLI = errors.New("backup operation unavailable")

func runBackup(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runBackupIO(ctx, args, os.Stdout, os.Stderr)
}
func runBackupIO(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	fail := func() int {
		_, _ = fmt.Fprintln(diagnostics, "amos backup: invalid input or operation unavailable")
		return 2
	}
	if ctx == nil || len(args) == 0 {
		return fail()
	}
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "")
	assetsPath := flags.String("assets-config", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *path == "" {
		return fail()
	}
	if args[0] != "run" && args[0] != "status" && args[0] != "check" && args[0] != "render" {
		return fail()
	}
	if (args[0] == "render") != (*assetsPath != "") {
		return fail()
	}
	body, err := readBackupConfiguration(*path)
	if err != nil {
		return fail()
	}
	config, err := backup.DecodeScheduleConfig(bytes.NewReader(body))
	if err != nil {
		return fail()
	}
	var output []byte
	var unsuccessful bool
	switch args[0] {
	case "render":
		body, err = readBackupConfiguration(*assetsPath)
		if err != nil {
			return fail()
		}
		assets, err := ops.DecodeBackupAssets(bytes.NewReader(body))
		if err != nil {
			return fail()
		}
		output, err = ops.RenderBackupAssets(config, assets)
		if err != nil || len(output) > 64<<10 {
			return fail()
		}
	case "run":
		if !config.Targets.Activated {
			return fail()
		}
		runContext, cancel := context.WithTimeout(ctx, config.RunTimeout+30*time.Second)
		defer cancel()
		// Resolve only the two write-side references. A read secret is never needed.
		ledgerURL, err := resolveBackupReference(config.Database.LedgerWriteRef)
		if err != nil {
			return fail()
		}
		dumpURL, err := resolveBackupReference(config.Database.DumpRef)
		if err != nil {
			return fail()
		}
		ledgerURL, err = backup.NormalizeScheduleDatabaseURL(config.Database, backup.ScheduleLedgerWrite, ledgerURL)
		if err != nil {
			return fail()
		}
		dumpURL, err = backup.NormalizeScheduleDatabaseURL(config.Database, backup.ScheduleDump, dumpURL)
		if err != nil {
			return fail()
		}
		db, err := openBackupDatabase(runContext, ledgerURL, config.Database.LedgerWriteRole, config.Database.LedgerDatabase)
		if err != nil {
			return fail()
		}
		defer func() { _ = db.Close() }()
		store, err := backup.NewRuntimeStore(runContext, config.AWS, config.Create.MaxBytes, config.RunTimeout+15*time.Second)
		if err != nil {
			return fail()
		}
		scheduler, err := backup.NewScheduler(db, config, store, dumpURL)
		if err != nil {
			return fail()
		}
		result, err := scheduler.RunDue(runContext)
		if err != nil {
			return fail()
		}
		switch result {
		case backup.ScheduleNotDue, backup.ScheduleBusy, backup.ScheduleCompleted:
		case backup.ScheduleFailed, backup.ScheduleUnknown, backup.ScheduleCapacity:
			unsuccessful = true
		default:
			return fail()
		}
		output, err = json.Marshal(struct {
			Version string                `json:"version"`
			Result  backup.ScheduleResult `json:"result"`
		}{Version: "amos-backup-run-v1", Result: result})
		if err != nil {
			return fail()
		}
	case "status", "check":
		readContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		ledgerURL, err := resolveBackupReference(config.Database.LedgerReadRef)
		if err != nil {
			return fail()
		}
		ledgerURL, err = backup.NormalizeScheduleDatabaseURL(config.Database, backup.ScheduleLedgerRead, ledgerURL)
		if err != nil {
			return fail()
		}
		db, err := openBackupDatabase(readContext, ledgerURL, config.Database.LedgerReadRole, config.Database.LedgerDatabase)
		if err != nil {
			return fail()
		}
		defer func() { _ = db.Close() }()
		reader, err := backup.NewScheduleReader(db, config)
		if err != nil {
			return fail()
		}
		if args[0] == "status" {
			snapshot, err := reader.Snapshot(readContext)
			if err != nil {
				return fail()
			}
			output, err = json.Marshal(snapshot)
			if err != nil {
				return fail()
			}
		} else {
			observation, err := ops.ReadBackupObservation(readContext, reader)
			if err != nil {
				return fail()
			}
			state := "unavailable"
			if observation.Available && !observation.At.IsZero() && !observation.Value.IsZero() && !observation.Value.After(observation.At) {
				state = "fresh"
				if observation.At.Sub(observation.Value) > config.Targets.RPO {
					state = "stale"
				}
			}
			output, err = json.Marshal(struct {
				Version     string              `json:"version"`
				State       string              `json:"state"`
				Observation ops.TimeObservation `json:"observation"`
			}{Version: "amos-backup-check-v1", State: state, Observation: observation})
			if err != nil {
				return fail()
			}
		}
	}
	output = bytes.TrimSuffix(output, []byte{'\n'})
	if len(output)+1 > 64<<10 {
		return fail()
	}
	if _, err := out.Write(append(output, '\n')); err != nil {
		return fail()
	}
	if unsuccessful {
		return fail()
	}
	return 0
}
func resolveBackupReference(ref string) (string, error) {
	if !strings.HasPrefix(ref, "env://") {
		return "", errBackupCLI
	}
	value, ok := os.LookupEnv(strings.TrimPrefix(ref, "env://"))
	if !ok || value == "" || len(value) > 8192 {
		return "", errBackupCLI
	}
	return value, nil
}
func openBackupDatabase(ctx context.Context, dsn, role, database string) (*sql.DB, error) {
	// Every ambient selector is rejected even if empty or overridden by the URL.
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "PG") {
			return nil, errBackupCLI
		}
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errBackupCLI
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var actualRole, actualDatabase string
	if db.QueryRowContext(check, "SELECT session_user,current_database()").Scan(&actualRole, &actualDatabase) != nil || actualRole != role || actualDatabase != database {
		_ = db.Close()
		return nil, errBackupCLI
	}
	return db, nil
}
func trustedBackupOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || stat.Uid == uint32(os.Geteuid()))
}
func readBackupConfiguration(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errBackupCLI
	}
	// Retain a verified descriptor at each component rather than reopening an
	// ancestor by pathname after checking it. A sticky root-owned ancestor may
	// contain the first protected owner directory, but no writable child is used.
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, errBackupCLI
	}
	defer func() { _ = root.Close() }()
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		if component == "" {
			continue
		}
		before, err := root.Lstat(component)
		if err != nil || !protectedBackupDirectory(before) {
			return nil, errBackupCLI
		}
		next, err := root.OpenRoot(component)
		if err != nil {
			return nil, errBackupCLI
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) || !protectedBackupDirectory(after) {
			_ = next.Close()
			return nil, errBackupCLI
		}
		_ = root.Close()
		root = next
	}
	before, err := root.Lstat(filepath.Base(path))
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || !trustedBackupOwner(before) || before.Mode().Perm()&0022 != 0 || before.Size() > 64<<10 {
		return nil, errBackupCLI
	}
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, errBackupCLI
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || !trustedBackupOwner(after) || after.Mode().Perm()&0022 != 0 {
		return nil, errBackupCLI
	}
	body, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return nil, errBackupCLI
	}
	return body, nil
}

func protectedBackupDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !trustedBackupOwner(info) {
		return false
	}
	if info.Mode().Perm()&0022 == 0 {
		return true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
}
