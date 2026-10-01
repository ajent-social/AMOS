// Package overrides composes selected, owner-authored page templates and
// namespaced theme assets around the public ui/render contract.
package overrides

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/ajent-social/amos/ui/render"
	nethtml "golang.org/x/net/html"
	"text/template/parse"
)

var (
	ErrInvalidConfig  = errors.New("invalid UI override configuration")
	ErrInvalidView    = errors.New("invalid UI override view model")
	ErrMissingSlot    = errors.New("UI override is missing a required action slot")
	ErrMissingDefault = errors.New("no default UI renderer is configured for this page")
)

const (
	HeadSlot       = "amos:head"
	SecureFormSlot = "amos:secure-form"
	ActionMarker   = `data-amos-action-contract="1"`
	maxTemplate    = 256 << 10
	maxAsset       = 5 << 20
)

var (
	namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	tokenNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	cssValuePattern  = regexp.MustCompile(`^[A-Za-z0-9#.,%() _+\-]+$`)
	assetNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	pageIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	unsafeCSSPattern = regexp.MustCompile(`(?i)(url\s*\(|@import|expression\s*\(|javascript\s*:)`)
	ownerFormPattern = regexp.MustCompile(`(?i)<form(?:[\s>])`)
	remoteScript     = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=\s*["']\s*(https?:|//|data:|javascript:)`)
	htmlComment      = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// PageOverride selects one owner-authored template for an exact shared page
// contract. Template is a path in Config.Files, read once at startup.
type PageOverride struct {
	Page     render.Page
	Template string
}

// Theme declares the owning application's namespace, token values, and local
// static assets. CSS and asset bytes are read from Config.Files at startup.
type Theme struct {
	Namespace string
	Tokens    map[string]string
	Assets    []Asset
}

// Asset is a versioned application-local image, font, or stylesheet. Path is
// its public name under the generated content-hashed namespace path; File is
// read only during New and is never used to resolve an HTTP request.
type Asset struct {
	Path        string
	File        string
	ContentType string
}

// Config is immutable after construction. ContractVersion must match the
// exported renderer version; every selected Page must declare that same version.
type Config struct {
	ContractVersion string
	Files           fs.FS
	Theme           Theme
	Pages           []PageOverride
}

// Renderer uses selected owner templates when registered and delegates every
// other page to Default. AssetHandler serves only the exact assets read at New.
type Renderer struct {
	defaultRenderer render.Renderer
	pages           map[string]*pageTemplate
	theme           themeData
	assets          map[string]assetData
}

type pageTemplate struct {
	page render.Page
	tmpl *template.Template
}

type assetData struct {
	path        string
	contentType string
	content     []byte
	integrity   string
}

type themeData struct {
	namespace string
	cssPath   string
	cssSRI    string
	assets    map[string]assetView
	styles    []assetView
}

type assetView struct {
	Path        string
	ContentType string
	Integrity   string
}

type templateData struct {
	View  render.ViewModel
	Theme themeView
}

type themeView struct {
	Namespace string
	CSSPath   string
	CSSSRI    string
	Assets    map[string]assetView
	Styles    []assetView
}

// New validates every page, protected slot, token, asset path, and asset byte
// stream before returning a renderer. No override filesystem path is opened
// after startup.
func New(defaultRenderer render.Renderer, cfg Config) (*Renderer, error) {
	if defaultRenderer == nil {
		return nil, ErrMissingDefault
	}
	if cfg.Files == nil || cfg.ContractVersion == "" || cfg.ContractVersion != render.ContractVersion || !namespacePattern.MatchString(cfg.Theme.Namespace) || len(cfg.Pages) == 0 {
		return nil, fmt.Errorf("%w: contract version, files, theme namespace, and at least one page are required", ErrInvalidConfig)
	}
	if err := validateTokens(cfg.Theme.Tokens); err != nil {
		return nil, err
	}

	r := &Renderer{
		defaultRenderer: defaultRenderer,
		pages:           make(map[string]*pageTemplate, len(cfg.Pages)),
		assets:          make(map[string]assetData, len(cfg.Theme.Assets)+1),
	}
	themeCSS := tokenStylesheet(cfg.Theme.Namespace, cfg.Theme.Tokens)
	themeAsset := assetData{contentType: "text/css; charset=utf-8", content: []byte(themeCSS)}
	themeAsset.path, themeAsset.integrity = contentAddress(cfg.Theme.Namespace, "tokens.css", themeAsset.content)
	r.assets[themeAsset.path] = themeAsset
	r.theme = themeData{namespace: cfg.Theme.Namespace, cssPath: themeAsset.path, cssSRI: themeAsset.integrity, assets: make(map[string]assetView)}

	for _, declared := range cfg.Theme.Assets {
		asset, err := loadAsset(cfg.Files, cfg.Theme.Namespace, declared)
		if err != nil {
			return nil, err
		}
		if _, exists := r.assets[asset.path]; exists {
			return nil, fmt.Errorf("%w: duplicate theme asset %q", ErrInvalidConfig, declared.Path)
		}
		r.assets[asset.path] = asset
		view := assetView{Path: asset.path, ContentType: asset.contentType, Integrity: asset.integrity}
		r.theme.assets[declared.Path] = view
		if asset.contentType == "text/css; charset=utf-8" {
			r.theme.styles = append(r.theme.styles, view)
		}
	}

	seen := make(map[string]struct{}, len(cfg.Pages))
	for _, override := range cfg.Pages {
		if !pageIDPattern.MatchString(override.Page.ID) || override.Page.Version != cfg.ContractVersion {
			return nil, fmt.Errorf("%w: page %q declares unsupported UI contract %q", ErrInvalidConfig, override.Page.ID, override.Page.Version)
		}
		if _, duplicate := seen[override.Page.ID]; duplicate {
			return nil, fmt.Errorf("%w: page %q is selected more than once", ErrInvalidConfig, override.Page.ID)
		}
		seen[override.Page.ID] = struct{}{}
		tmpl, err := loadPageTemplate(cfg.Files, override)
		if err != nil {
			return nil, err
		}
		r.pages[override.Page.ID] = &pageTemplate{page: override.Page, tmpl: tmpl}
	}
	sort.Slice(r.theme.styles, func(i, j int) bool { return r.theme.styles[i].Path < r.theme.styles[j].Path })
	return r, nil
}

// Render uses the exact registered contract version or delegates to the
// default renderer. Override models remain escaped text; forms require a
// validated local POST action and one nonempty operation CSRF token.
func (r *Renderer) Render(ctx context.Context, w http.ResponseWriter, req *http.Request, page render.Page, model render.ViewModel) error {
	if r == nil || req == nil {
		return ErrInvalidView
	}
	selected, ok := r.pages[page.ID]
	if !ok {
		if r.defaultRenderer == nil {
			return ErrMissingDefault
		}
		return r.defaultRenderer.Render(ctx, w, req, page, model)
	}
	if selected.page.Version != page.Version {
		return fmt.Errorf("%w: registered %q, requested %q", ErrInvalidConfig, selected.page.Version, page.Version)
	}
	model.Page = page
	if err := validateView(model); err != nil {
		return err
	}
	data := templateData{
		View: model,
		Theme: themeView{
			Namespace: r.theme.namespace,
			CSSPath:   r.theme.cssPath,
			CSSSRI:    r.theme.cssSRI,
			Assets:    cloneAssets(r.theme.assets),
			Styles:    append([]assetView(nil), r.theme.styles...),
		},
	}
	instance, err := selected.tmpl.Clone()
	if err != nil {
		return ErrInvalidConfig
	}
	secureForms := 0
	instance.Funcs(template.FuncMap{"amosExecutedSecureForm": func() string { secureForms++; return "" }})
	var expectedForm bytes.Buffer
	if model.Form != nil {
		if err := instance.ExecuteTemplate(&expectedForm, SecureFormSlot, data); err != nil {
			return ErrInvalidConfig
		}
		secureForms = 0
	}
	var body bytes.Buffer
	if err := instance.ExecuteTemplate(&body, pageTemplateName(page.ID), data); err != nil {
		return fmt.Errorf("render override page %q: %w", page.ID, err)
	}
	if model.Form != nil && (secureForms != 1 || !bytes.Contains(body.Bytes(), expectedForm.Bytes())) {
		return ErrMissingSlot
	}
	if model.Form != nil {
		document, err := nethtml.ParseWithOptions(bytes.NewReader(body.Bytes()), nethtml.ParseOptionEnableScripting(true))
		if err != nil || !hasActiveSecureForm(document, model.Form) {
			return ErrMissingSlot
		}
		// Serve the parsed representation used for this decision rather than
		// relying on the browser to repair different original markup.
		body.Reset()
		if err := nethtml.Render(&body, document); err != nil {
			return ErrInvalidView
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self'; font-src 'self'; style-src 'self'; script-src 'self'")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Add("Vary", "HX-Request")
	w.Header().Add("Vary", "HX-Target")
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, &body)
	return err
}

// AssetHandler serves only declared theme assets and the generated token
// stylesheet. Paths include their content digest and use immutable caching.
func (r *Renderer) AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		asset, ok := r.assets[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set("Content-Length", fmt.Sprint(len(asset.content)))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		if req.Method == http.MethodGet {
			_, _ = w.Write(asset.content)
		}
	})
}

func loadPageTemplate(files fs.FS, override PageOverride) (*template.Template, error) {
	if override.Template == "" || !fs.ValidPath(override.Template) {
		return nil, fmt.Errorf("%w: page %q has an invalid template path", ErrInvalidConfig, override.Page.ID)
	}
	content, err := fs.ReadFile(files, override.Template)
	if err != nil {
		return nil, fmt.Errorf("%w: page %q template could not be read", ErrInvalidConfig, override.Page.ID)
	}
	if len(content) == 0 || len(content) > maxTemplate || remoteScript.Match(content) || ownerFormPattern.Match(htmlComment.ReplaceAll(content, nil)) {
		return nil, fmt.Errorf("%w: page %q template is empty, too large, or declares a remote script", ErrInvalidConfig, override.Page.ID)
	}
	root := pageTemplateName(override.Page.ID)
	tmpl, err := template.New(root).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("%w: page %q template does not parse", ErrInvalidConfig, override.Page.ID)
	}
	for _, candidate := range tmpl.Templates() {
		name := candidate.Name()
		if strings.HasPrefix(name, "amos:") || (name != root && !strings.HasPrefix(name, root+":")) {
			return nil, fmt.Errorf("%w: page %q defines a reserved or foreign template slot", ErrInvalidConfig, override.Page.ID)
		}
	}
	rootTemplate := tmpl.Lookup(root)
	if rootTemplate == nil || rootTemplate.Tree == nil || rootTemplate.Tree.Root == nil {
		return nil, fmt.Errorf("%w: page %q has no root template", ErrInvalidConfig, override.Page.ID)
	}
	// Calls inside HTML comments are not active page slots. Strip comments
	// before inspecting the syntax tree so a disabled form cannot satisfy the
	// startup action-slot check.
	slotScan, err := template.New(root).Option("missingkey=error").Parse(string(htmlComment.ReplaceAll(content, nil)))
	if err != nil {
		return nil, fmt.Errorf("%w: page %q slot declarations do not parse", ErrInvalidConfig, override.Page.ID)
	}
	rootTemplate = slotScan.Lookup(root)
	calls := make(map[string]bool)
	collectTemplateCalls(rootTemplate.Tree.Root, calls)
	if !calls[HeadSlot] || !calls[SecureFormSlot] {
		return nil, fmt.Errorf("%w: page %q must include %q and %q", ErrMissingSlot, override.Page.ID, HeadSlot, SecureFormSlot)
	}
	if unsafeCSSPattern.Match(content) {
		return nil, fmt.Errorf("%w: page %q contains an active CSS or URL construct", ErrInvalidConfig, override.Page.ID)
	}
	if _, err := tmpl.New(HeadSlot).Parse(`{{define "` + HeadSlot + `"}}<link rel="stylesheet" href="{{.Theme.CSSPath}}" integrity="{{.Theme.CSSSRI}}" crossorigin="anonymous">{{range .Theme.Styles}}<link rel="stylesheet" href="{{.Path}}" integrity="{{.Integrity}}" crossorigin="anonymous">{{end}}{{end}}`); err != nil {
		return nil, fmt.Errorf("%w: installing protected theme slot", ErrInvalidConfig)
	}
	tmpl.Funcs(template.FuncMap{"amosExecutedSecureForm": func() string { return "" }})
	if _, err := tmpl.New(SecureFormSlot).Parse(`{{define "` + SecureFormSlot + `"}}{{with .View.Form}}{{amosExecutedSecureForm}}<form ` + ActionMarker + ` action="{{.Action}}" method="{{.Method}}"><input type="hidden" name="_csrf" value="{{.CSRFToken}}"><label for="{{.Field}}">{{.Label}}</label>{{range $.View.FieldErrors}}<span class="field-error">{{.Message}}</span>{{end}}<input id="{{.Field}}" name="{{.Field}}" value="{{.Value}}"><button type="submit">{{.Submit}}</button></form>{{end}}{{end}}`); err != nil {
		return nil, fmt.Errorf("%w: installing protected action slot", ErrInvalidConfig)
	}
	return tmpl, nil
}

