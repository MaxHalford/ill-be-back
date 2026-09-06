package app

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed policies/*.html
var policyFiles embed.FS
var policyTemplates = template.Must(template.ParseFS(policyFiles, "policies/*.html"))

func (a *Server) policy(w http.ResponseWriter, r *http.Request) {
	title := "Privacy policy"
	if r.URL.Path == "/terms" {
		title = "Terms of use"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_ = policyTemplates.ExecuteTemplate(w, "page", struct{ Title, Page string }{title, r.URL.Path})
}
