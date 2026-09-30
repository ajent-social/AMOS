package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ajent-social/amos/migrations"
)

var (
	// ErrMigrationHistory means the applied ledger is not a prefix of registry.
	ErrMigrationHistory = errors.New("applied migration history differs from registry")
	// ErrMigrationChecksum means an applied migration's source has changed.
	ErrMigrationChecksum = errors.New("applied migration checksum changed")
	// ErrMigrationApply means a registered migration or ledger write failed.
	ErrMigrationApply = errors.New("migration failed")
)

const (
	migrationLockNamespace int32 = 0x414d4f53 // "AMOS"
	migrationLockResource  int32 = 0x4d494752 // "MIGR"
)

const createLedgerSQL = `
CREATE TABLE IF NOT EXISTS amos_schema_migrations (
	sequence BIGINT PRIMARY KEY CHECK (sequence > 0),
	migration_id TEXT NOT NULL UNIQUE,
	checksum BYTEA NOT NULL CHECK (octet_length(checksum) = 32),
	applied_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp()
);
CREATE OR REPLACE FUNCTION amos_reject_schema_migration_mutation()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
	RAISE EXCEPTION 'AMOS migration ledger is append-only';
END;
$function$;
DO $block$
BEGIN
	IF NOT EXISTS (
		SELECT 1 FROM pg_trigger
		WHERE tgrelid = 'amos_schema_migrations'::regclass
		  AND tgname = 'amos_schema_migrations_no_row_mutation'
		  AND NOT tgisinternal
	) THEN
		CREATE TRIGGER amos_schema_migrations_no_row_mutation
		BEFORE UPDATE OR DELETE ON amos_schema_migrations
		FOR EACH ROW EXECUTE FUNCTION amos_reject_schema_migration_mutation();
	END IF;
	IF NOT EXISTS (
		SELECT 1 FROM pg_trigger
		WHERE tgrelid = 'amos_schema_migrations'::regclass
		  AND tgname = 'amos_schema_migrations_no_truncate'
		  AND NOT tgisinternal
	) THEN
		CREATE TRIGGER amos_schema_migrations_no_truncate
		BEFORE TRUNCATE ON amos_schema_migrations
		FOR EACH STATEMENT EXECUTE FUNCTION amos_reject_schema_migration_mutation();
	END IF;
END;
$block$`

type appliedMigration struct {
	sequence int64
	id       string
	checksum []byte
}

// Migrate applies each registered migration in order. A PostgreSQL advisory
// transaction lock serializes both local and cross-process migrators. Each SQL
// source and ledger row commit atomically in its own transaction.
func Migrate(ctx context.Context, db *DB, registry migrations.Registry) error {
	if ctx == nil || db == nil {
		return ErrInvalidConfig
	}
	entries := registry.Migrations()

	// Validate existing history before any new migration can change data.
	if err := db.withMigrationTx(ctx, func(tx *sql.Tx) error {
		if err := lockMigrations(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, createLedgerSQL); err != nil {
			return fmt.Errorf("create migration ledger: %w: %w", ErrMigrationApply, err)
		}
		applied, err := readApplied(ctx, tx)
		if err != nil {
			return fmt.Errorf("read migration ledger: %w: %w", ErrMigrationApply, err)
		}
		return validateHistory(applied, entries)
	}); err != nil {
		return err
	}

	for _, migration := range entries {
		migration := migration
		if err := db.withMigrationTx(ctx, func(tx *sql.Tx) error {
			if err := lockMigrations(ctx, tx); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, createLedgerSQL); err != nil {
				return fmt.Errorf("create migration ledger: %w: %w", ErrMigrationApply, err)
			}
			applied, err := readApplied(ctx, tx)
			if err != nil {
				return fmt.Errorf("read migration ledger: %w: %w", ErrMigrationApply, err)
			}
			if err := validateHistory(applied, entries); err != nil {
				return err
			}
			if uint64(len(applied)) >= migration.Sequence {
				return nil
			}
			if migration.Sequence != uint64(len(applied)+1) {
				return ErrMigrationHistory
			}

			if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
				return fmt.Errorf("apply migration %s: %w: %w", migration.ID(), ErrMigrationApply, err)
			}
			checksum := sha256.Sum256([]byte(migration.SQL))
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO amos_schema_migrations (sequence, migration_id, checksum) VALUES ($1, $2, $3)`,
				int64(migration.Sequence), migration.ID(), checksum[:]); err != nil {
				return fmt.Errorf("record migration %s: %w: %w", migration.ID(), ErrMigrationApply, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func lockMigrations(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, migrationLockNamespace, migrationLockResource); err != nil {
		return fmt.Errorf("acquire migration lock: %w: %w", ErrMigrationApply, err)
	}
	return nil
}

func readApplied(ctx context.Context, tx *sql.Tx) ([]appliedMigration, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sequence, migration_id, checksum FROM amos_schema_migrations ORDER BY sequence`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var applied []appliedMigration
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.sequence, &item.id, &item.checksum); err != nil {
			return nil, err
		}
		applied = append(applied, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

func validateHistory(applied []appliedMigration, registry []migrations.Migration) error {
	if len(applied) > len(registry) {
		return ErrMigrationHistory
	}
	for index, item := range applied {
		migration := registry[index]
		if item.sequence != int64(index+1) || item.id != migration.ID() {
			return ErrMigrationHistory
		}
		checksum := sha256.Sum256([]byte(migration.SQL))
		if !bytes.Equal(item.checksum, checksum[:]) {
			return fmt.Errorf("migration %s: %w", migration.ID(), ErrMigrationChecksum)
		}
	}
	return nil
}
