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

// web/static/lib (HTMX, Sora) is named so the build fails until `mise run
// assets` has copied it from npm.
//
//go:embed web web/static/lib
var webFS embed.FS

var pageTmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"iconURL": iconURL,
	"initial": func(s string) string {
		r, _ := utf8.DecodeRuneInString(s)
		return strings.ToUpper(string(r))
	},
}).ParseFS(webFS, "web/*.html"))

func (a *app) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web") // can't fail: web is embedded
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.page)
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("GET /search", a.search)
	mux.HandleFunc("GET /search/thumb/{key}/{id}", a.searchThumb)
	mux.HandleFunc("GET /background", a.backgroundPhoto)
	mux.HandleFunc("GET /keys", a.keysPage)
	mux.Handle("POST /keys", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.saveKey)))
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
		Name          string
		Sections      []config.Section
		Searchable    bool   // apps can be searched
		PersonalKeys  bool   // some can be searched with the user's own key
		Background    string // the mode, see background.go
		PhotoInterval int    // seconds between background photos; 0 without photos
	}{s.Name, a.cfg.Sections(s.Groups), len(a.searchable(s.Groups)) > 0, len(a.keyApps(s, nil)) > 0, a.bg.mode, a.bg.photoInterval()}
	if err := pageTmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("render: %v", err)
	}
}
