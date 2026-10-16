package localcapture

import (
	"context"
	email "github.com/ajent-social/amos/delivery/email"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentTLSCaptureRemainsPrivateAndOriginBound(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	origin := "https://127.0.0.1:8443"
	capture, err := NewDevelopmentTLS(dir, origin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = capture.Close() }()
	render := func(origin string) email.Message {
		t.Helper()
		renderer, err := email.NewRenderer(email.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: origin, MaxBodyBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		material, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		message, err := renderer.Render(id, email.Request{Template: email.TemplateVerifyEmail, MaterialRef: email.SecretReference("material:" + material.String()), ExpiresInSeconds: 600}, email.PrivateMaterial{Recipient: "developer@example.test", ActionURL: origin + "/verify-email?challenge=synthetic&token=synthetic"})
		if err != nil {
			t.Fatal(err)
		}
		return message
	}
	message := render(origin)
	for i := 0; i < 2; i++ {
		receipt, err := capture.Send(context.Background(), message)
		if err != nil || receipt.State() != email.ReceiptAccepted {
			t.Fatal("private capture failed", err)
		}
	}
	if _, err := capture.Reconcile(context.Background(), message.JobID()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, message.JobID().String()+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("message not private")
	}
	if _, err := capture.Send(context.Background(), render("https://127.0.0.1:9443")); err == nil {
		t.Fatal("foreign action accepted")
	}
	if _, err := New(dir, origin, false); err == nil {
		t.Fatal("legacy remote mode widened")
	}
	for _, origin := range []string{"http://127.0.0.1:8443", "https://example.invalid:8443", "https://192.0.2.1:8443", "https://127.0.0.1:8443/"} {
		if got, err := NewDevelopmentTLS(dir, origin); err == nil {
			_ = got.Close()
			t.Fatal("invalid TLS capture origin accepted")
		}
	}
	shared := t.TempDir()
	if err := os.Chmod(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := NewDevelopmentTLS(shared, origin); err == nil {
		_ = got.Close()
		t.Fatal("shared directory accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if got, err := NewDevelopmentTLS(link, origin); err == nil {
		_ = got.Close()
		t.Fatal("symlink directory accepted")
	}
}
