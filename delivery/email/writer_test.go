package email

import (
	"context"
	"errors"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
)

func TestDeliveryEmailWriterUnrooted(t *testing.T) {
	renderer, err := NewRenderer(RenderConfig{FromAddress: "sender@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlstore.NewTxWriter(sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 3, MaxLease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	d := aw.Delivery{MaterialID: id, JobKey: "delivery"}
	req := Request{Template: TemplateVerifyEmail, MaterialRef: SecretReference("material:" + id.String()), ExpiresInSeconds: 60}
	for _, a := range []*aw.Attempt{nil, {}} {
		job, err := EnqueueWriter(context.Background(), a, d, store, renderer, id, id, d.JobKey, req, time.Now().Add(time.Minute))
		if !errors.Is(err, ErrUnavailable) || job.ID != uuid.Nil {
			t.Fatal("unrooted email admitted")
		}
	}
	intent, err := requestIntent(renderer, id, id, d.JobKey, req, time.Now().Add(time.Minute))
	if err != nil || !intent.ExternalEffect || intent.Kind != Kind {
		t.Fatal("durable email intent changed")
	}
	req.Template = "unknown"
	if _, err := requestIntent(renderer, id, id, d.JobKey, req, time.Now()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("unknown template admitted")
	}
}
