package apphost

import (
	"context"
	"github.com/ajent-social/amos/jobs"
	"time"
)

// observeMetrics runs only on the existing serialized worker cycle, never on a scrape.
func (h *Host) observeMetrics(ctx context.Context) {
	if h.metrics == nil {
		return
	}
	stats, err := h.db.Stats()
	h.metrics.ObserveDatabase(stats, err)
	snapshotCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var snapshot jobs.Snapshot
	if h.outbox == nil {
		err = ErrLocalDependency
	} else {
		snapshot, err = h.outbox.Snapshot(snapshotCtx)
	}
	_ = h.metrics.ObserveQueue("local-email", snapshot, err)
}
