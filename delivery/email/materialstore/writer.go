package materialstore

import (
	"context"
	"net/url"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

// Writer participates only in the root-owned W1 material insertion step. It
// retains neither a pool nor a runner and cannot resolve or prune material.
type Writer struct{ data *writerData }

func NewWriter(cfg TxConfig) (*Writer, error) {
	data, err := newWriterData(cfg)
	if err != nil {
		return nil, err
	}
	return &Writer{data: data}, nil
}

func (w *Writer) PutVerificationWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, ref email.SecretReference, material email.PrivateMaterial, expiry time.Time) error {
	return w.putWriter(ctx, a, delivery, ref, material, expiry, "/verify-email")
}
func (w *Writer) PutSignInWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, ref email.SecretReference, material email.PrivateMaterial, expiry time.Time) error {
	return w.putWriter(ctx, a, delivery, ref, material, expiry, "/magic-link")
}
func (w *Writer) PutPasswordResetWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, ref email.SecretReference, material email.PrivateMaterial, expiry time.Time) error {
	return w.putWriter(ctx, a, delivery, ref, material, expiry, "/reset-password")
}

func (w *Writer) putWriter(ctx context.Context, a *aw.Attempt, delivery aw.Delivery, ref email.SecretReference, material email.PrivateMaterial, expiry time.Time, purpose string) error {
	if w == nil || w.data == nil || ctx == nil {
		return materialWriterFailure(a)
	}
	realm, err := a.Realm()
	if err != nil || realm.Installation != w.data.installation || realm.Application != w.data.application || realm.Environment != w.data.environment {
		return materialWriterFailure(a)
	}
	if !w.data.validWriterMaterial(delivery, ref, material, expiry, purpose) {
		return materialWriterFailure(a)
	}
	tx, err := a.DeliveryTx(ctx, delivery, aw.MaterialInsert)
	if err != nil {
		return materialWriterFailure(a)
	}
	if err := a.RecordMutation(aw.DeliveryWrite, nil); err != nil {
		return materialWriterFailure(a)
	}
	if err := w.data.putMaterial(ctx, tx, ref, material, expiry, purpose); err != nil {
		return materialWriterFailure(a)
	}
	return nil
}

// A failed participant is terminal even if its caller mistakenly ignores the
// returned error. Finish grants no SQL or commit authority for this outcome.
func materialWriterFailure(a *aw.Attempt) error {
	if err := a.Finish(aw.UnavailableRollback); err != nil {
		return ErrUnavailable
	}
	return ErrUnavailable
}

func (s *writerData) validWriterMaterial(delivery aw.Delivery, ref email.SecretReference, material email.PrivateMaterial, expiry time.Time, purpose string) bool {
	id, err := parseRef(ref)
	action, actionErr := url.Parse(material.ActionURL)
	return err == nil && id == delivery.MaterialID && s.validMaterial(material) && actionErr == nil && action.Path == purpose && !expiry.IsZero()
}