func collectTemplateCalls(node parse.Node, calls map[string]bool) {
	if node == nil {
		return
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			collectTemplateCalls(child, calls)
		}
	case *parse.TemplateNode:
		calls[current.Name] = true
		// Conditional branches cannot establish a required slot: execution depends
		// on owner-controlled data, and an empty range executes no body at all.

	}
}

func validateTokens(tokens map[string]string) error {
	for name, value := range tokens {
		if !tokenNamePattern.MatchString(name) || strings.TrimSpace(value) != value || value == "" || !cssValuePattern.MatchString(value) {
			return fmt.Errorf("%w: invalid namespaced CSS token %q", ErrInvalidConfig, name)
		}
	}
	return nil
}

func tokenStylesheet(namespace string, tokens map[string]string) string {
	keys := make([]string, 0, len(tokens))
	for name := range tokens {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var css strings.Builder
	css.WriteString(":root{")
	for _, name := range keys {
		fmt.Fprintf(&css, "--amos-%s-%s:%s;", namespace, name, tokens[name])
	}
	css.WriteString("}\n")
	return css.String()
}

func loadAsset(files fs.FS, namespace string, declaration Asset) (assetData, error) {
	if !assetNamePattern.MatchString(declaration.Path) || strings.Contains(declaration.Path, "..") || declaration.File == "" || !fs.ValidPath(declaration.File) {
		return assetData{}, fmt.Errorf("%w: invalid theme asset declaration", ErrInvalidConfig)
	}
	if !allowedAssetType(declaration.ContentType) {
		return assetData{}, fmt.Errorf("%w: unsupported theme asset content type %q", ErrInvalidConfig, declaration.ContentType)
	}
	content, err := fs.ReadFile(files, declaration.File)
	if err != nil || len(content) == 0 || len(content) > maxAsset {
		return assetData{}, fmt.Errorf("%w: theme asset %q is unavailable or too large", ErrInvalidConfig, declaration.Path)
	}
	if declaration.ContentType == "text/css" && unsafeCSSPattern.Match(content) {
		return assetData{}, fmt.Errorf("%w: theme stylesheet contains a remote or active URL construct", ErrInvalidConfig)
	}
	assetPath, integrity := contentAddress(namespace, declaration.Path, content)
	contentType := declaration.ContentType
	if contentType == "text/css" {
		contentType += "; charset=utf-8"
	}
	return assetData{path: assetPath, contentType: contentType, content: append([]byte(nil), content...), integrity: integrity}, nil
}

func allowedAssetType(contentType string) bool {
	switch contentType {
	case "text/css", "image/png", "image/jpeg", "image/webp", "image/avif", "font/woff", "font/woff2", "font/ttf":
		return true
	default:
		return false
	}
}

func contentAddress(namespace, name string, content []byte) (string, string) {
	digest := sha256.Sum256(content)
	encoded := hex.EncodeToString(digest[:])
	pathName := path.Base(name)
	return "/assets/themes/" + namespace + "/" + encoded[:20] + "/" + pathName, "sha256-" + base64.StdEncoding.EncodeToString(digest[:])
}

func pageTemplateName(id string) string { return "owner:" + id }

func validateView(model render.ViewModel) error {
	if model.Title == "" || model.Heading == "" {
		return ErrInvalidView
	}
	for _, link := range model.Navigation {
		if link.Label == "" || !safeLocalPath(link.Path) {
			return ErrInvalidView
		}
	}
	for _, action := range model.Actions {
		if action.Name == "" || !safeLocalPath(action.Path) {
			return ErrInvalidView
		}
	}
	for _, fieldError := range model.FieldErrors {
		if fieldError.Field == "" || fieldError.Message == "" {
			return ErrInvalidView
		}
	}
	for _, flash := range model.Flash {
		if flash.Kind != "status" && flash.Kind != "error" {
			return ErrInvalidView
		}
	}
	if model.Form != nil && (!safeLocalPath(model.Form.Action) || model.Form.Method != http.MethodPost || model.Form.Label == "" || model.Form.Field == "" || model.Form.Submit == "" || model.Form.CSRFToken == "") {
		return ErrInvalidView
	}
	return nil
}

func safeLocalPath(value string) bool {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n\x00") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/") {
		return false
	}
	decoded := parsed.Path
	for range 2 {
		next, err := url.PathUnescape(decoded)
		if err != nil || strings.ContainsAny(next, "\\\r\n\x00") || strings.HasPrefix(next, "//") {
			return false
		}
		for _, segment := range strings.Split(next, "/") {
			if segment == "." || segment == ".." {
				return false
			}
		}
		if next == decoded {
			break
		}
		decoded = next
	}
	return true
}

