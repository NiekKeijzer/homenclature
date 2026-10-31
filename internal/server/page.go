package server

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

//go:embed web
var webFS embed.FS

var pageTmpl = template.Must(template.New("index.html").Funcs(template.FuncMap{
	"iconURL": iconURL,
	"initial": func(s string) string {
		r, _ := utf8.DecodeRuneInString(s)
		return strings.ToUpper(string(r))
	},
}).ParseFS(webFS, "web/index.html"))

func (a *app) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web") // can't fail: web is embedded
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.page)
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.Handle("GET /icons/", a.icons)
	mux.Handle("GET /static/", http.FileServerFS(static))
	return mux
}

func (a *app) page(w http.ResponseWriter, r *http.Request) {
	var s session
	if !a.readCookie(r, sessionCookie, &s) {
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return
	}
	data := struct {
		Name     string
		Sections []config.Section
	}{s.Name, a.cfg.Sections(s.Groups)}
	if err := pageTmpl.Execute(w, data); err != nil {
		log.Printf("render: %v", err)
	}
}
