package email

import (
	"context"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
)

// EnqueueWriter stages durable email intent only. The native action must check
// all original challenge, material and job deadlines at the final root sample.
func EnqueueWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, store *sqlstore.TxWriter, renderer *Renderer, installationID, applicationID uuid.UUID, key string, req Request, deadline time.Time) (jobs.Job, error) {
	if store == nil || renderer == nil || req.MaterialRef != SecretReference("material:"+delivery.MaterialID.String()) {
		return emailWriterFailure(a)
	}
	intent, err := requestIntent(renderer, installationID, applicationID, key, req, deadline)
	if err != nil {
		return emailWriterFailure(a)
	}
	job, err := store.EnqueueWriter(ctx, a, delivery, intent)
	if err != nil {
		// The SQL participant has already terminated the attempt.
		return jobs.Job{}, ErrUnavailable
	}
	return job, nil
}

func emailWriterFailure(a *aw.Attempt) (jobs.Job, error) {
	if err := a.Finish(aw.UnavailableRollback); err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	return jobs.Job{}, ErrUnavailable
}
