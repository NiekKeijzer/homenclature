package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const paperlessURL = "https://docs.example"

// newKeysApp returns an app where parents can search a fake Paperless (which
// takes the shared key "shared" and a personal key "mine") and Jellyfin
// (which doesn't take personal keys). The fake records the last token it
// got. Kids can't search anything; media can only search Jellyfin.
func newKeysApp(t *testing.T) (a *app, lastToken *atomic.Value) {
	lastToken = &atomic.Value{}
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		lastToken.Store(token)
		if token != "Token shared" && token != "Token mine" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/documents/":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"count": 1, "results": [{"id": 7, "title": "Energy bill"}]}`)
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

	t.Setenv("TEST_TOKEN", "shared")
	jellyfin := `[[dashboards.%s.sections.links]]
title = "Jellyfin"
url = "https://jellyfin.example"
search = { type = "jellyfin", token_env = "TEST_TOKEN", api_url = "` + broken.URL + `" }
`
	return newTestApp(t, `
[[dashboards.parents.sections]]
title = "Home"
[[dashboards.parents.sections.links]]
title = "Paperless"
url = "`+paperlessURL+`"
search = { type = "paperless", token_env = "TEST_TOKEN", api_url = "`+paperless.URL+`" }
`+strings.ReplaceAll(jellyfin, "%s", "parents")+`
[[dashboards.kids.sections]]
title = "Home"
[[dashboards.kids.sections.links]]
title = "Mealie"
url = "https://recipes.example"

[[dashboards.media.sections]]
title = "Media"
`+strings.ReplaceAll(jellyfin, "%s", "media")), lastToken
}

func appKey(a *app, dashboard string) string {
	return searchKey(a.cfg.Dashboards[dashboard].Sections[0].Links[0])
}

func keysFor(a *app, sub string, keys map[string]string, ttl time.Duration) *http.Cookie {
	return cookieFor(a, keysCookie, personalKeys{Sub: sub, Keys: keys}, ttl)
}

func postKey(a *app, form url.Values, cookies ...*http.Cookie) *http.Response {
	return post(a, "/keys", form, cookies...)
}

func post(a *app, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	return rec.Result()
}

func savedKeys(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == keysCookie {
			return c
		}
	}
	return nil
}

// tokenUsed searches as parents and returns the token Paperless got.
func tokenUsed(t *testing.T, a *app, lastToken *atomic.Value, cookies ...*http.Cookie) string {
	t.Helper()
	lastToken.Store("")
	if html := body(t, get(a, "/search?q=energy", cookies...)); !strings.Contains(html, "Energy bill") {
		t.Fatalf("got no Paperless results:\n%s", html)
	}
	return lastToken.Load().(string)
}

func TestKeysSaveAndUse(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	resp := postKey(a, url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"  mine \n"}}, me)
	if want := "/keys#app-" + appKey(a, "parents"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
		t.Fatalf("got %d to %q, want 303 to %s", resp.StatusCode, resp.Header.Get("Location"), want)
	}
	keys := savedKeys(resp)
	if keys == nil || keys.MaxAge != int(keysTTL.Seconds()) {
		t.Fatalf("got keys cookie %v, want one for %v", keys, keysTTL)
	}

	if got := tokenUsed(t, a, lastToken, me, keys); got != "Token mine" {
		t.Errorf("search sent %q, want the personal key", got)
	}
	lastToken.Store("")
	if resp := get(a, "/search/thumb/"+appKey(a, "parents")+"/7", me, keys); resp.StatusCode != http.StatusOK || lastToken.Load() != "Token mine" {
		t.Errorf("thumbnail: got %d with %q, want 200 with the personal key", resp.StatusCode, lastToken.Load())
	}
	html := body(t, get(a, "/keys", me, keys))
	if !strings.Contains(html, "Using your key") || strings.Contains(html, "mine") {
		t.Errorf("keys page should say the key is set, without showing it:\n%s", html)
	}
}

func TestKeysWithoutPersonalKey(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	mine := map[string]string{paperlessURL: "mine"}
	noSub := cookieFor(a, sessionCookie, session{Name: "Test", Groups: []string{"parents"}}, time.Hour)
	for name, cookies := range map[string][]*http.Cookie{
		"no keys":             {me},
		"someone else's":      {me, keysFor(a, "someone-else", mine, time.Hour)},
		"expired":             {me, keysFor(a, "test-sub", mine, -time.Second)},
		"session without sub": {noSub, keysFor(a, "", mine, time.Hour)},
		"other app's key":     {me, keysFor(a, "test-sub", map[string]string{"https://other.example": "mine"}, time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tokenUsed(t, a, lastToken, cookies...); got != "Token shared" {
				t.Errorf("search sent %q, want the shared key", got)
			}
		})
	}
	if resp := get(a, "/keys", noSub); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/auth/login" {
		t.Errorf("keys page without sub: got %d to %q, want redirect to login", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestKeysRemove(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	keys := keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour)
	resp := postKey(a, url.Values{"app": {appKey(a, "parents")}, "action": {"remove"}}, me, keys)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", resp.StatusCode)
	}
	if got := tokenUsed(t, a, lastToken, me, savedKeys(resp)); got != "Token shared" {
		t.Errorf("search sent %q after removing the key, want the shared key", got)
	}
}

