package mfa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"time"

	wp "github.com/ajent-social/amos/identity/internal/writerproof"
)

func factorShape(f Factor) error {
	if !validID(f.ID) || !validScope(f.Scope) || len(f.SeedCiphertext) < 32 || len(f.SeedCiphertext) > 4096 || f.CreatedAt.IsZero() || f.LastUsedStep < -1 || f.FailedAttempts < 0 || f.FailedAttempts > DefaultMaxAttempts {
		return ErrUnavailable
	}
	if (f.FailedAttempts == DefaultMaxAttempts) != (f.LockedUntil != nil) || f.LockedUntil != nil && (!f.LockedUntil.After(f.CreatedAt)) {
		return ErrUnavailable
	}
	switch f.State {
	case FactorPending:
		if f.PendingSecurityEpoch == nil || *f.PendingSecurityEpoch < 0 || f.ExpiresAt == nil || !f.ExpiresAt.Equal(f.CreatedAt.Add(DefaultPendingLifetime)) || f.ActivatedAt != nil || f.LastUsedStep != -1 {
			return ErrUnavailable
		}
	case FactorActive:
		if f.PendingSecurityEpoch != nil || f.ExpiresAt != nil || f.ActivatedAt == nil || f.ActivatedAt.Before(f.CreatedAt) || f.LastUsedStep < 0 {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	return nil
}
func factorCheck(f Factor, epoch, step int64, at time.Time) wp.FactorCheck {
	c := wp.FactorCheck{ID: f.ID, State: string(f.State), Epoch: epoch, LastStep: f.LastUsedStep, AcceptedStep: step, CiphertextDigest: sha256.Sum256(f.SeedCiphertext), FailedAttempts: f.FailedAttempts, VerifiedAt: at}
	if f.LockedUntil != nil {
		c.LockedUntil = *f.LockedUntil
	}
	if f.ExpiresAt != nil {
		c.PendingUntil = *f.ExpiresAt
	}
	if step >= 0 {
		c.WindowUntil = time.Unix((step+2)*30, 0).UTC()
	}
	return c
}
func sameTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
func sameEpoch(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func sameFactor(a, b Factor) bool {
	return a.ID == b.ID && a.Scope == b.Scope && bytes.Equal(a.SeedCiphertext, b.SeedCiphertext) && a.State == b.State && sameEpoch(a.PendingSecurityEpoch, b.PendingSecurityEpoch) && a.LastUsedStep == b.LastUsedStep && a.FailedAttempts == b.FailedAttempts && sameTime(a.LockedUntil, b.LockedUntil) && a.CreatedAt.Equal(b.CreatedAt) && sameTime(a.ExpiresAt, b.ExpiresAt) && sameTime(a.ActivatedAt, b.ActivatedAt)
}
func checkFactorFinal(ctx context.Context, tx *sql.Tx, expected Factor, updated time.Time) error {
	st, err := NewReadStore(tx)
	if err != nil {
		return ErrUnavailable
	}
	final, err := st.Find(ctx, expected.Scope, expected.ID)
	if err != nil {
		return ErrUnavailable
	}
	if !sameFactor(expected, final) {
		return ErrDenied
	}
	var at time.Time
	if err = tx.QueryRowContext(ctx, `SELECT updated_at FROM public.identity_totp_factors WHERE id=$1`, expected.ID).Scan(&at); err != nil {
		return ErrUnavailable
	}
	if !at.Equal(updated) {
		return ErrDenied
	}
	return nil
}
