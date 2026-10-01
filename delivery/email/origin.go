package email

import (
	"net"
	"net/url"
	"strings"
)

// ParseApplicationOrigin permits HTTP only under an explicit local development
// grant, for canonical loopback hosts. Remote origins always require HTTPS.
func ParseApplicationOrigin(value string, developmentLoopback bool) (*url.URL, error) {
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.ContainsAny(value, "\r\n\t ") {
		return nil, ErrSetupRequired
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if developmentLoopback && (!loopback || (ip != nil && !ip.IsLoopback())) {
		return nil, ErrSetupRequired
	}
	if u.Scheme != "https" && (!developmentLoopback || u.Scheme != "http" || !loopback) {
		return nil, ErrSetupRequired
	}
	if u.Port() != "" {
		if port, err := net.LookupPort("tcp", u.Port()); err != nil || port < 1 || port > 65535 {
			return nil, ErrSetupRequired
		}
	}
	u.Path = ""
	return u, nil
}
