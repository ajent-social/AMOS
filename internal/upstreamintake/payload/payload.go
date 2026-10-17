// Package payload decodes bounded diagnostic payload bytes. It has no HTTP
// listener, admission, provenance, receipt, storage or publication authority.
package payload

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/ajent-social/amos/internal/diagnosticcontract"
)

const (
	MaxBytes       = diagnosticcontract.MaxPayloadBytes
	MaxHeaderBytes = 8192
)

var ErrInvalid = errors.New("diagnostic payload rejected")

// Metadata describes the supplied HTTP envelope, not authenticated identity.
// Headers must preserve all field values, including duplicates and case aliases.
// ContentLength is the parsed known length; use -1 when unknown. Callers enforce
// raw HTTP framing and must not discard duplicate metadata before this call.
// Inputs must not be mutated concurrently with Decode.
type Metadata struct {
	Headers          http.Header
	ContentLength    int64
	TransferEncoding []string
	Trailers         http.Header
}

// Canonical contains fresh finite-profile JSON only. The zero value is empty,
// not a validated report or any authority-bearing admission capability.
type Canonical struct{ data []byte }

// Bytes returns an independent copy. No input bytes or headers are retained.
func (c Canonical) Bytes() []byte { return bytes.Clone(c.data) }

// Decode validates supplied bounded bytes and returns fixed-order compact JSON.
// It does not read a socket, enforce network deadlines, charge attempts, or
// establish executing-component provenance. Invalid input is never hashed.
func Decode(meta Metadata, wire []byte) (Canonical, error) {
	if len(wire) > MaxBytes || meta.ContentLength < 0 || meta.ContentLength > MaxBytes || meta.ContentLength != int64(len(wire)) || len(meta.TransferEncoding) != 0 || len(meta.Trailers) != 0 {
		return Canonical{}, ErrInvalid
	}
	encoding, err := envelope(meta.Headers, meta.ContentLength)
	if err != nil {
		return Canonical{}, ErrInvalid
	}
	decoded := wire
	if encoding == "gzip" {
		reader := bytes.NewReader(wire)
		gz, e := gzip.NewReader(reader)
		if e != nil {
			return Canonical{}, ErrInvalid
		}
		gz.Multistream(false)
		decoded, e = io.ReadAll(io.LimitReader(gz, MaxBytes+1))
		closeErr := gz.Close()
		if e != nil || closeErr != nil || len(decoded) > MaxBytes {
			return Canonical{}, ErrInvalid
		}
		// Mutation anchor: gzip must consume exactly one member and all input.
		if reader.Len() != 0 {
			return Canonical{}, ErrInvalid
		}
	}
	if diagnosticcontract.DecodeAndValidate(decoded) != nil {
		return Canonical{}, ErrInvalid
	}
	var diagnostic diagnosticcontract.Diagnostic
	if json.Unmarshal(decoded, &diagnostic) != nil {
		return Canonical{}, ErrInvalid
	}
	data, err := json.Marshal(diagnostic)
	if err != nil || len(data) > MaxBytes {
		return Canonical{}, ErrInvalid
	}
	return Canonical{data: data}, nil
}

func envelope(headers http.Header, length int64) (string, error) {
	if len(headers) > MaxHeaderBytes {
		return "", ErrInvalid
	}
	var contentType, encoding, contentLength []string
	total := 0
	for name, values := range headers {
		if len(name) == 0 || len(name) > MaxHeaderBytes || len(values) == 0 || len(values) > MaxHeaderBytes {
			return "", ErrInvalid
		}
		if !fieldName(name) {
			return "", ErrInvalid
		}
		for _, v := range values {
			if len(name)+4 > MaxHeaderBytes-total || len(v) > MaxHeaderBytes-total-len(name)-4 {
				return "", ErrInvalid
			}
			total += len(name) + len(v) + 4
			if total > MaxHeaderBytes || !fieldValue(v) {
				return "", ErrInvalid
			}
		}
		switch strings.ToLower(name) {
		case "content-type":
			if len(values) != 1 || contentType != nil {
				return "", ErrInvalid
			}
			contentType = values
		case "content-encoding":
			if len(values) != 1 || encoding != nil {
				return "", ErrInvalid
			}
			encoding = values
		case "content-length":
			if len(values) != 1 || contentLength != nil {
				return "", ErrInvalid
			}
			contentLength = values
		case "transfer-encoding", "trailer":
			return "", ErrInvalid
		}
	}
	if len(contentType) != 1 || strings.Count(contentType[0], ";") > 1 {
		return "", ErrInvalid
	}
	for _, c := range contentType[0] {
		if c > 127 {
			return "", ErrInvalid
		}
	}
	media, params, err := mime.ParseMediaType(contentType[0])
	if err != nil || media != "application/json" || len(params) > 1 || strings.Contains(contentType[0], ";") && len(params) != 1 {
		return "", ErrInvalid
	}
	for key, value := range params {
		if key != "charset" || !strings.EqualFold(value, "utf-8") {
			return "", ErrInvalid
		}
	}
	if contentLength != nil {
		value := strings.Trim(contentLength[0], " \t")
		if value == "" {
			return "", ErrInvalid
		}
		for _, c := range value {
			if c < '0' || c > '9' {
				return "", ErrInvalid
			}
		}
		parsed, e := strconv.ParseInt(value, 10, 64)
		if e != nil || parsed != length {
			return "", ErrInvalid
		}
	}
	if encoding == nil {
		return "identity", nil
	}
	value := strings.ToLower(strings.Trim(encoding[0], " \t"))
	if value != "identity" && value != "gzip" {
		return "", ErrInvalid
	}
	return value, nil
}

func fieldName(name string) bool {
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}
func fieldValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == '\r' || value[i] == '\n' || value[i] == 0 || value[i] < 32 && value[i] != '\t' || value[i] == 127 {
			return false
		}
	}
	return true
}
