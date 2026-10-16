package authpassword

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidTheme means presentation configuration cannot be served safely.
var ErrInvalidTheme = errors.New("invalid native auth theme")

// Theme supplies owner-authored presentation for native password pages. Assets
// must be canonical application-local paths served by the owning application.
// New copies this value; request input must never select the theme.
type Theme struct {
	BrandName     string
	LogoURL       string
	StylesheetURL string
}

// Validate checks presentation data without opening dependencies or fetching
// assets. Constructors also validate and copy any configured theme.
func (t Theme) Validate() error {
	if !utf8.ValidString(t.BrandName) || strings.TrimSpace(t.BrandName) == "" || utf8.RuneCountInString(t.BrandName) > 100 {
		return ErrInvalidTheme
	}
	for _, r := range t.BrandName {
		if unicode.IsControl(r) {
			return ErrInvalidTheme
		}
	}
	for _, asset := range []string{t.LogoURL, t.StylesheetURL} {
		if asset != "" && !canonicalAssetPath(asset) {
			return ErrInvalidTheme
		}
	}
	return nil
}

func canonicalAssetPath(value string) bool {
	if !strings.HasPrefix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
				return false
			}
		}
	}
	return true
}
