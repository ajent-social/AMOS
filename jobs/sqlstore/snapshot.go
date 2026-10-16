package sqlstore

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/ajent-social/amos/jobs"
)

// Snapshot reads retained-row aggregates using exactly this store's claim scope.
// It never leases, repairs or mutates rows. Caller cancellation bounds the query.
func (s *Store) Snapshot(ctx context.Context) (jobs.Snapshot, error) {
	if s == nil || s.runner == nil || ctx == nil {
		return jobs.Snapshot{}, ErrInvalidConfig
	}
	var out jobs.Snapshot
	// Snapshot always requests a read-only transaction, including legacy pool stores.
	reader := *s
	reader.runtimeOptions = true
	err := reader.withTx(ctx, true, func(tx *sql.Tx) error {
		var age float64
		var retries string
		var last sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT clock_timestamp(),
 count(*) FILTER(WHERE status='queued'),count(*) FILTER(WHERE status='unknown'),count(*) FILTER(WHERE status='leased'),count(*) FILTER(WHERE status='dead'),
 COALESCE(GREATEST(EXTRACT(EPOCH FROM clock_timestamp()-min(created_at) FILTER(WHERE status='queued')),0),0)::double precision,
 LEAST(COALESCE(sum(GREATEST(attempt_count::numeric-1,0) + reconciliation_attempt_count::numeric),0),18446744073709551615)::text,
 max(updated_at) FILTER(WHERE status='succeeded')
 FROM amos_jobs AS j WHERE (COALESCE(cardinality($1::text[]),0)=0 OR kind=ANY($1::text[])) AND `+claimScopePredicate("$2", "j"), s.config.ClaimKinds, s.claimScopesJSON).Scan(&out.ObservedAt, &out.Queued, &out.Unknown, &out.Leased, &out.Dead, &age, &retries, &last)
		if err != nil {
			return ErrUnavailable
		}
		out.OldestQueuedAge = time.Duration(min(age, float64((1<<63-1)/int64(time.Second))) * float64(time.Second))
		value, err := strconv.ParseUint(retries, 10, 64)
		if err != nil {
			return ErrUnavailable
		}
		out.RetryAttempts = value
		out.LastSuccessKnown = last.Valid
		if last.Valid {
			out.LastSuccess = last.Time
		}
		return nil
	})
	if err != nil {
		return jobs.Snapshot{}, err
	}
	return out, nil
}
