package backup

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/credentials/endpointcreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/smithy-go/logging"
)

var runtimeRelativePath = regexp.MustCompile(`^/v2/credentials/[A-Za-z0-9-]{1,128}$`)

func rejectRuntimeEnvironment(kind string) error {
	forbidden := map[string]bool{}
	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY", "AWS_SECRET_KEY", "AWS_SESSION_TOKEN", "AWS_SECURITY_TOKEN", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_CONFIG_FILE", "AWS_ROLE_ARN", "AWS_ROLE_SESSION_NAME", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_AUTHORIZATION_TOKEN", "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", "AWS_EC2_METADATA_DISABLED", "AWS_EC2_METADATA_SERVICE_ENDPOINT", "AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE", "AWS_EC2_METADATA_V1_DISABLED", "AWS_CA_BUNDLE", "AWS_ENDPOINT_URL", "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "AWS_USE_FIPS_ENDPOINT", "AWS_USE_DUALSTACK_ENDPOINT", "AWS_STS_REGIONAL_ENDPOINTS", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		forbidden[name] = true
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if forbidden[name] || strings.HasPrefix(name, "AWS_ENDPOINT_URL_") || (name == "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI" && kind != "ecs_task") {
			return ErrRuntimeStore
		}
	}
	if kind == "ecs_task" && !runtimeRelativePath.MatchString(os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI")) {
		return ErrRuntimeStore
	}
	return nil
}
func runtimeMetadataSource(kind string) (aws.CredentialsProvider, error) {
	client, err := runtimeHTTPClient(2*time.Second, true)
	if err != nil {
		return nil, ErrRuntimeStore
	}
	switch kind {
	case "imds_v2":
		metadata := imds.New(imds.Options{Endpoint: "http://" + net.IPv4(169, 254, 169, 254).String(), EndpointMode: imds.EndpointModeStateIPv4, HTTPClient: client, Retryer: aws.NopRetryer{}, ClientEnableState: imds.ClientEnabled, EnableFallback: aws.FalseTernary, Logger: logging.Nop{}})
		return ec2rolecreds.New(func(options *ec2rolecreds.Options) { options.Client = metadata }), nil
	case "ecs_task":
		relative := os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI")
		if !runtimeRelativePath.MatchString(relative) {
			return nil, ErrRuntimeStore
		}
		return endpointcreds.New("http://"+net.IPv4(169, 254, 170, 2).String()+relative, func(options *endpointcreds.Options) { options.HTTPClient = client; options.Retryer = aws.NopRetryer{} }), nil
	default:
		return nil, ErrRuntimeStore
	}
}
func runtimeHTTPClient(timeout time.Duration, metadata bool) (*http.Client, error) {
	// Reject process trust overrides before obtaining a private root snapshot.
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if _, ok := os.LookupEnv(name); ok {
			return nil, ErrRuntimeStore
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		return nil, ErrRuntimeStore
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: -1}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots.Clone()}, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false}
	var responseTimeout time.Duration
	if metadata {
		responseTimeout = 2 * time.Second
	}
	var roundTripper http.RoundTripper = boundedMetadataTransport{transport: transport, timeout: responseTimeout}
	return &http.Client{Transport: roundTripper, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type boundedMetadataTransport struct {
	transport http.RoundTripper
	timeout   time.Duration
}

func (t boundedMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, ErrRuntimeStore
	}
	ctx := request.Context()
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}
	response, err := t.transport.RoundTrip(request.Clone(ctx))
	if err != nil {
		return nil, ErrRuntimeStore
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > 64<<10 {
		return nil, ErrRuntimeStore
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}
