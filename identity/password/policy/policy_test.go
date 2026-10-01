package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestLocalPolicyPinnedCommonPhraseAndSupplement(t *testing.T) {
	s, e := New(nil, "")
	if e != nil || !s.Ready() {
		t.Fatal(e)
	}
	if !s.ContainsNormalized("qwertyuiopasdfghjkl") {
		t.Fatal("pinned common phrase accepted")
	}
	phrase := "synthetic owner policy blocked phrase"
	d := sha256.Sum256([]byte(phrase))
	data := hex.EncodeToString(d[:]) + "\n"
	digest := sha256.Sum256([]byte(data))
	s, e = New(strings.NewReader(data), hex.EncodeToString(digest[:]))
	if e != nil || !s.ContainsNormalized(phrase) {
		t.Fatal("supplement not enforced", e)
	}
	if s.ContainsNormalized("an uncommon synthetic testing phrase 9274") {
		t.Fatal("unlisted phrase blocked")
	}
	if _, e = New(strings.NewReader(data), strings.Repeat("0", 64)); e != ErrUnavailable {
		t.Fatal("unbound supplement accepted")
	}
	malformed := "not-a-digest\n"
	digest = sha256.Sum256([]byte(malformed))
	if _, e = New(strings.NewReader(malformed), hex.EncodeToString(digest[:])); e != ErrUnavailable {
		t.Fatal("malformed policy accepted")
	}
	if _, e = New(strings.NewReader(strings.Repeat("x", MaxSupplementBytes+1)), strings.Repeat("0", 64)); e != ErrUnavailable {
		t.Fatal("unbounded policy accepted")
	}
	var unavailable *Set
	if unavailable.Ready() || !unavailable.ContainsNormalized(phrase) {
		t.Fatal("unready policy failed open")
	}
}
