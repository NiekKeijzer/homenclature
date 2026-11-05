package server

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
	"github.com/NiekKeijzer/homenclature/internal/search"
)

// Personal keys: people can search an app with their own API key instead of
// the shared one (token_env). The keys stay in an encrypted cookie in their
// browser, tied to their OIDC subject. With HOMENCLATURE_DATA_DIR set, each
// person can choose to keep them on the server instead, for all their
// devices (keystore.go).

const (
	keysCookie   = "homenclature_keys"
	keysTTL      = 400 * 24 * time.Hour // the longest browsers keep a cookie
	maxKeyLen    = 512
	maxKeysBytes = 2048 // URLs and keys together, so the cookie stays under 4 KB
)

// keyHelp says where people make their own key, for the search types that
// take one. Jellyfin API keys are made by admins and Seerr has one admin
// key, so those always use the shared key.
var keyHelp = map[string]string{
	"home-assistant": "Profile > Security > Long-lived access tokens",
	"immich":         "Account settings > API keys, with asset.read and asset.view",
	"mealie":         "Profile > API tokens",
	"paperless":      "My Profile > API auth token",
}

// personalKeys is the keys cookie.
type personalKeys struct {
	Sub  string            `json:"sub"`           // whose keys they are
	Keys map[string]string `json:"keys"`          // by link URL
	Gen  int               `json:"gen,omitempty"` // userKeys.Gen when it was set
}

// userKeys are the user's own keys, and where they're kept.
type userKeys struct {
	Keys     map[string]string // by link URL; nil if they have none
	OnServer bool              // in the key store, for all their devices; else in this browser
	// Gen counts the user's moves to the server (0 if none). A keys cookie
	// from an older one was left on another device meanwhile, so it's stale:
	// once the keys are back in a browser, only that browser's count.
	Gen int
}

// readKeys returns the user's own keys: from the key store if they keep
// them there, else from this browser. Keys of someone else who used this
// browser, or stale ones, are ignored.
func (a *app) readKeys(r *http.Request, s session) userKeys {
	if s.Sub == "" {
		return userKeys{}
	}
	var stored storedKeys
	if a.store != nil {
		stored, _ = a.store.get(s.Sub)
		if stored.OnServer {
			return userKeys{Keys: stored.Keys, OnServer: true, Gen: stored.Gen}
		}
	}
	var k personalKeys
	if !a.readCookie(r, keysCookie, &k) || k.Sub != s.Sub || k.Gen != stored.Gen {
		return userKeys{Gen: stored.Gen}
	}
	return userKeys{Keys: k.Keys, Gen: k.Gen}
}

// renewKeys gives the user's keys in this browser a fresh TTL, so they only
// expire after keysTTL without a login.
func (a *app) renewKeys(w http.ResponseWriter, r *http.Request, s session) {
	if k := a.readKeys(r, s); !k.OnServer && len(k.Keys) > 0 {
		a.setCookie(w, keysCookie, personalKeys{s.Sub, k.Keys, k.Gen}, keysTTL)
	}
}

// writeKeys saves k where it says. Moved to the server, the keys leave this
// browser and Gen goes up, so the cookies left anywhere go stale. Moved back,
// the file stays without keys, to keep Gen.
func (a *app) writeKeys(w http.ResponseWriter, s session, k userKeys, wasOnServer bool) error {
	if k.OnServer {
		if !wasOnServer {
			k.Gen++
		}
		if err := a.store.put(s.Sub, storedKeys{OnServer: true, Gen: k.Gen, Keys: k.Keys}); err != nil {
			return err
		}
		// Keys read from this browser were the user's; with none, a keys
		// cookie here is someone else's, so it stays.
		if !wasOnServer && len(k.Keys) > 0 {
			a.clearCookie(w, keysCookie)
		}
		return nil
	}
	if wasOnServer {
		if err := a.store.put(s.Sub, storedKeys{Gen: k.Gen}); err != nil {
			return err
		}
	}
	a.setCookie(w, keysCookie, personalKeys{s.Sub, k.Keys, k.Gen}, keysTTL)
	return nil
}

