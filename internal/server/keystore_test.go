package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-test-secret-test-secret"

func newTestStore(t *testing.T, dir string) *keyStore {
	t.Helper()
	ks, err := newKeyStore(dir, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

func TestKeyStore(t *testing.T) {
	if ks, err := newKeyStore("", testSecret); ks != nil || err != nil {
		t.Fatalf("no dir: got %v, %v; want no store", ks, err)
	}
	dir := filepath.Join(t.TempDir(), "data") // made if missing
	ks := newTestStore(t, dir)

	if keys, ok := ks.get("me"); ok || keys != nil {
		t.Fatalf("before put: got %v, %t; want nothing", keys, ok)
	}
	// An empty file still says the keys are kept here.
	if err := ks.put("me", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if keys, ok := ks.get("me"); !ok || len(keys) != 0 {
		t.Errorf("no keys: got %v, %t; want none, kept here", keys, ok)
	}
	if err := ks.put("me", map[string]string{paperlessURL: "mine"}); err != nil {
		t.Fatal(err)
	}
	if keys, ok := ks.get("me"); !ok || keys[paperlessURL] != "mine" {
		t.Errorf("got %v, %t; want the key", keys, ok)
	}

	data, err := os.ReadFile(ks.path("me"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "mine") || strings.Contains(string(data), paperlessURL) {
		t.Error("the file isn't encrypted")
	}
	if fi, _ := os.Stat(ks.path("me")); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("got %d files, want only the keys file", len(entries))
	}

	// Someone else's file under my name, or a file from another secret, is
	// kept here but has no keys.
	os.WriteFile(ks.path("other"), data, 0o600)
	if keys, ok := ks.get("other"); !ok || keys != nil {
		t.Errorf("swapped file: got %v, %t; want no keys", keys, ok)
	}
	other, _ := newKeyStore(dir, "another-secret-another-secret-another")
	if keys, ok := other.get("me"); !ok || keys != nil {
		t.Errorf("other secret: got %v, %t; want no keys", keys, ok)
	}

	if err := ks.remove("me"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ks.get("me"); ok {
		t.Error("removed keys are still kept here")
	}
	if err := ks.remove("me"); err != nil {
		t.Errorf("removing again: %v", err)
	}
}

func TestKeyStoreNotWritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	if _, err := newKeyStore(file, testSecret); err == nil {
		t.Error("a file as the data dir should fail at startup")
	}
}

// moveKeys posts a command that moves the user's keys.
func moveKeys(a *app, to string, cookies ...*http.Cookie) *http.Response {
	return post(a, "/commands/keys", url.Values{"action": {to}}, cookies...)
}

func TestKeysOnServer(t *testing.T) {
	a, lastToken := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	me := login(a, "parents")
	inBrowser := keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour)

	html := body(t, get(a, "/", me, inBrowser))
	if !strings.Contains(html, `data-action="server"`) || !strings.Contains(html, "Keep your keys on all your devices") {
		t.Fatalf("page is missing the command to keep keys on the server:\n%s", html)
	}

	// Moving them to the server takes them out of this browser.
	resp := moveKeys(a, "server", me, inBrowser)
	html = body(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(html, "Your keys are now on all your devices.") || !strings.Contains(html, "Keep your keys in this browser only") {
		t.Fatalf("got %d:\n%s", resp.StatusCode, html)
	}
	if c := savedKeys(resp); c == nil || c.MaxAge >= 0 {
		t.Errorf("got keys cookie %v, want it cleared", c)
	}
	// Another device, without the cookie, has them.
	if got := tokenUsed(t, a, lastToken, me); got != "Token mine" {
		t.Errorf("search sent %q, want the personal key from the server", got)
	}

	// Removing and saving keys changes them on the server, not in a cookie.
	resp = post(a, "/commands/keys", url.Values{"app": {appKey(a, "parents")}, "action": {"remove"}}, me)
	if resp.StatusCode != http.StatusOK || savedKeys(resp) != nil {
		t.Fatalf("remove: got %d, keys cookie %v; want 200, no cookie", resp.StatusCode, savedKeys(resp))
	}
	if got := tokenUsed(t, a, lastToken, me); got != "Token shared" {
		t.Errorf("search sent %q after removing the key, want the shared key", got)
	}
	if html := body(t, get(a, "/", me)); !strings.Contains(html, "Keep your keys in this browser only") {
		t.Error("without keys, they should still be kept on the server")
	}
	resp = post(a, "/commands/keys", url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"mine"}}, me)
	if resp.StatusCode != http.StatusOK || savedKeys(resp) != nil {
		t.Fatalf("save: got %d, keys cookie %v; want 200, no cookie", resp.StatusCode, savedKeys(resp))
	}
	if got := tokenUsed(t, a, lastToken, me); got != "Token mine" {
		t.Errorf("search sent %q, want the saved key", got)
	}

	// Moving them back puts them in this browser only.
	resp = moveKeys(a, "browser", me)
	if html := body(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(html, "Your keys are now in this browser only.") {
		t.Fatalf("got %d:\n%s", resp.StatusCode, html)
	}
	back := savedKeys(resp)
	if back == nil || back.MaxAge != int(keysTTL.Seconds()) {
		t.Fatalf("got keys cookie %v, want one for %v", back, keysTTL)
	}
	if _, ok := a.store.get("test-sub"); ok {
		t.Error("the keys are still on the server")
	}
	if got := tokenUsed(t, a, lastToken, me, back); got != "Token mine" {
		t.Errorf("search sent %q, want the personal key from this browser", got)
	}
	if got := tokenUsed(t, a, lastToken, me); got != "Token shared" {
		t.Errorf("another device sent %q, want the shared key", got)
	}
}

func TestKeysOnServerLeaveOthersCookie(t *testing.T) {
	a, _ := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	theirs := keysFor(a, "someone-else", map[string]string{paperlessURL: "theirs"}, time.Hour)
	resp := moveKeys(a, "server", login(a, "parents"), theirs)
	if resp.StatusCode != http.StatusOK || savedKeys(resp) != nil {
		t.Errorf("got %d, keys cookie %v; want 200, someone else's cookie left alone", resp.StatusCode, savedKeys(resp))
	}
}

func TestKeysMoveRefused(t *testing.T) {
	a, _ := newKeysApp(t)
	if resp := moveKeys(a, "server", login(a, "parents")); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("without a key store: got %d, want 400", resp.StatusCode)
	}
	a.store = newTestStore(t, t.TempDir())
	if resp := moveKeys(a, "server", login(a, "kids")); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("without apps that take a key: got %d, want 400", resp.StatusCode)
	}
	if html := body(t, get(a, "/", login(a, "kids"))); strings.Contains(html, "Keep your keys") {
		t.Error("people without apps that take a key shouldn't get the command")
	}
	if _, ok := a.store.get("test-sub"); ok {
		t.Error("refused moves saved keys")
	}

	// The data dir went away: the key isn't saved, and the user is told.
	dir := t.TempDir()
	a.store = newTestStore(t, dir)
	os.RemoveAll(dir)
	resp := moveKeys(a, "server", login(a, "parents"))
	if got := body(t, resp); resp.StatusCode != http.StatusInternalServerError || !strings.Contains(got, "couldn't be saved") {
		t.Errorf("got %d %q, want 500 with why", resp.StatusCode, got)
	}
}

