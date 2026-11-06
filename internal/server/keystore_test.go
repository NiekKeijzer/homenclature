package server

import (
	"io"
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

	if stored, ok := ks.get("me"); ok || stored.OnServer || stored.Keys != nil {
		t.Fatalf("before put: got %+v, %t; want nothing", stored, ok)
	}
	want := storedKeys{OnServer: true, Epoch: "e", Keys: map[string]string{paperlessURL: "mine"}}
	if err := ks.put("me", want); err != nil {
		t.Fatal(err)
	}
	if stored, ok := ks.get("me"); !ok || !stored.OnServer || stored.Epoch != "e" || stored.Keys[paperlessURL] != "mine" {
		t.Errorf("got %+v, %t; want %+v", stored, ok, want)
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

	// Someone else's file under my name, or a file from another secret,
	// counts as keys kept here, without any.
	os.WriteFile(ks.path("other"), data, 0o600)
	if stored, ok := ks.get("other"); !ok || !stored.OnServer || stored.Keys != nil {
		t.Errorf("swapped file: got %+v, %t; want kept here, no keys", stored, ok)
	}
	other, _ := newKeyStore(dir, "another-secret-another-secret-another")
	if stored, ok := other.get("me"); !ok || !stored.OnServer || stored.Keys != nil {
		t.Errorf("other secret: got %+v, %t; want kept here, no keys", stored, ok)
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
	if stored, _ := a.store.get("test-sub"); stored.OnServer || stored.Keys != nil {
		t.Errorf("the keys are still on the server: %+v", stored)
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

// A keys cookie left on another device while the keys were on the server is
// stale once they're moved back to a browser: it isn't used or renewed.
func TestKeysStaleCookie(t *testing.T) {
	a, lastToken := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	me := login(a, "parents")
	other := keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour) // another device

	resp := moveKeys(a, "server", me, keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move to server: got %d", resp.StatusCode)
	}
	if got := tokenUsed(t, a, lastToken, me, other); got != "Token mine" {
		t.Errorf("on the server: search sent %q, want the key from the server", got)
	}
	// Remove the key, then move back to this browser, with no keys.
	post(a, "/commands/keys", url.Values{"app": {appKey(a, "parents")}, "action": {"remove"}}, me)
	resp = moveKeys(a, "browser", me)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move back: got %d", resp.StatusCode)
	}
	if got := tokenUsed(t, a, lastToken, me, other); got != "Token shared" {
		t.Errorf("the other device's old cookie sent %q, want the shared key", got)
	}
	if html := body(t, get(a, "/", me, other)); !strings.Contains(html, "Use your own Paperless key") {
		t.Error("the other device's old cookie shows as a key")
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	req.AddCookie(other)
	rec := httptest.NewRecorder()
	a.renewKeys(rec, req, session{Sub: "test-sub"})
	if c := savedKeys(rec.Result()); c != nil {
		t.Errorf("a login renewed the stale cookie: %v", c)
	}

	// This browser's cookie, set when moving back, counts; a new one too.
	resp = post(a, "/commands/keys", url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"mine"}}, me, savedKeys(resp))
	if got := tokenUsed(t, a, lastToken, me, savedKeys(resp)); got != "Token mine" {
		t.Errorf("a key saved after moving back sent %q, want it", got)
	}
}

func TestRenewKeysOnServer(t *testing.T) {
	a, _ := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	a.store.put("test-sub", storedKeys{OnServer: true, Keys: map[string]string{paperlessURL: "mine"}})
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	req.AddCookie(keysFor(a, "test-sub", map[string]string{paperlessURL: "old"}, time.Hour))
	rec := httptest.NewRecorder()
	a.renewKeys(rec, req, session{Sub: "test-sub"})
	if c := savedKeys(rec.Result()); c != nil {
		t.Errorf("with keys on the server, a login set keys cookie %v", c)
	}
}

// Two changes at once can't undo each other: while one key is checked in the
// app, a move waits for it, then moves the key too.
func TestKeysChangedOneAtATime(t *testing.T) {
	checking, release := make(chan struct{}), make(chan struct{})
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Token slow" {
			close(checking)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"count": 0, "results": []}`)
	}))
	t.Cleanup(paperless.Close)
	t.Setenv("TEST_TOKEN", "shared")
	a := newTestApp(t, `
[[dashboards.parents.sections]]
title = "Home"
[[dashboards.parents.sections.links]]
title = "Paperless"
url = "`+paperlessURL+`"
search = { type = "paperless", token_env = "TEST_TOKEN", api_url = "`+paperless.URL+`" }
`)
	a.store = newTestStore(t, t.TempDir())
	me := login(a, "parents")
	if resp := moveKeys(a, "server", me); resp.StatusCode != http.StatusOK {
		t.Fatalf("move to server: got %d", resp.StatusCode)
	}

	saved, moved := make(chan *http.Response), make(chan *http.Response)
	go func() {
		saved <- post(a, "/commands/keys", url.Values{"app": {appKey(a, "parents")}, "action": {"save"}, "key": {"slow"}}, me)
	}()
	<-checking
	go func() { moved <- moveKeys(a, "browser", me) }()
	var resp *http.Response
	select {
	case resp = <-moved:
		t.Error("the move ran while the key was being checked")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if resp := <-saved; resp.StatusCode != http.StatusOK {
		t.Fatalf("save: got %d", resp.StatusCode)
	}
	if resp == nil {
		resp = <-moved
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: got %d", resp.StatusCode)
	}
	if stored, _ := a.store.get("test-sub"); stored.OnServer {
		t.Errorf("the save undid the move: %+v", stored)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(savedKeys(resp))
	if k := a.readKeys(req, session{Sub: "test-sub"}); k.Keys[paperlessURL] != "slow" {
		t.Errorf("the move lost the saved key: %+v", k)
	}
}

// A repeated move, as from a double click, carries this browser's cookie
// from before the first one's answer. It changes nothing, so it must not
// overwrite the cookie the first one set.
func TestKeysMoveTwice(t *testing.T) {
	a, lastToken := newKeysApp(t)
	a.store = newTestStore(t, t.TempDir())
	me := login(a, "parents")
	moveKeys(a, "server", me, keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour))

	first := moveKeys(a, "browser", me)
	second := moveKeys(a, "browser", me)
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("got %d and %d, want 200 twice", first.StatusCode, second.StatusCode)
	}
	if c := savedKeys(second); c != nil {
		t.Errorf("the repeated move set keys cookie %v", c)
	}
	if got := tokenUsed(t, a, lastToken, me, savedKeys(first)); got != "Token mine" {
		t.Errorf("search sent %q, want the key moved back", got)
	}
}

// A file that can't be read counts as keys on the server, without any.
// Moving them back must still make old cookies from other devices stale.
func TestKeysUnreadableFile(t *testing.T) {
	a, lastToken := newKeysApp(t)
	dir := t.TempDir()
	a.store = newTestStore(t, dir)
	me := login(a, "parents")
	other := keysFor(a, "test-sub", map[string]string{paperlessURL: "mine"}, time.Hour) // another device, from before
	moveKeys(a, "server", me)
	os.WriteFile(a.store.path("test-sub"), []byte("not a keys file, not even encrypted"), 0o600)

	if html := body(t, get(a, "/", me)); !strings.Contains(html, "Keep your keys in this browser only") {
		t.Fatal("an unreadable file should count as keys on the server")
	}
	resp := moveKeys(a, "browser", me)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move back: got %d", resp.StatusCode)
	}
	if got := tokenUsed(t, a, lastToken, me, other); got != "Token shared" {
		t.Errorf("the other device's old cookie sent %q, want the shared key", got)
	}
}
