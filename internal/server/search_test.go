package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newSearchApp returns an app where parents can search a working fake
// Paperless and a broken Jellyfin; kids can't search anything.
func newSearchApp(t *testing.T) (a *app, paperlessHits *atomic.Int32) {
	paperlessHits = &atomic.Int32{}
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paperlessHits.Add(1)
		switch r.URL.Path {
		case "/api/documents/":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"count": 3, "results": [{"id": 7, "title": "Energy bill", "created": "2024-05-01"}]}`)
		case "/api/documents/7/thumb/":
			w.Header().Set("Content-Type", "image/webp")
			io.WriteString(w, "webp")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(paperless.Close)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)

	t.Setenv("TEST_TOKEN", "tok")
	return newTestApp(t, `
[[dashboards.parents.sections]]
title = "Home"
[[dashboards.parents.sections.links]]
title = "Paperless"
url = "https://docs.example"
icon = "paperless-ngx"
search = { type = "paperless", token_env = "TEST_TOKEN", api_url = "`+paperless.URL+`" }
[[dashboards.parents.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TEST_TOKEN", api_url = "`+broken.URL+`" }

[[dashboards.kids.sections]]
title = "Home"
[[dashboards.kids.sections.links]]
title = "Mealie"
url = "https://recipes.example"
`), paperlessHits
}

func login(a *app, groups ...string) *http.Cookie {
	return cookieFor(a, sessionCookie, session{Name: "Test", Groups: groups}, time.Hour)
}

func TestSearchRequiresLogin(t *testing.T) {
	a, _ := newSearchApp(t)
	resp := get(a, "/search?q=energy")
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("HX-Redirect") != "/auth/login" {
		t.Errorf("got %d, HX-Redirect %q; want 401 to /auth/login", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
}

func TestSearchShortQueryClearsResults(t *testing.T) {
	a, hits := newSearchApp(t)
	html := strings.TrimSpace(body(t, get(a, "/search?q=e", login(a, "parents"))))
	if want := `<div id="chips" class="chips" hx-swap-oob="true"></div>`; html != want {
		t.Errorf("got %q, want only empty chips", html)
	}
	if hits.Load() != 0 {
		t.Error("searched Paperless for a 1-character query")
	}
}

func TestSearchResults(t *testing.T) {
	a, _ := newSearchApp(t)
	key := searchKey(a.cfg.Dashboards["parents"].Sections[0].Links[0])
	html := body(t, get(a, "/search?q=energy", login(a, "parents")))
	for _, want := range []string{
		// Chips
		`<button type="button" class="chip" data-app="" aria-pressed="true">All</button>`,
		`data-app="` + key + `" aria-pressed="false"><img src="` + iconURL("paperless-ngx") + `" alt="" onerror="this.remove()">Paperless<span class="count">3</span></button>`,
		// Paperless section
		`<section class="app glass" data-app="` + key + `" aria-label="Paperless">`,
		`<a class="open" href="https://docs.example/documents?query=energy" aria-label="All 3 in Paperless">All 3<span class="open-in"> in Paperless</span>`,
		"Energy bill", "1 May 2024", `href="https://docs.example/documents/7/details"`,
		`src="/search/thumb/` + key + `/7"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("results are missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Jellyfin") {
		t.Errorf("results show Jellyfin, whose search failed:\n%s", html)
	}
}

func TestSearchHidesAppsWithoutResults(t *testing.T) {
	jellyfin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"TotalRecordCount": 0, "Items": []}`)
	}))
	t.Cleanup(jellyfin.Close)
	t.Setenv("TEST_TOKEN", "tok")
	a := newTestApp(t, `
[[dashboards.family.sections]]
title = "Media"
[[dashboards.family.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TEST_TOKEN", api_url = "`+jellyfin.URL+`", seerr_url = "https://seerr.example" }
`)
	html := body(t, get(a, "/search?q=nothing", login(a, "family")))
	if strings.Contains(html, "<section") || strings.Contains(html, `class="chip"`) {
		t.Errorf("got %s, want no sections", html)
	}
}

func TestSearchSeerrLinkOnlyWithoutSeerrSearch(t *testing.T) {
	fake := func(body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	jellyfin := fake(`{"TotalRecordCount": 1, "Items": [{"Id": "0123456789abcdef0123456789abcdef", "Name": "Garden State", "Type": "Movie"}]}`)
	seerr := fake(`{"totalPages": 1, "results": [{"id": 1, "mediaType": "movie", "title": "The Secret Garden"}]}`)
	t.Setenv("TEST_TOKEN", "tok")
	jellyfinLink := `
title = "Media"
[[dashboards.%s.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TEST_TOKEN", api_url = "` + jellyfin + `", seerr_url = "https://seerr.example" }
`
	a := newTestApp(t, "[[dashboards.parents.sections]]"+strings.ReplaceAll(jellyfinLink, "%s", "parents")+`
[[dashboards.parents.sections.links]]
title = "Seerr"
url = "https://seerr.example"
search = { type = "seerr", token_env = "TEST_TOKEN", api_url = "`+seerr+`" }

[[dashboards.kids.sections]]`+strings.ReplaceAll(jellyfinLink, "%s", "kids"))

	button := `<a href="https://seerr.example/search?query=garden">Request it in Seerr</a>`
	if html := body(t, get(a, "/search?q=garden", login(a, "kids"))); !strings.Contains(html, button) {
		t.Errorf("kids can't search Seerr, so want the button:\n%s", html)
	}
	html := body(t, get(a, "/search?q=garden", login(a, "parents")))
	if strings.Contains(html, "Request it in Seerr") {
		t.Errorf("parents can search Seerr, so want no button:\n%s", html)
	}
	if !strings.Contains(html, `aria-label="Seerr"`) {
		t.Errorf("parents should see a Seerr section:\n%s", html)
	}
}

func TestSearchOnlyOwnApps(t *testing.T) {
	a, hits := newSearchApp(t)
	html := body(t, get(a, "/search?q=energy", login(a, "kids")))
	if strings.Contains(html, "<section") || strings.Contains(html, `class="chip"`) {
		t.Errorf("got %s, want no results", html)
	}
	if hits.Load() != 0 {
		t.Error("searched Paperless for a user without it on their dashboard")
	}
}

func TestSearchThumb(t *testing.T) {
	a, _ := newSearchApp(t)
	key := searchKey(a.cfg.Dashboards["parents"].Sections[0].Links[0])

	resp := get(a, "/search/thumb/"+key+"/7", login(a, "parents"))
	if got := body(t, resp); resp.StatusCode != http.StatusOK || got != "webp" {
		t.Fatalf("got %d %q, want the thumbnail", resp.StatusCode, got)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("got CSP %q, want sandbox", csp)
	}

	for name, tt := range map[string]struct {
		path   string
		cookie *http.Cookie
	}{
		"not logged in":        {"/search/thumb/" + key + "/7", nil},
		"app not on dashboard": {"/search/thumb/" + key + "/7", login(a, "kids")},
		"invalid id":           {"/search/thumb/" + key + "/x", login(a, "parents")},
		"unknown key":          {"/search/thumb/0000000000000000/7", login(a, "parents")},
	} {
		t.Run(name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tt.cookie != nil {
				cookies = append(cookies, tt.cookie)
			}
			if resp := get(a, tt.path, cookies...); resp.StatusCode != http.StatusNotFound {
				t.Errorf("got %d, want 404", resp.StatusCode)
			}
		})
	}
}

func TestSearchKey(t *testing.T) {
	t.Setenv("TOKEN_A", "a")
	t.Setenv("TOKEN_B", "b")
	a := newTestApp(t, `
[[dashboards.family.sections]]
title = "Home"
[[dashboards.family.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TOKEN_A", limit = 3 }
[[dashboards.kids.sections]]
title = "Home"
[[dashboards.kids.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TOKEN_A", limit = 3, types = ["Movie"] }
`)
	family := a.cfg.Dashboards["family"].Sections[0].Links[0]
	kids := a.cfg.Dashboards["kids"].Sections[0].Links[0]
	if searchKey(family) == searchKey(kids) {
		t.Error("links with different search options share a key")
	}
	if len(a.providers) != 2 {
		t.Errorf("got %d providers, want 2", len(a.providers))
	}
	s := *family.Search
	s.Token = "changed"
	changed := family
	changed.Search = &s
	if searchKey(changed) != searchKey(family) {
		t.Error("the key depends on the token")
	}
}
