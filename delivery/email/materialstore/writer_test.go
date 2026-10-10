package materialstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

func TestDeliveryWriterConfigurationCopy(t *testing.T) {
	cfg := materialTxConfig(t)
	original := append([]byte(nil), cfg.Keys["v1"]...)
	w, err := NewWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Keys["v1"][0] ^= 255
	delete(cfg.Keys, "v1")
	cfg.ApplicationOrigin = "https://changed.example.test"
	block, err := aes.NewCipher(original)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, expected.NonceSize())
	aad := w.data.aad(cfg.InstallationID, "v1", time.Unix(123, 0))
	plain := []byte("synthetic material")
	sealed := w.data.keys["v1"].Seal(nil, nonce, plain, aad)
	got, err := expected.Open(nil, nonce, sealed, aad)
	if err != nil || !bytes.Equal(got, plain) || w.data.origin != "https://app.example.test" {
		t.Fatal("configuration mutation changed writer")
	}
	for name, change := range map[string]func(*TxConfig){
		"scope":  func(c *TxConfig) { c.EnvironmentID = uuid.Nil },
		"active": func(c *TxConfig) { c.ActiveKeyID = "missing" },
		"key":    func(c *TxConfig) { c.Keys["v1"] = make([]byte, 31) },
		"origin": func(c *TxConfig) { c.ApplicationOrigin = "http://app.example.test" },
	} {
		t.Run(name, func(t *testing.T) {
			c := materialTxConfig(t)
			change(&c)
			if w, e := NewWriter(c); w != nil || !errors.Is(e, ErrConfiguration) {
				t.Fatal("invalid config admitted")
			}
		})
	}
}

func TestDeliveryWriterPurposeAndReference(t *testing.T) {
	w, err := NewWriter(materialTxConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	ref := reference(t)
	id, err := parseRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	d := aw.Delivery{MaterialID: id, JobKey: "delivery"}
	expiry := time.Now().Add(time.Minute)
	for _, purpose := range []string{"/verify-email", "/magic-link", "/reset-password"} {
		t.Run(purpose, func(t *testing.T) {
			m := email.PrivateMaterial{Recipient: "person@example.test", ActionURL: "https://app.example.test" + purpose + "?token=synthetic"}
			if !w.data.validWriterMaterial(d, ref, m, expiry, purpose) {
				t.Fatal("valid purpose denied")
			}
			for _, other := range []string{"/verify-email", "/magic-link", "/reset-password"} {
				if other != purpose && w.data.validWriterMaterial(d, ref, m, expiry, other) {
					t.Fatal("cross-purpose material admitted")
				}
			}
			bad := m
			bad.ActionURL = "https://other.example.test" + purpose
			if w.data.validWriterMaterial(d, ref, bad, expiry, purpose) {
				t.Fatal("foreign origin admitted")
			}
			if w.data.validWriterMaterial(aw.Delivery{}, ref, m, expiry, purpose) {
				t.Fatal("wrong reference admitted")
			}
			if w.data.validWriterMaterial(d, ref, m, time.Time{}, purpose) {
				t.Fatal("zero expiry admitted")
			}
		})
	}
}

func TestDeliveryWriterUnrootedFailsClosed(t *testing.T) {
	w, err := NewWriter(materialTxConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []*aw.Attempt{nil, {}} {
		for _, writer := range []*Writer{nil, {}, w} {
			for _, put := range []func(context.Context, *aw.Attempt, aw.Delivery, email.SecretReference, email.PrivateMaterial, time.Time) error{writer.PutVerificationWriter, writer.PutSignInWriter, writer.PutPasswordResetWriter} {
				if err := put(context.Background(), a, aw.Delivery{}, "", email.PrivateMaterial{}, time.Time{}); !errors.Is(err, ErrUnavailable) {
					t.Fatal("unrooted write admitted", err)
				}
			}
		}
	}
}
