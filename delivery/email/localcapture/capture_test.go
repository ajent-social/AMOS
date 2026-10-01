package localcapture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	email "github.com/ajent-social/amos/delivery/email"
	"github.com/google/uuid"
)

func captureMessage(t *testing.T, id uuid.UUID) email.Message {
	t.Helper()
	renderer, e := email.NewRenderer(email.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "http://127.0.0.1:8080", DevelopmentLoopback: true, MaxBodyBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	ref, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	m, e := renderer.Render(id, email.Request{Template: email.TemplateVerifyEmail, MaterialRef: email.SecretReference("material:" + ref.String()), ExpiresInSeconds: 1800}, email.PrivateMaterial{Recipient: "developer@example.test", ActionURL: "http://127.0.0.1:8080/verify-email?challenge=synthetic&token=synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func newCapture(t *testing.T) (*Capture, string) {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	c, e := New(dir, "http://127.0.0.1:8080", true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, dir
}
func TestCapturePrivateIdempotentMessageAndReconciliation(t *testing.T) {
	c, dir := newCapture(t)
	id, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	message := captureMessage(t, id)
	for i := 0; i < 2; i++ {
		r, e := c.Send(context.Background(), message)
		if e != nil || r.State() != email.ReceiptAccepted {
			t.Fatalf("local capture unavailable: %v", e)
		}
	}
	r, e := c.Reconcile(context.Background(), id)
	if e != nil || r.State() != email.ReceiptAccepted {
		t.Fatal("local receipt unavailable")
	}
	name := filepath.Join(dir, id.String()+".json")
	st, e := os.Stat(name)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("message permission changed")
	}
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	var m envelope
	if json.Unmarshal(b, &m) != nil || m.Recipient != message.Recipient() || m.Text != message.TextBody() {
		t.Fatal("capture message differs")
	}
	if e := os.WriteFile(name, []byte("incomplete"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Send(context.Background(), message); e == nil {
		t.Fatal("incomplete existing message overwritten")
	}
	if _, e := c.Reconcile(context.Background(), id); e == nil {
		t.Fatal("incomplete receipt accepted")
	}
}

func TestCaptureRejectsActionFromDifferentLocalOrigin(t *testing.T) {
	c, dir := newCapture(t)
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := email.NewRenderer(email.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "http://127.0.0.1:9090", DevelopmentLoopback: true, MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	message, err := renderer.Render(id, email.Request{Template: email.TemplateVerifyEmail, MaterialRef: email.SecretReference("material:" + ref.String()), ExpiresInSeconds: 1800}, email.PrivateMaterial{Recipient: "developer@example.test", ActionURL: "http://127.0.0.1:9090/verify-email?challenge=synthetic&token=synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), message); err == nil {
		t.Fatal("foreign local origin accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected message persisted")
	}
}
func TestCaptureRejectsRemoteModeSharedDirectoryAndSymlink(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	for _, tc := range []struct {
		origin  string
		enabled bool
	}{{"http://127.0.0.1:8080", false}, {"https://app.example.test", true}, {"http://127.0.0.2:8080", true}} {
		if c, e := New(dir, tc.origin, tc.enabled); e == nil {
			_ = c.Close()
			t.Fatal("remote or implicit local capture accepted")
		}
	}
	_ = os.Chmod(dir, 0755)
	if c, e := New(dir, "http://127.0.0.1:8080", true); e == nil {
		_ = c.Close()
		t.Fatal("shared mailbox accepted")
	}
	_ = os.Chmod(dir, 0700)
	link := filepath.Join(t.TempDir(), "linked")
	if e := os.Symlink(dir, link); e != nil {
		t.Fatal(e)
	}
	if c, e := New(link, "http://127.0.0.1:8080", true); e == nil {
		_ = c.Close()
		t.Fatal("symlink directory accepted")
	}
	c, _ := newCapture(t)
	id, _ := uuid.NewV7()
	outside := filepath.Join(t.TempDir(), "outside")
	if e := os.WriteFile(outside, []byte("owned sentinel"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := c.root.Symlink(outside, id.String()+".json"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Send(context.Background(), captureMessage(t, id)); e == nil {
		t.Fatal("message symlink accepted")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "owned sentinel" {
		t.Fatal("outside file modified")
	}
}
func TestCaptureBoundedMailboxAndClosedState(t *testing.T) {
	c, dir := newCapture(t)
	for i := 0; i < maxMessages; i++ {
		if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("owned-fixture-%d", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	id, _ := uuid.NewV7()
	r, e := c.Send(context.Background(), captureMessage(t, id))
	if e != nil || r.State() != email.ReceiptRejected {
		t.Fatal("full mailbox accepted another message")
	}
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Send(context.Background(), captureMessage(t, id)); e == nil {
		t.Fatal("closed mailbox accepted")
	}
}
