package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPageCommands(t *testing.T) {
	a, _ := newKeysApp(t)
	mine := keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour)
	for name, tt := range map[string]struct {
		group        string
		cookies      []*http.Cookie
		want, absent []string
	}{
		// Key commands are grouped under "Manage your keys". The /keys page
		// isn't linked: commands stay on the page.
		"shared key": {"parents", nil,
			[]string{`id="commands"`, `data-opens="keys"`, "Manage your keys", `data-group="keys" data-key="` + appKey(a, "parents") + `" data-action="save"`, "Use your own Paperless key", `class="command-hint"`, `class="prompt"`},
			[]string{"Remove your Paperless key", "Jellyfin key", `href="/keys"`}},
		"own key": {"parents", []*http.Cookie{mine},
			[]string{"Replace your Paperless key", "Remove your Paperless key"},
			[]string{"Use your own Paperless key"}},
		// No app that takes a key: no commands, so no hint or prompt either.
		"jellyfin only": {"media", nil, nil, []string{`id="commands"`, `class="command-hint"`, `class="prompt"`}},
		"no search":     {"kids", nil, nil, []string{`id="commands"`, `class="command-hint"`, `class="prompt"`}},
	} {
		t.Run(name, func(t *testing.T) {
			html := body(t, get(a, "/", append([]*http.Cookie{login(a, tt.group)}, tt.cookies...)...))
			for _, w := range tt.want {
				if !strings.Contains(html, w) {
					t.Errorf("page is missing %q", w)
				}
			}
			for _, n := range tt.absent {
				if strings.Contains(html, n) {
					t.Errorf("page has %q", n)
				}
			}
		})
	}
}

func TestCommandKey(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	form := func(action, key string) url.Values {
		return url.Values{"app": {appKey(a, "parents")}, "action": {action}, "key": {key}}
	}

	resp := post(a, "/commands/keys", form("save", "wrong"), me)
	if got := body(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(got, "Paperless didn't accept this key") || savedKeys(resp) != nil {
		t.Fatalf("wrong key: got %d %q, keys cookie %v; want 422 with why, no cookie", resp.StatusCode, got, savedKeys(resp))
	}

	resp = post(a, "/commands/keys", form("save", "mine"), me)
	html := body(t, resp)
	keys := savedKeys(resp)
	if resp.StatusCode != http.StatusOK || keys == nil {
		t.Fatalf("got %d, keys cookie %v; want 200 and a cookie", resp.StatusCode, keys)
	}
	for _, w := range []string{`id="commands"`, "Paperless now uses your key.", "Remove your Paperless key"} {
		if !strings.Contains(html, w) {
			t.Errorf("answer is missing %q:\n%s", w, html)
		}
	}
	if got := tokenUsed(t, a, lastToken, me, keys); got != "Token mine" {
		t.Errorf("search sent %q, want the personal key", got)
	}

	resp = post(a, "/commands/keys", form("remove", ""), me, keys)
	if html := body(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(html, "Paperless uses the shared key again.") || !strings.Contains(html, "Use your own Paperless key") {
		t.Errorf("remove: got %d:\n%s", resp.StatusCode, html)
	}
	if got := tokenUsed(t, a, lastToken, me, savedKeys(resp)); got != "Token shared" {
		t.Errorf("search sent %q after removing the key, want the shared key", got)
	}
}

func TestCommandKeyRefused(t *testing.T) {
	a, _ := newKeysApp(t)
	noSub := cookieFor(a, sessionCookie, session{Name: "Test", Groups: []string{"parents"}}, time.Hour)
	save := url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"mine"}}
	for name, tt := range map[string]struct {
		form    url.Values
		cookies []*http.Cookie
		want    int
	}{
		"not logged in":       {save, nil, http.StatusUnauthorized},
		"session without sub": {save, []*http.Cookie{noSub}, http.StatusUnauthorized},
		"app takes no key":    {url.Values{"app": {appKey(a, "media")}, "action": {"save"}, "key": {"mine"}}, []*http.Cookie{login(a, "parents")}, http.StatusBadRequest},
		"app not on page":     {save, []*http.Cookie{login(a, "kids")}, http.StatusBadRequest},
		"unknown action":      {url.Values{"app": {appKey(a, "parents")}, "action": {"show"}}, []*http.Cookie{login(a, "parents")}, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			resp := post(a, "/commands/keys", tt.form, tt.cookies...)
			if resp.StatusCode != tt.want || savedKeys(resp) != nil {
				t.Errorf("got %d, keys cookie %v; want %d and no cookie", resp.StatusCode, savedKeys(resp), tt.want)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/commands/keys", strings.NewReader(save.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(login(a, "parents"))
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	if resp := rec.Result(); resp.StatusCode != http.StatusForbidden || savedKeys(resp) != nil {
		t.Errorf("cross-site: got %d, keys cookie %v; want 403 and no cookie", resp.StatusCode, savedKeys(resp))
	}
}

func TestSearchSkipsCommands(t *testing.T) {
	a, lastToken := newKeysApp(t)
	lastToken.Store("")
	html := body(t, get(a, "/search?q="+url.QueryEscape("> paperless key"), login(a, "parents")))
	if strings.Contains(html, "<section") || lastToken.Load() != "" {
		t.Errorf("a command was searched in the apps:\n%s", html)
	}
}
