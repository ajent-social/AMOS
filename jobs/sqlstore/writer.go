package sqlstore

import (
	"context"
	"encoding/json"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs"
)

// EnqueueWriter appends durable intent in the sole JobEnqueue step, after the
// matching material insertion. It does not claim or dispatch any job.
func (w *TxWriter) EnqueueWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, intent jobs.Intent) (jobs.Job, error) {
	if w == nil || w.store == nil || ctx == nil {
		return jobWriterFailure(a)
	}
	realm, err := a.Realm()
	if err != nil || intent.InstallationID != realm.Installation || intent.ApplicationID != realm.Application || intent.Key != delivery.JobKey {
		return jobWriterFailure(a)
	}
	if err := jobs.ValidateIntent(intent, w.store.config.MaxPayloadBytes); err != nil || !json.Valid(intent.Payload) {
		return jobWriterFailure(a)
	}
	tx, err := a.DeliveryTx(ctx, delivery, aw.JobEnqueue)
	if err != nil {
		return jobWriterFailure(a)
	}
	if err := a.RecordMutation(aw.DeliveryWrite, nil); err != nil {
		return jobWriterFailure(a)
	}
	job, err := w.store.EnqueueTx(ctx, tx, intent)
	if err != nil {
		return jobWriterFailure(a)
	}
	return job, nil
}

func jobWriterFailure(a *aw.Attempt) (jobs.Job, error) {
	if err := a.Finish(aw.UnavailableRollback); err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	return jobs.Job{}, ErrUnavailable
}
