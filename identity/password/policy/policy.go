// Package policy supplies a versioned, local common-password blocklist.
package policy

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

const Version = "amos-common-100k-20261001"
const AssetSHA256 = "a0802be3bba37cde6697251c2a92cf2a9e66a4f4a5575a626d70b6a642db6493"
const MaxSupplementBytes = 1 << 20

//go:embed common-sha256.txt
var common []byte
var ErrUnavailable = errors.New("password blocklist policy unavailable")

// Set is immutable after construction. It stores only digest membership.
type Set struct {
	hashes map[[32]byte]struct{}
	ready  bool
}

// New loads the pinned built-in snapshot and optional owner-reviewed supplement.
// Supplement rows are lowercase SHA-256 of NFC-normalized phrases, one per line.
// expectedDigest binds exact supplementary bytes; no private file is copied.
func New(supplement io.Reader, expectedDigest string) (*Set, error) {
	asset := sha256.Sum256(common)
	if hex.EncodeToString(asset[:]) != AssetSHA256 {
		return nil, ErrUnavailable
	}
	s := &Set{hashes: make(map[[32]byte]struct{})}
	if e := s.load(common); e != nil {
		return nil, e
	}
	if supplement != nil {
		b, e := io.ReadAll(io.LimitReader(supplement, MaxSupplementBytes+1))
		if e != nil || len(b) > MaxSupplementBytes || len(expectedDigest) != 64 {
			return nil, ErrUnavailable
		}
		d := sha256.Sum256(b)
		if hex.EncodeToString(d[:]) != expectedDigest {
			return nil, ErrUnavailable
		}
		if e = s.load(b); e != nil {
			return nil, e
		}
	} else if expectedDigest != "" {
		return nil, ErrUnavailable
	}
	if len(s.hashes) == 0 {
		return nil, ErrUnavailable
	}
	s.ready = true
	return s, nil
}
func (s *Set) load(data []byte) error {
	scan := bufio.NewScanner(bytes.NewReader(data))
	rows := 0
	for scan.Scan() {
		line := scan.Text()
		if len(line) != 64 || strings.ToLower(line) != line {
			return ErrUnavailable
		}
		b, e := hex.DecodeString(line)
		if e != nil || len(b) != 32 {
			return ErrUnavailable
		}
		var d [32]byte
		copy(d[:], b)
		s.hashes[d] = struct{}{}
		rows++
	}
	if scan.Err() != nil || rows == 0 {
		return ErrUnavailable
	}
	return nil
}
func (s *Set) Ready() bool { return s != nil && s.ready }
func (s *Set) ContainsNormalized(phrase string) bool {
	if !s.Ready() {
		return true
	}
	d := sha256.Sum256([]byte(phrase))
	_, blocked := s.hashes[d]
	return blocked
}