func TestKeysPageMove(t *testing.T) {
	a, lastToken := newKeysApp(t)
	me := login(a, "parents")
	if html := body(t, get(a, "/keys", me)); strings.Contains(html, `id="where"`) {
		t.Error("without a key store, the keys page shouldn't offer to move keys")
	}
	a.store = newTestStore(t, t.TempDir())
	if html := body(t, get(a, "/keys", me)); !strings.Contains(html, `value="server"`) || !strings.Contains(html, "in this browser only") {
		t.Errorf("keys page should offer to keep keys on the server:\n%s", html)
	}
	resp := postKey(a, url.Values{"action": {"server"}}, me)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/keys#where" {
		t.Fatalf("got %d to %q, want 303 to /keys#where", resp.StatusCode, resp.Header.Get("Location"))
	}
	if html := body(t, get(a, "/keys", me)); !strings.Contains(html, `value="browser"`) || !strings.Contains(html, "on the server") {
		t.Errorf("keys page should say the keys are on the server:\n%s", html)
	}
	resp = postKey(a, url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"mine"}}, me)
	if resp.StatusCode != http.StatusSeeOther || savedKeys(resp) != nil {
		t.Fatalf("save: got %d, keys cookie %v; want 303, no cookie", resp.StatusCode, savedKeys(resp))
	}
	if got := tokenUsed(t, a, lastToken, me); got != "Token mine" {
		t.Errorf("search sent %q, want the personal key from the server", got)
	}
}

func TestRenewKeysOnServer(t *testing.T) {
	a, _ := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	a.store.put("test-sub", map[string]string{paperlessURL: "mine"})
	s := session{Sub: "test-sub"}
	for name, tt := range map[string]struct {
		sub     string
		cleared bool
	}{"own, left from before": {"test-sub", true}, "someone else's": {"someone-else", false}} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
			req.AddCookie(keysFor(a, tt.sub, map[string]string{paperlessURL: "old"}, time.Hour))
			rec := httptest.NewRecorder()
			a.renewKeys(rec, req, s)
			c := savedKeys(rec.Result())
			if cleared := c != nil && c.MaxAge < 0; cleared != tt.cleared || (c != nil && !cleared) {
				t.Errorf("got keys cookie %v, want cleared: %t", c, tt.cleared)
			}
		})
	}
}
