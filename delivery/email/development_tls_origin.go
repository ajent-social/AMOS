package email

import (
	"net/netip"
	"net/url"
	"strconv"
)

// ParseDevelopmentTLSOrigin accepts only canonical, explicit HTTPS private IPv4
// development origins. It performs no DNS lookup and does not widen other profiles.
func ParseDevelopmentTLSOrigin(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return nil, ErrSetupRequired
	}
	address, err := netip.ParseAddr(u.Hostname())
	allowedAddress := address.IsPrivate() || address.String() == "127.0.0.1"
	if err != nil || !address.Is4() || address.String() != u.Hostname() || !allowedAddress {
		return nil, ErrSetupRequired
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 || strconv.FormatUint(port, 10) != u.Port() || u.Host != address.String()+":"+u.Port() || u.String() != value {
		return nil, ErrSetupRequired
	}
	return u, nil
}
