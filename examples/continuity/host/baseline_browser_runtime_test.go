package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// browserEvidence contains only finite counters/status, never DOM, URLs,
// credentials, headers or browser diagnostics.
type baselineBrowserEvidence struct {
	Phase       int  `json:"phase"`
	OK          bool `json:"ok"`
	Pages       int  `json:"pages"`
	Navigations int  `json:"navigations"`
	Layouts     int  `json:"layouts"`
	Keyboard    int  `json:"keyboard"`
	Forms       int  `json:"forms"`
	Denied      int  `json:"denied"`
	External    int  `json:"external"`
	Mutations   int  `json:"mutations"`
}

type baselineBrowserOutput struct{ buffer bytes.Buffer }

func (b *baselineBrowserOutput) Write(p []byte) (int, error) {
	if len(p) > 4096-b.buffer.Len() {
		return 0, io.ErrShortWrite
	}
	return b.buffer.Write(p)
}

// Explicit local Node/Playwright/Chromium prerequisites are operator-qualified.
// This test never installs tools. It exercises synthetic native GET navigation,
// not public-host admission, POST/CSRF verification or an enrollment journey.
func TestPrivateBaselineBrowserRuntimeRequiredService(t *testing.T) {
	localPath := func(key string, executable bool) string {
		t.Helper()
		path := os.Getenv(key)
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || (executable && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0)) {
			t.Fatalf("required explicit local browser prerequisite %s absent or invalid", key)
		}
		return path
	}
	node := localPath("AMOS_CONTINUITY_BROWSER_NODE", true)
	playwright := localPath("AMOS_CONTINUITY_BROWSER_PLAYWRIGHT", false)
	chromium := localPath("AMOS_CONTINUITY_BROWSER_CHROMIUM", true)
	f := privateBaselineFixture(t)
	if f == nil {
		return
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "assets", "base.css"))
	if err != nil || len(css) == 0 || len(css) > 128*1024 {
		t.Fatal("required exact application stylesheet unavailable")
	}
	baseline, source := f.core.baselineHandler(), f.core.sourceHandler()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/assets/base.css" && r.URL.RawQuery == "" && r.URL.RawPath == "" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", strconv.Itoa(len(css)))
			if r.Method != http.MethodHead {
				_, _ = w.Write(css)
			}
			return
		}
		if r.URL.Path == sourcePath || strings.HasPrefix(r.URL.Path, sourcePath+"/") {
			source.ServeHTTP(w, r)
			return
		}
		baseline.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	address, ok := server.Listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() {
		t.Fatal("browser fixture listener must be loopback only")
	}
	server.Config.ReadHeaderTimeout = 2 * time.Second
	server.Config.ReadTimeout = 5 * time.Second
	server.Config.WriteTimeout = 5 * time.Second
	server.Config.IdleTimeout = 5 * time.Second
	server.Config.MaxHeaderBytes = 8192
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()

	// Stdin is the sole credential transport. No command argument, environment
	// entry, screenshot, trace, page dump or evidence file receives the cookie.
	input := struct {
		Origin        string `json:"origin"`
		SessionCookie string `json:"sessionCookie"`
		Playwright    string `json:"playwright"`
		Chromium      string `json:"chromium"`
		Property      string `json:"property"`
		Case          string `json:"case"`
		Application   string `json:"application"`
		Procedure     string `json:"procedure"`
		Source        string `json:"source"`
	}{server.URL, f.token, playwright, chromium, f.own.property.String(), f.own.caseID.String(), f.own.application.String(), f.own.procedure.String(), f.own.source.String()}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal("browser private input encoding failed")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("testdata", "baseline-browser.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	// No inherited runtime configuration, Node preload or browser profile. The
	// disposable browser profile and all temporary files stay under the test's TMPDIR.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "TMPDIR=" + t.TempDir()}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	var output baselineBrowserOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	var evidence baselineBrowserEvidence
	decoder := json.NewDecoder(bytes.NewReader(output.buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		t.Fatal("browser evidence invalid")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("browser evidence has trailing data")
	}
	for _, count := range []int{evidence.Pages, evidence.Navigations, evidence.Layouts, evidence.Keyboard, evidence.Forms, evidence.Denied, evidence.External, evidence.Mutations} {
		if count < 0 || count > 1000 {
			t.Fatal("browser evidence count invalid")
		}
	}
	if evidence.Phase < 1 || evidence.Phase > 13 {
		t.Fatal("browser evidence phase invalid")
	}
	if runErr != nil || !evidence.OK {
		t.Fatalf("private browser failed: phase=%d pages=%d layouts=%d keyboard=%d forms=%d denied=%d external=%d mutations=%d", evidence.Phase, evidence.Pages, evidence.Layouts, evidence.Keyboard, evidence.Forms, evidence.Denied, evidence.External, evidence.Mutations)
	}
	if evidence.Phase != 13 || evidence.Pages != 19 || evidence.Navigations != 19 || evidence.Layouts != 38 || evidence.Keyboard != 4 || evidence.Forms != 4 || evidence.Denied != 1 || evidence.External != 0 || evidence.Mutations != 0 {
		t.Fatal("browser evidence incomplete or outside the read-only boundary")
	}
	t.Logf("private browser GET checks: pages=%d navigations=%d layouts=%d keyboard=%d forms=%d denied=%d external=%d mutations=%d", evidence.Pages, evidence.Navigations, evidence.Layouts, evidence.Keyboard, evidence.Forms, evidence.Denied, evidence.External, evidence.Mutations)
}
