package sqlstore

import (
	"context"
	"database/sql"

	"github.com/ajent-social/amos/jobs"
)

// TxWriter appends jobs through a caller-owned transaction without owning a
// connection pool. The caller must commit before acknowledging durable intent.
type TxWriter struct{ store *Store }

func NewTxWriter(cfg Config) (*TxWriter, error) {
	if !validConfig(cfg) {
		return nil, ErrInvalidConfig
	}
	cfg.ClaimKinds = append([]string(nil), cfg.ClaimKinds...)
	return &TxWriter{store: &Store{config: cfg}}, nil
}
func (w *TxWriter) EnqueueTx(ctx context.Context, tx *sql.Tx, intent jobs.Intent) (jobs.Job, error) {
	if w == nil || w.store == nil {
		return jobs.Job{}, ErrInvalidConfig
	}
	return w.store.EnqueueTx(ctx, tx, intent)
}
