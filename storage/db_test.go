package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpenDoesNotEchoDSNOnInvalidConfiguration(t *testing.T) {
	const marker = "dsn-sentinel"
	for _, dsn := range []string{
		"postgres://test-user:" + marker + "%zz@localhost/amos",
		"postgres://test-user:" + marker + "@localhost/amos?sslmode=invalid",
	} {
		db, err := Open(context.Background(), dsn)
		if db != nil {
			if closeErr := db.Close(); closeErr != nil {
				t.Fatalf("close unexpected DB handle: %v", closeErr)
			}
			t.Fatal("Open() returned a handle for an invalid URL")
		}
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Open() error = %v, want ErrInvalidConfig", err)
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), dsn) {
			t.Fatalf("Open() error leaked connection URL: %v", err)
		}
	}
}

func TestOpenRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const marker = "dsn-sentinel"
	db, err := Open(ctx, "postgres://test-user:"+marker+"@127.0.0.1:55439/amos")
	if db != nil {
		_ = db.Close()
		t.Fatal("Open() returned a handle for a canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("Open() error leaked connection credentials: %v", err)
	}
}

func TestOpenPingsAndDoesNotEchoDSNOnFailure(t *testing.T) {
	const marker = "dsn-sentinel"
	dsn := "postgres://test-user:" + marker + "@127.0.0.1:1/amos"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn)
	if db != nil {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close unexpected DB handle: %v", closeErr)
		}
		t.Fatal("Open() returned a handle for an unavailable server")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open() error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), dsn) {
		t.Fatalf("Open() error leaked connection URL: %v", err)
	}
}