func cloneAssets(source map[string]assetView) map[string]assetView {
	result := make(map[string]assetView, len(source))
	for name, asset := range source {
		result[name] = asset
	}
	return result
}

var _ render.Renderer = (*Renderer)(nil)

func uniqueHTMLAttribute(node *nethtml.Node, key string) (string, bool) {
	value, count := "", 0
	for _, attribute := range node.Attr {
		if attribute.Namespace == "" && attribute.Key == key {
			value = attribute.Val
			count++
		}
	}
	return value, count == 1
}

func hasActiveSecureForm(document *nethtml.Node, form *render.Form) bool {
	if document == nil || form == nil {
		return false
	}
	forms, valid := 0, false
	var visit func(*nethtml.Node)
	visit = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && (node.Data == "template" || node.Namespace != "") {
			return
		}
		if node.Type == nethtml.ElementNode && node.Data == "form" {
			forms++
			marker, m := uniqueHTMLAttribute(node, "data-amos-action-contract")
			action, a := uniqueHTMLAttribute(node, "action")
			method, p := uniqueHTMLAttribute(node, "method")
			tokens, correct := 0, false
			var inputs func(*nethtml.Node)
			inputs = func(child *nethtml.Node) {
				if child.Type == nethtml.ElementNode && (child.Data == "template" || child.Namespace != "") {
					return
				}
				if child.Type == nethtml.ElementNode && child.Data == "input" {
					name, n := uniqueHTMLAttribute(child, "name")
					if name == "_csrf" {
						tokens++
						value, v := uniqueHTMLAttribute(child, "value")
						kind, k := uniqueHTMLAttribute(child, "type")
						correct = n && v && k && kind == "hidden" && value == form.CSRFToken
					}
				}
				for child := child.FirstChild; child != nil; child = child.NextSibling {
					inputs(child)
				}
			}
			inputs(node)
			valid = m && marker == "1" && a && action == form.Action && p && strings.EqualFold(method, http.MethodPost) && tokens == 1 && correct
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return forms == 1 && valid
}
