package payload

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ajent-social/amos/internal/diagnosticcontract"
)

func FuzzDecode(f *testing.F) {
	data := fixture(f)
	gz := compress(f, data, "")
	f.Add(uint8(0), data, "application/json")
	f.Add(uint8(1), gz, "application/json; charset=utf-8")
	f.Add(uint8(1), append(bytes.Clone(gz), 0), "application/json")
	f.Add(uint8(1), append(bytes.Clone(gz), compress(f, nil, "")...), "application/json")
	f.Add(uint8(0), append([]byte(`{"\u0073chema":"other",`), data[1:]...), "application/json")
	f.Add(uint8(0), []byte{0xff}, "application/json")
	f.Add(uint8(0), data, "application/json;charset=utf-8;charset=utf-8")
	f.Add(uint8(4), data, "application/json")
	f.Fuzz(func(t *testing.T, mode uint8, wire []byte, contentType string) {
		meta := metadata(wire)
		meta.Headers.Set("Content-Type", contentType)
		if mode&1 != 0 {
			meta.Headers.Set("Content-Encoding", "gzip")
		}
		if mode&2 != 0 {
			meta.ContentLength = -1
		}
		if mode&4 != 0 {
			meta.Headers["content-type"] = []string{contentType}
		}
		got, err := Decode(meta, wire)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || err.Error() != "diagnostic payload rejected" || len(got.Bytes()) != 0 {
				t.Fatal("non-generic or data-bearing rejection")
			}
			return
		}
		out := got.Bytes()
		if len(wire) > MaxBytes || len(out) > MaxBytes || diagnosticcontract.DecodeAndValidate(out) != nil {
			t.Fatal("invalid bounded output")
		}
		again, e := Decode(metadata(out), out)
		if e != nil || !bytes.Equal(out, again.Bytes()) {
			t.Fatal("unstable canonical output")
		}
		out[0] = '!'
		if bytes.Equal(out, got.Bytes()) {
			t.Fatal("output exposes mutable backing bytes")
		}
	})
}
