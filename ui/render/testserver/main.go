// Command testserver serves the real renderer to the browser shell spec.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ajent-social/amos/ui/render"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4176"
	}
	r, err := render.NewDefault()
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ready\n")) })
	mux.HandleFunc("GET /assets/base.css", func(w http.ResponseWriter, req *http.Request) {
		root, err := os.Getwd()
		if err != nil {
			http.Error(w, "unavailable", 500)
			return
		}
		path := filepath.Join(root, "ui", "assets", "base.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeFile(w, req, path)
	})
	handler := func(w http.ResponseWriter, req *http.Request) {
		status := ""
		if req.Method == http.MethodPost {
			if err := req.ParseForm(); err != nil {
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			status = "Profile saved"
		}
		name := req.URL.Query().Get("name")
		if req.Method == http.MethodPost {
			name = req.Form.Get("name")
		}
		if name == "" {
			name = "Ada Lovelace"
		}
		view := render.ViewModel{
			Title: "Profile", Heading: "Your profile", ProfileName: name, RequestID: "test-request",
			Navigation: []render.Link{{Label: "Profile", Path: "/ui"}},
			FieldErrors: func() []render.FieldError {
				if req.URL.Query().Get("error") == "true" {
					return []render.FieldError{{Field: "name", Message: "Enter a display name"}}
				}
				return nil
			}(),
			Form:   &render.Form{Action: "/ui", Method: http.MethodPost, Label: "Display name", Field: "name", Value: name, Submit: "Save profile", CSRFToken: "synthetic-operation-token"},
			Status: status,
		}
		if err := r.Render(context.Background(), w, req, render.Page{ID: "profile", Version: render.ContractVersion}, view); err != nil {
			http.Error(w, "render unavailable", http.StatusInternalServerError)
		}
	}
	mux.HandleFunc("GET /ui", handler)
	mux.HandleFunc("POST /ui", handler)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, req *http.Request) { http.Redirect(w, req, "/ui", http.StatusSeeOther) })
	addr := "127.0.0.1:" + port
	log.Printf("test renderer listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