// lockKeys holds the user's keys until unlock, so two changes at once (from
// two devices, say) can't read the same keys and undo each other.
func (a *app) lockKeys(sub string) (unlock func()) {
	m, _ := a.keyLocks.LoadOrStore(sub, new(sync.Mutex))
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// provider searches l with the user's own key if they set one, else with
// the shared key.
func (a *app) provider(l config.Link, keys map[string]string) search.Provider {
	if k := keys[l.URL]; k != "" && keyHelp[l.Search.Type] != "" {
		s := *l.Search
		s.Token = k
		l.Search = &s
		p, _ := search.New(l, searchClient) // can't fail: the type was checked at startup
		return p
	}
	return a.providers[searchKey(l)]
}

// keyApp is an app on the keys page.
type keyApp struct {
	Key   string // searchKey, to name the app in the form
	Link  config.Link
	Help  string // where to make a key
	Own   bool   // the user set their own key
	Error string // why their key wasn't saved
}

// keyApps returns the apps on the user's dashboard that take their own key,
// each URL once.
func (a *app) keyApps(s session, keys map[string]string) []keyApp {
	var out []keyApp
	for _, r := range a.searchable(s.Groups) {
		help := keyHelp[r.Link.Search.Type]
		if help == "" || slices.ContainsFunc(out, func(k keyApp) bool { return k.Link.URL == r.Link.URL }) {
			continue
		}
		out = append(out, keyApp{Key: r.Key, Link: r.Link, Help: help, Own: keys[r.Link.URL] != ""})
	}
	return out
}

// keysSession returns the session, or sends the user to log in. Sessions
// from before personal keys have no subject to tie keys to.
func (a *app) keysSession(w http.ResponseWriter, r *http.Request) (session, bool) {
	var s session
	if !a.readCookie(r, sessionCookie, &s) || s.Sub == "" {
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return s, false
	}
	return s, true
}

func (a *app) keysPage(w http.ResponseWriter, r *http.Request) {
	s, ok := a.keysSession(w, r)
	if !ok {
		return
	}
	k := a.readKeys(r, s)
	a.renderKeys(w, s, a.keyApps(s, k.Keys), k.OnServer, http.StatusOK)
}

// saveKey handles the forms on /keys.
func (a *app) saveKey(w http.ResponseWriter, r *http.Request) {
	s, ok := a.keysSession(w, r)
	if !ok {
		return
	}
	defer a.lockKeys(s.Sub)()
	k, wasOnServer := a.keysToChange(r, s)
	apps := a.keyApps(s, k.Keys)
	i, failed, ok := a.changeKeys(r, apps, &k)
	switch {
	case !ok:
		http.Error(w, "Unknown app or action.", http.StatusBadRequest)
	case failed != "":
		apps[i].Error = failed
		a.renderKeys(w, s, apps, k.OnServer, http.StatusUnprocessableEntity)
	default:
		if err := a.writeKeys(w, s, k, wasOnServer); err != nil {
			log.Printf("keys: save: %v", err)
			http.Error(w, "Your keys couldn't be saved. Try again later.", http.StatusInternalServerError)
			return
		}
		at := "#where" // back at the form that was sent
		if i >= 0 {
			at = "#app-" + apps[i].Key
		}
		http.Redirect(w, r, "/keys"+at, http.StatusSeeOther)
	}
}

// commandKey runs a key command from the search pill. It answers with the
// command list, updated, or with why the key wasn't saved, as text.
func (a *app) commandKey(w http.ResponseWriter, r *http.Request) {
	var s session
	if !a.readCookie(r, sessionCookie, &s) || s.Sub == "" {
		http.Error(w, "Log in again to change your keys.", http.StatusUnauthorized)
		return
	}
	defer a.lockKeys(s.Sub)()
	k, wasOnServer := a.keysToChange(r, s)
	apps := a.keyApps(s, k.Keys)
	i, failed, ok := a.changeKeys(r, apps, &k)
	switch {
	case !ok:
		http.Error(w, "Unknown app or action.", http.StatusBadRequest)
		return
	case failed != "":
		http.Error(w, failed, http.StatusUnprocessableEntity)
		return
	}
	if err := a.writeKeys(w, s, k, wasOnServer); err != nil {
		log.Printf("keys: save: %v", err)
		http.Error(w, "Your keys couldn't be saved. Try again later.", http.StatusInternalServerError)
		return
	}
	list := commandList{Items: a.commands(s, k)}
	switch r.PostFormValue("action") {
	case "save":
		list.Message = apps[i].Link.Title + " now uses your key."
	case "remove":
		list.Message = apps[i].Link.Title + " uses the shared key again."
	case "server":
		list.Message = "Your keys are now on all your devices."
	case "browser":
		list.Message = "Your keys are now in this browser only."
	}
	if err := pageTmpl.ExecuteTemplate(w, "commands.html", list); err != nil {
		log.Printf("render commands: %v", err)
	}
}

// keysToChange returns a copy of the user's keys, and whether they're on the
// server now.
func (a *app) keysToChange(r *http.Request, s session) (userKeys, bool) {
	k := a.readKeys(r, s)
	k.Keys = maps.Clone(k.Keys)
	if k.Keys == nil {
		k.Keys = map[string]string{}
	}
	return k, k.OnServer
}

// changeKeys applies a posted key form to k: save an app's key if the app
// accepts it, or remove it (app, action save or remove, key); or keep all
// keys on the server or in this browser (action server or browser). It
// returns which of apps the form is for (-1 for none), why the key wasn't
// saved ("" if it was), and false if the form is invalid.
func (a *app) changeKeys(r *http.Request, apps []keyApp, k *userKeys) (i int, failed string, ok bool) {
	switch action := r.PostFormValue("action"); action {
	case "server", "browser":
		if a.store == nil || len(apps) == 0 {
			return -1, "", false
		}
		k.OnServer = action == "server"
		return -1, "", true
	}
	i = slices.IndexFunc(apps, func(ka keyApp) bool { return ka.Key == r.PostFormValue("app") })
	if i < 0 {
		return -1, "", false
	}
	l := apps[i].Link
	switch r.PostFormValue("action") {
	case "save":
		key := strings.TrimSpace(r.PostFormValue("key"))
		if failed := a.checkKey(r.Context(), l, key, k.Keys); failed != "" {
			return i, failed, true
		}
		k.Keys[l.URL] = key
	case "remove":
		delete(k.Keys, l.URL)
	default:
		return -1, "", false
	}
	return i, "", true
}

// checkKey returns why key can't be saved for l, or "" if it can: it must
// fit in the cookie, and the app must accept it.
func (a *app) checkKey(ctx context.Context, l config.Link, key string, keys map[string]string) string {
	// Visible ASCII only: keys go into request headers.
	if key == "" || len(key) > maxKeyLen || strings.ContainsFunc(key, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return fmt.Sprintf("Paste a key of up to %d characters, without spaces.", maxKeyLen)
	}
	size := len(l.URL) + len(key)
	for u, k := range keys {
		if u != l.URL {
			size += len(u) + len(k)
		}
	}
	if size > maxKeysBytes {
		return "There's no room for this key next to your others. Remove one first."
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	if _, err := a.provider(l, map[string]string{l.URL: key}).Search(ctx, "homenclature"); err != nil {
		log.Printf("keys: %s: %v", l.Title, err)
		return l.Title + " didn't accept this key, or couldn't be reached."
	}
	return ""
}

func (a *app) renderKeys(w http.ResponseWriter, s session, apps []keyApp, onServer bool, status int) {
	data := struct {
		Name          string
		Apps          []keyApp
		OnServer      bool // the keys are kept on the server
		CanMove       bool // the user can choose where their keys are kept
		Background    string
		PhotoInterval int
	}{s.Name, apps, onServer, a.store != nil && len(apps) > 0, a.bg.mode, a.bg.photoInterval()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTmpl.ExecuteTemplate(w, "keys.html", data); err != nil {
		log.Printf("render keys: %v", err)
	}
}
