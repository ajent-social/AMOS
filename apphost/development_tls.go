package apphost

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ajent-social/amos/app"
	delivery "github.com/ajent-social/amos/delivery/email"
)

// DevelopmentTLSConfig is an explicit owner-operated development profile, not
// production deployment or generated amos dev LAN support. PEM inputs are copied
// and parsed before dependencies. No caller signer, TLS policy or callback escapes.
type DevelopmentTLSConfig struct {
	Local                         LocalConfig
	CertificatePEM, PrivateKeyPEM []byte
}
type developmentTLSIdentity struct {
	certificate tls.Certificate
	leaf        *x509.Certificate
}

func NewDevelopmentTLS(ctx context.Context, cfg DevelopmentTLSConfig) (*Host, error) {
	origin, err := delivery.ParseDevelopmentTLSOrigin(cfg.Local.Origin)
	if ctx == nil || err != nil || cfg.Local.Billing != nil {
		return nil, ErrLocalConfiguration
	}
	identity, err := parseDevelopmentTLSIdentity(cfg.CertificatePEM, cfg.PrivateKeyPEM, origin)
	if err != nil {
		return nil, ErrLocalConfiguration
	}
	return newHost(ctx, cfg.Local, identity)
}
func parseDevelopmentTLSIdentity(certificate, key []byte, origin *url.URL) (*developmentTLSIdentity, error) {
	if len(certificate) == 0 || len(certificate) > 64<<10 || len(key) == 0 || len(key) > 64<<10 {
		return nil, ErrLocalConfiguration
	}
	certificate = bytes.Clone(certificate)
	key = bytes.Clone(key)
	if !bytes.HasPrefix(bytes.TrimSpace(certificate), []byte("-----BEGIN CERTIFICATE-----")) || bytes.Count(certificate, []byte("-----BEGIN ")) != 1 || !bytes.HasPrefix(bytes.TrimSpace(key), []byte("-----BEGIN ")) || bytes.Count(key, []byte("-----BEGIN ")) != 1 {
		return nil, ErrLocalConfiguration
	}
	block, rest := pem.Decode(certificate)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrLocalConfiguration
	}
	keyBlock, rest := pem.Decode(key)
	if keyBlock == nil || len(keyBlock.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrLocalConfiguration
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil || len(pair.Certificate) != 1 {
		return nil, ErrLocalConfiguration
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, ErrLocalConfiguration
	}
	explicitServerAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			explicitServerAuth = true
		}
	}
	ip := net.ParseIP(origin.Hostname())
	san := false
	for _, candidate := range leaf.IPAddresses {
		if candidate.Equal(ip) {
			san = true
		}
	}
	if !explicitServerAuth || !san || leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return nil, ErrLocalConfiguration
	}
	pair.Leaf = leaf
	identity := &developmentTLSIdentity{certificate: pair, leaf: leaf}
	if !identity.valid(time.Now()) {
		return nil, ErrLocalConfiguration
	}
	return identity, nil
}
func (i *developmentTLSIdentity) valid(now time.Time) bool {
	return i != nil && i.leaf != nil && !now.Before(i.leaf.NotBefore) && now.Before(i.leaf.NotAfter)
}
func (h *Host) serveDevelopmentTLS(ctx context.Context, listener net.Listener) error {
	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		return ErrLocalConfiguration
	}
	address, ok := tcp.Addr().(*net.TCPAddr)
	port, err := strconv.Atoi(h.origin.Port())
	if !ok || err != nil || address.Port != port || !address.IP.Equal(net.ParseIP(h.origin.Hostname())) || !h.tlsIdentity.valid(time.Now()) {
		return ErrLocalConfiguration
	}
	h.serveMu.Lock()
	if h.tlsServing {
		h.serveMu.Unlock()
		return app.ErrAlreadyServing
	}
	h.tlsServing = true
	h.serveMu.Unlock()
	defer func() { h.serveMu.Lock(); h.tlsServing = false; h.serveMu.Unlock() }()
	return h.serveWithWorker(ctx, func(workCtx context.Context) error {
		identity := h.tlsIdentity
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if !identity.valid(time.Now()) {
				return nil, ErrLocalConfiguration
			}
			return &identity.certificate, nil
		}}
		server := &http.Server{Handler: h.developmentTLSGuard(h.app.Handler()), TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
		done := make(chan error, 1)
		go func() { done <- server.ServeTLS(tcp, "", "") }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-workCtx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			shutdownErr := server.Shutdown(shutdownCtx)
			if shutdownErr != nil {
				_ = server.Close()
			}
			serveErr := <-done
			if errors.Is(serveErr, http.ErrServerClosed) {
				serveErr = nil
			}
			return errors.Join(shutdownErr, serveErr)
		}
	})
}
func (h *Host) developmentTLSGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || r.Host != h.origin.Host {
			http.Error(w, "request unavailable", http.StatusBadRequest)
			return
		}
		if !h.tlsIdentity.valid(time.Now()) {
			http.Error(w, "request unavailable", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}
