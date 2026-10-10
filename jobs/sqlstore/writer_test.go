package sqlstore

import (
	"context"
	"errors"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs"
)

func TestDeliveryJobWriterUnrooted(t *testing.T) {
	w, err := NewTxWriter(Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 3, MaxLease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range []*TxWriter{nil, {}, w} {
		for _, a := range []*aw.Attempt{nil, {}} {
			job, err := writer.EnqueueWriter(context.Background(), a, aw.Delivery{}, jobs.Intent{})
			if !errors.Is(err, ErrUnavailable) || job.ID.String() != "00000000-0000-0000-0000-000000000000" {
				t.Fatal("unrooted job admitted")
			}
		}
	}
}
