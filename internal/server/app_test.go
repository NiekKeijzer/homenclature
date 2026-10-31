package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func newTestApp(t *testing.T, configTOML string) *app {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(configTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.DefaultDashboardEnv, "")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return &app{cfg: cfg, aead: newAEAD("test-secret-test-secret-test-secret"), icons: newIconProxy(cfg)}
}

// cookieFor returns an encrypted cookie as the app would set it.
func cookieFor(a *app, name string, v any, ttl time.Duration) *http.Cookie {
	rec := httptest.NewRecorder()
	a.setCookie(rec, name, v, ttl)
	return rec.Result().Cookies()[0]
}

func get(a *app, path string, cookies ...*http.Cookie) *http.Response {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPageWithoutSessionRedirectsToLogin(t *testing.T) {
	a := newTestApp(t, `[[dashboards.family.sections]]
title = "Home"`)
	resp := get(a, "/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/auth/login" {
		t.Fatalf("got %d to %q, want redirect to /auth/login", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestPageShowsLinksForGroups(t *testing.T) {
	example, err := os.ReadFile("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, string(example))
	s := session{Name: "Niek", Groups: []string{"parents", "family"}}
	resp := get(a, "/", cookieFor(a, sessionCookie, s, time.Hour))
	html := body(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", resp.StatusCode)
	}
	for _, want := range []string{"Hello, Niek", "Paperless", "Proxmox", "Jellyfin", `src="` + iconURL("jellyfin") + `"`} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(html, "Audiobookshelf") {
		t.Error("page shows the kids-only link Audiobookshelf")
	}
}

func TestPageWithoutLinks(t *testing.T) {
	a := newTestApp(t, `[[dashboards.family.sections]]
title = "Home"`)
	resp := get(a, "/", cookieFor(a, sessionCookie, session{Name: "Guest"}, time.Hour))
	if html := body(t, resp); !strings.Contains(html, "No links for you yet.") {
		t.Errorf("page is missing the empty message:\n%s", html)
	}
}

func TestCookieRejected(t *testing.T) {
	a := newTestApp(t, `[[dashboards.family.sections]]
title = "Home"`)
	valid := cookieFor(a, sessionCookie, session{Name: "Niek"}, time.Hour)

	tampered := *valid
	tampered.Value = valid.Value[:len(valid.Value)-2] + "AA"
	expired := cookieFor(a, sessionCookie, session{Name: "Niek"}, -time.Second)
	// A login cookie must not work as a session cookie.
	swapped := cookieFor(a, loginCookie, session{Name: "Niek"}, time.Hour)
	swapped.Name = sessionCookie

	for name, c := range map[string]*http.Cookie{"tampered": &tampered, "expired": expired, "swapped": swapped} {
		t.Run(name, func(t *testing.T) {
			if resp := get(a, "/", c); resp.StatusCode != http.StatusFound {
				t.Errorf("got status %d, want redirect to login", resp.StatusCode)
			}
		})
	}
	if resp := get(a, "/", valid); resp.StatusCode != http.StatusOK {
		t.Errorf("valid cookie: got status %d", resp.StatusCode)
	}
}

func TestIconProxy(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/not-an-image" {
			w.Header().Set("Content-Type", "text/html")
		} else {
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		io.WriteString(w, "<svg/>")
	}))
	defer upstream.Close()

	icon := upstream.URL + "/icon.svg"
	notImage := upstream.URL + "/not-an-image"
	a := newTestApp(t, `
[[dashboards.family.sections]]
title = "Home"
[[dashboards.family.sections.links]]
title = "A"
url = "https://a.example"
icon = "`+icon+`"
[[dashboards.family.sections.links]]
title = "B"
url = "https://b.example"
icon = "`+notImage+`"
`)

	for range 2 {
		resp := get(a, iconURL(icon))
		if got := body(t, resp); resp.StatusCode != http.StatusOK || got != "<svg/>" {
			t.Fatalf("got %d %q, want the icon", resp.StatusCode, got)
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
			t.Errorf("got CSP %q, want sandbox", csp)
		}
	}
	if hits != 1 {
		t.Errorf("upstream hit %d times, want 1 (cached)", hits)
	}
	if resp := get(a, iconURL(notImage)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("non-image: got status %d, want 404", resp.StatusCode)
	}
	if resp := get(a, iconURL("https://not-in-config.example/x.svg")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("icon not in config: got status %d, want 404", resp.StatusCode)
	}
}

func TestIconSources(t *testing.T) {
	got := iconSources("jellyfin")
	if len(got) != 2 || got[0] != dashboardIcons+"/svg/jellyfin.svg" || got[1] != dashboardIcons+"/png/jellyfin.png" {
		t.Errorf("got %q", got)
	}
	if got := iconSources("https://x.example/a.png"); len(got) != 1 || got[0] != "https://x.example/a.png" {
		t.Errorf("got %q", got)
	}
}
