// Example ui-overrides demonstrates composing selected owner templates with
// the shared server-rendered UI. It is not an authentication or checkout flow.
package main

import (
	"embed"
	"log"
	"net/http"
	"os"

	"github.com/ajent-social/amos/ui/overrides"
	"github.com/ajent-social/amos/ui/render"
)

//go:embed templates/* assets/*
var content embed.FS

func main() {
	handler, err := NewHandler()
	if err != nil {
		log.Fatal(err)
	}
	addr := ":4177"
	if value := os.Getenv("AMOS_UI_OVERRIDES_ADDR"); value != "" {
		addr = value
	}
	log.Fatal(http.ListenAndServe(addr, handler))
}

// NewHandler exposes the public composition seam for generated consumers.
func NewHandler() (http.Handler, error) {
	base, err := render.NewDefault()
	if err != nil {
		return nil, err
	}
	owner, err := overrides.New(base, overrides.Config{
		ContractVersion: render.ContractVersion,
		Files:           content,
		Theme:           overrides.Theme{Namespace: "northstar", Tokens: map[string]string{"accent": "#3155d9", "surface": "#f4f6ff", "text": "#17213c"}, Assets: []overrides.Asset{{Path: "brand.css", File: "assets/brand.css", ContentType: "text/css"}}},
		Pages: []overrides.PageOverride{
			{Page: render.Page{ID: "signin", Version: render.ContractVersion}, Template: "templates/signin.html"},
			{Page: render.Page{ID: "checkout", Version: render.ContractVersion}, Template: "templates/checkout.html"},
			{Page: render.Page{ID: "profile", Version: render.ContractVersion}, Template: "templates/profile.html"},
		},
	})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/assets/themes/", owner.AssetHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/signin", func(w http.ResponseWriter, r *http.Request) {
		page(owner, w, r, "signin", "Northstar sign in", "Sign in to Northstar", &render.Form{Action: "/signin", Method: http.MethodPost, Label: "Email", Field: "email", Submit: "Continue", CSRFToken: "example-test-token"})
	})
	mux.HandleFunc("/checkout", func(w http.ResponseWriter, r *http.Request) {
		page(owner, w, r, "checkout", "Northstar checkout", "Review your order", &render.Form{Action: "/checkout", Method: http.MethodPost, Label: "Address", Field: "address", Submit: "Continue", CSRFToken: "example-test-token"})
	})
	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "Guest"
		}
		page(owner, w, r, "profile", "Profile", "Profile for "+name, nil)
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		page(owner, w, r, "dashboard", "Dashboard", "Default dashboard", nil)
	})
	return mux, nil
}

func page(renderer render.Renderer, w http.ResponseWriter, r *http.Request, id, title, heading string, form *render.Form) {
	model := render.ViewModel{Title: title, Heading: heading, Form: form}
	if err := renderer.Render(r.Context(), w, r, render.Page{ID: id, Version: render.ContractVersion}, model); err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
	}
}
