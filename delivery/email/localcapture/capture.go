// Package localcapture is an explicitly unqualified local development mailbox.
// It never acts as a live provider and never emits message bodies to logs.
package localcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"

	email "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/internal/receipt"
	"github.com/google/uuid"
)

const maxMessageBytes = 160 << 10
const maxMessages = 128

var ErrUnavailable = errors.New("local development mailbox unavailable")

type Capture struct {
	root   *os.Root
	origin string
	mu     sync.Mutex
}
type envelope struct {
	JobID     string `json:"job_id"`
	Recipient string `json:"recipient"`
	Sender    string `json:"sender"`
	Subject   string `json:"subject"`
	Text      string `json:"text"`
	HTML      string `json:"html"`
}

// New requires an existing private directory and explicit loopback development
// mode. The caller owns lifecycle and documents this as local capture, not email
// delivery qualification. No directory or existing resource is modified here.
func New(directory, origin string, developmentLoopback bool) (*Capture, error) {
	if !developmentLoopback {
		return nil, ErrUnavailable
	}
	parsed, e := email.ParseApplicationOrigin(origin, true)
	if e != nil {
		return nil, ErrUnavailable
	}
	before, e := os.Lstat(directory)
	if e != nil || !privateDirectory(before) {
		return nil, ErrUnavailable
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, ErrUnavailable
	}
	f, e := root.Open(".")
	if e != nil {
		_ = root.Close()
		return nil, ErrUnavailable
	}
	after, e := f.Stat()
	_ = f.Close()
	if e != nil || !privateDirectory(after) || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, ErrUnavailable
	}
	return &Capture{root: root, origin: parsed.String()}, nil
}
func (c *Capture) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == nil {
		return nil
	}
	e := c.root.Close()
	c.root = nil
	return e
}

func (c *Capture) Send(ctx context.Context, m email.Message) (email.Receipt, error) {
	if ctx == nil || ctx.Err() != nil || c == nil || m.JobID().Version() != 7 || !validAction(m.TextBody(), c.origin) {
		return email.Receipt{}, ErrUnavailable
	}
	data, e := json.Marshal(envelope{m.JobID().String(), m.Recipient(), m.Sender(), m.Subject(), m.TextBody(), m.HTMLBody()})
	if e != nil || len(data) > maxMessageBytes {
		return email.Receipt{}, ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == nil {
		return email.Receipt{}, ErrUnavailable
	}
	name := m.JobID().String() + ".json"
	if old, e := c.read(name); e == nil {
		if !bytes.Equal(old, data) {
			return email.Receipt{}, ErrUnavailable
		}
		return receipt.AcceptedResult("local-capture:" + m.JobID().String()), nil
	} else if !errors.Is(e, fs.ErrNotExist) {
		return email.Receipt{}, ErrUnavailable
	}
	directory, e := c.root.Open(".")
	if e != nil {
		return email.Receipt{}, ErrUnavailable
	}
	entries, readErr := directory.ReadDir(maxMessages)
	closeErr := directory.Close()
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil || len(entries) >= maxMessages {
		return receipt.RejectedResult("development.mailbox_full", false, 0), nil
	}
	f, e := c.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return email.Receipt{}, ErrUnavailable
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	fileCloseErr := f.Close()
	if writeErr != nil || syncErr != nil || fileCloseErr != nil {
		return email.Receipt{}, ErrUnavailable
	}
	return receipt.AcceptedResult("local-capture:" + m.JobID().String()), nil
}
func (c *Capture) Reconcile(ctx context.Context, id uuid.UUID) (email.Receipt, error) {
	if ctx == nil || ctx.Err() != nil || c == nil || id.Version() != 7 {
		return email.Receipt{}, ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == nil {
		return email.Receipt{}, ErrUnavailable
	}
	data, e := c.read(id.String() + ".json")
	if e != nil {
		return email.Receipt{}, ErrUnavailable
	}
	var m envelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF || m.JobID != id.String() || m.Recipient == "" || !validAction(m.Text, c.origin) {
		return email.Receipt{}, ErrUnavailable
	}
	return receipt.AcceptedResult("local-capture:" + id.String()), nil
}
func (c *Capture) read(name string) ([]byte, error) {
	f, e := openPrivate(c.root, name)
	if e != nil {
		return nil, e
	}
	defer func() { _ = f.Close() }()
	b, e := io.ReadAll(io.LimitReader(f, maxMessageBytes+1))
	if e != nil || len(b) > maxMessageBytes {
		return nil, ErrUnavailable
	}
	return b, nil
}

func validAction(text, origin string) bool {
	first, _, _ := strings.Cut(text, "\n")
	if len(first) > 2048 || strings.TrimSpace(first) != first {
		return false
	}
	u, err := url.Parse(first)
	return err == nil && u.User == nil && u.Fragment == "" && u.RawPath == "" && u.Scheme+"://"+u.Host == origin && (u.Path == "/verify-email" || u.Path == "/reset-password" || u.Path == "/magic-link")
}
