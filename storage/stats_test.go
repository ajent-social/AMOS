package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
)

func TestMetricsStatsLocalAndRuntimeHandles(t *testing.T) {
	pool, _ := testkit.NewPostgres(t)
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if extra, err := pool.Conn(waitCtx); err == nil {
		_ = extra.Close()
		t.Fatal("pool did not saturate")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	db := &DB{pool: pool, migrationPool: pool}
	defer func() { _ = db.Close() }()
	stats, err := db.Stats()
	if err != nil || stats.OpenConnections < 1 || stats.MaxOpenConnections != 1 || stats.WaitCount < 1 || stats.WaitDuration <= 0 {
		t.Fatal(stats, err)
	}
	runtime := &RuntimeDB{pool: pool}
	rstats, err := runtime.Stats()
	if err != nil || rstats.OpenConnections < 1 {
		t.Fatal(rstats, err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Stats(); !errors.Is(err, ErrClosed) {
		t.Fatal("runtime closed stats", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Stats(); !errors.Is(err, ErrClosed) {
		t.Fatal("local closed stats", err)
	}
	var nilDB *DB
	var nilRuntime *RuntimeDB
	if _, err := nilDB.Stats(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := nilRuntime.Stats(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
