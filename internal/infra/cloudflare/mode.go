package cloudflare

import (
	"errors"
	"fmt"
	"strings"
)

// HostingProfile identifies an AMOS deployment profile whose DNS is owned by
// the selected Cloudflare zone.
type HostingProfile string

const (
	ProfileAWSManaged HostingProfile = "aws_managed"
	ProfileAWSVM      HostingProfile = "aws_vm"
)

// ProxyMode describes whether Cloudflare proxies application traffic.
type ProxyMode string

const (
	ProxyDNSOnly ProxyMode = "dns_only"
	ProxyEnabled ProxyMode = "proxied"
)

var ErrProxyUnsupported = errors.New("cloudflare proxy mode is unsupported; use explicit DNS-only mode")

// Mode is the explicit, DNS-only Cloudflare configuration for one application
// hostname. ZoneID is the provider identifier; ZoneName is the DNS suffix that
// establishes which names this application may manage.
type Mode struct {
	Profile  HostingProfile
	ZoneID   string
	ZoneName string
	Hostname string
	Proxy    ProxyMode
}

// Capabilities reports only protections enabled by this deployment mode.
type Capabilities struct {
	DNS bool
	CDN bool
	WAF bool
}

// AliasRequest is the exact record scope a DNS alias adapter may submit.
type AliasRequest struct {
	ZoneID   string
	Hostname string
	Proxy    ProxyMode
}

// AliasOptions is an immutable, validated DNS-only input for an upstream
// alias adapter. Its private fields prevent callers from enabling proxying or
// changing the selected zone and hostname after validation.
type AliasOptions struct {
	zoneID   string
	hostname string
}

// ZoneID returns the selected DNS zone identifier.
func (o AliasOptions) ZoneID() string { return o.zoneID }

// Hostname returns the selected application hostname in canonical form.
func (o AliasOptions) Hostname() string { return o.hostname }

// Proxied reports whether the alias adapter may proxy application traffic.
// DNS-only options always return false.
func (o AliasOptions) Proxied() bool { return false }

// Validate rejects a zero value or options that do not contain a canonical
// zone identifier and hostname.
func (o AliasOptions) Validate() error {
	if strings.TrimSpace(o.zoneID) != o.zoneID {
		return errors.New("alias zone ID must not contain surrounding whitespace")
	}
	if o.zoneID == "" {
		return errors.New("alias zone ID is required")
	}
	host, err := normalizeHostname(o.hostname)
	if err != nil {
		return fmt.Errorf("invalid alias hostname: %w", err)
	}
	if host != o.hostname {
		return errors.New("alias hostname must be canonical")
	}
	return nil
}

// Validate rejects incomplete, out-of-zone, or proxied configurations.
func (m Mode) Validate() error {
	if m.Profile != ProfileAWSManaged && m.Profile != ProfileAWSVM {
		return fmt.Errorf("unsupported Cloudflare hosting profile %q", m.Profile)
	}
	if strings.TrimSpace(m.ZoneID) != m.ZoneID {
		return errors.New("cloudflare zone ID must not contain surrounding whitespace")
	}
	if m.ZoneID == "" {
		return errors.New("cloudflare zone ID is required")
	}
	zone, err := normalizeHostname(m.ZoneName)
	if err != nil {
		return fmt.Errorf("invalid Cloudflare zone name: %w", err)
	}
	host, err := normalizeHostname(m.Hostname)
	if err != nil {
		return fmt.Errorf("invalid application hostname: %w", err)
	}
	if host != zone && !strings.HasSuffix(host, "."+zone) {
		return fmt.Errorf("application hostname %q is outside selected Cloudflare zone %q", host, zone)
	}
	switch m.Proxy {
	case ProxyDNSOnly:
		return nil
	case ProxyEnabled:
		return ErrProxyUnsupported
	default:
		return fmt.Errorf("cloudflare proxy mode must be explicitly %q", ProxyDNSOnly)
	}
}

// Capabilities returns the protections provided by the only supported mode.
func (m Mode) Capabilities() Capabilities {
	return Capabilities{DNS: true}
}

// AliasOptions validates the selected zone, exact application hostname, and
// DNS-only mode before producing input for an upstream DNS alias adapter.
func (m Mode) AliasOptions(request AliasRequest) (AliasOptions, error) {
	if err := m.Validate(); err != nil {
		return AliasOptions{}, err
	}
	if request.Proxy == ProxyEnabled {
		return AliasOptions{}, ErrProxyUnsupported
	}
	if request.Proxy != ProxyDNSOnly {
		return AliasOptions{}, fmt.Errorf("alias proxy mode must be explicitly %q", ProxyDNSOnly)
	}
	if request.ZoneID != m.ZoneID {
		return AliasOptions{}, errors.New("alias zone does not match selected Cloudflare zone")
	}
	name, err := normalizeHostname(request.Hostname)
	if err != nil {
		return AliasOptions{}, fmt.Errorf("invalid alias hostname: %w", err)
	}
	selected, _ := normalizeHostname(m.Hostname)
	if name != selected {
		return AliasOptions{}, errors.New("alias hostname does not match selected application hostname")
	}
	return AliasOptions{zoneID: m.ZoneID, hostname: selected}, nil
}

func normalizeHostname(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || len(host) > 253 {
		return "", errors.New("hostname is empty or too long")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == 45 || label[len(label)-1] == 45 {
			return "", fmt.Errorf("invalid DNS label %q", label)
		}
		for _, r := range label {
			validLetter := r >= 'a' && r <= 'z'
			validDigit := r >= '0' && r <= '9'
			if !validLetter && !validDigit && r != '-' {
				return "", fmt.Errorf("invalid character in DNS label %q", label)
			}
		}
	}
	return host, nil
}
