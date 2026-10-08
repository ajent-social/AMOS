package session

import (
	"context"
	"errors"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"time"
)

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// CheckStagedWriter is the session-owned plain-read final fence. It obtains no
// new SQL capability and can only use the original staged lexical transaction.
func (s *Service) CheckStagedWriter(ctx context.Context, a *aw.Attempt, staged Staged, f time.Time) error {
	d := staged.data
	if d == nil {
		return writerFailure(a, ErrUnavailable)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	wasChecked := d.checked
	d.checked = true
	d.finalOK = false
	if wasChecked || s == nil || d.service != s || d.attempt != a || d.tx == nil || !a.IsFinalSample(f) {
		return writerFailure(a, ErrUnavailable)
	}
	if _, err := s.writerRequest(ctx, d.request); err != nil {
		return writerFailure(a, err)
	}
	now, _, err := s.readCurrentAt(ctx, d.tx, d.inserted.person, d.newID, false, f)
	if err != nil {
		return writerFailure(a, err)
	}
	if !sameSession(d.inserted, now) {
		return writerFailure(a, ErrUnavailable)
	}
	if !f.Before(d.issued.AssuranceExpires) {
		return writerFailure(a, ErrUnauthenticated)
	}
	if d.priorID != uuid.Nil {
		old, _, err := s.readCurrentAt(ctx, d.tx, d.rotated.person, d.priorID, false, f)
		if !errors.Is(err, ErrUnauthenticated) || !old.revoked.Valid || !sameSession(d.rotated, old) {
			return writerFailure(a, ErrUnavailable)
		}
		if f.Before(old.revoked.Time) {
			return writerFailure(a, ErrUnavailable)
		}
	}
	if !a.IsFinalSample(f) {
		return writerFailure(a, ErrUnavailable)
	}
	if _, err := s.writerRequest(ctx, d.request); err != nil {
		return writerFailure(a, err)
	}
	d.finalOK = true
	return nil
}