func TestKeysRejected(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	full := keysFor(a, "test-sub", map[string]string{"https://other.example": strings.Repeat("k", maxKeysBytes)}, time.Hour)
	for name, tt := range map[string]struct {
		key     string
		cookies []*http.Cookie
		want    string
		checked bool // the key was tried in the app
	}{
		"wrong":     {"wrong", nil, "accept this key", true},
		"empty":     {" ", nil, "Paste a key", false},
		"too long":  {strings.Repeat("k", maxKeyLen+1), nil, "Paste a key", false},
		"space":     {"mi ne", nil, "Paste a key", false},
		"control":   {"mi\x01ne", nil, "Paste a key", false},
		"non-ASCII": {"minè", nil, "Paste a key", false},
		"no room":   {"mine", []*http.Cookie{full}, "no room", false},
	} {
		t.Run(name, func(t *testing.T) {
			lastToken.Store("")
			form := url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {tt.key}}
			resp := postKey(a, form, append([]*http.Cookie{me}, tt.cookies...)...)
			if resp.StatusCode != http.StatusUnprocessableEntity || savedKeys(resp) != nil {
				t.Fatalf("got %d, keys cookie %v; want 422 and no cookie", resp.StatusCode, savedKeys(resp))
			}
			// The field with the error is focused, so it scrolls into view.
			if html := body(t, resp); !strings.Contains(html, tt.want) || strings.Count(html, " autofocus") != 1 {
				t.Errorf("page doesn't say %q, with the field focused:\n%s", tt.want, html)
			}
			if checked := lastToken.Load() != ""; checked != tt.checked {
				t.Errorf("key tried in the app: %t, want %t", checked, tt.checked)
			}
		})
	}
}

func TestKeysOnlyOwnApps(t *testing.T) {
	a, _ := newKeysApp(t)
	html := body(t, get(a, "/keys", login(a, "parents")))
	if !strings.Contains(html, "Paperless") || strings.Contains(html, "Jellyfin") {
		t.Errorf("keys page should list Paperless only:\n%s", html)
	}
	for name, tt := range map[string]struct {
		group, app, action string
	}{
		"no personal keys":    {"parents", appKey(a, "media"), "save"},
		"not on dashboard":    {"kids", appKey(a, "parents"), "save"},
		"unknown action":      {"parents", appKey(a, "parents"), "show"},
		"remove not on board": {"kids", appKey(a, "parents"), "remove"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := postKey(a, url.Values{"app": {tt.app}, "action": {tt.action}, "key": {"mine"}}, login(a, tt.group))
			if resp.StatusCode != http.StatusBadRequest || savedKeys(resp) != nil {
				t.Errorf("got %d, keys cookie %v; want 400 and no cookie", resp.StatusCode, savedKeys(resp))
			}
		})
	}
}

func TestKeysCrossOrigin(t *testing.T) {
	a, _ := newKeysApp(t)
	form := url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"mine"}}
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(login(a, "parents"))
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	if resp := rec.Result(); resp.StatusCode != http.StatusForbidden || savedKeys(resp) != nil {
		t.Errorf("got %d, keys cookie %v; want 403 and no cookie", resp.StatusCode, savedKeys(resp))
	}
}

func TestRenewKeys(t *testing.T) {
	a, _ := newKeysApp(t)
	s := session{Sub: "test-sub"}
	for name, tt := range map[string]struct {
		sub  string
		want bool
	}{"own": {"test-sub", true}, "someone else's": {"someone-else", false}} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
			req.AddCookie(keysFor(a, tt.sub, map[string]string{paperlessURL: "mine"}, time.Hour))
			rec := httptest.NewRecorder()
			a.renewKeys(rec, req, s)
			keys := savedKeys(rec.Result())
			if renewed := keys != nil && keys.MaxAge == int(keysTTL.Seconds()); renewed != tt.want {
				t.Errorf("renewed: %t, want %t", renewed, tt.want)
			}
		})
	}
}
