package server

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
	"github.com/NiekKeijzer/homenclature/internal/search"
)

// Personal keys: people can search an app with their own API key instead of
// the shared one (token_env). The keys stay in an encrypted cookie in their
// browser, tied to their OIDC subject; the server stores nothing.

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
	Sub  string            `json:"sub"`  // whose keys they are
	Keys map[string]string `json:"keys"` // by link URL
}

// readKeys returns the user's own keys by link URL; nil if they have none.
// Keys of someone else who used this browser are ignored.
func (a *app) readKeys(r *http.Request, s session) map[string]string {
	var k personalKeys
	if s.Sub == "" || !a.readCookie(r, keysCookie, &k) || k.Sub != s.Sub {
		return nil
	}
	return k.Keys
}

// renewKeys gives the user's keys a fresh TTL, so they only expire after
// keysTTL without a login.
func (a *app) renewKeys(w http.ResponseWriter, r *http.Request, s session) {
	if keys := a.readKeys(r, s); len(keys) > 0 {
		a.setCookie(w, keysCookie, personalKeys{s.Sub, keys}, keysTTL)
	}
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
	a.renderKeys(w, s, a.keyApps(s, a.readKeys(r, s)), http.StatusOK)
}

// saveKey saves or removes the user's own key for an app. A key is only
// saved if the app accepts it.
func (a *app) saveKey(w http.ResponseWriter, r *http.Request) {
	s, ok := a.keysSession(w, r)
	if !ok {
		return
	}
	keys := maps.Clone(a.readKeys(r, s))
	if keys == nil {
		keys = map[string]string{}
	}
	apps := a.keyApps(s, keys)
	i := slices.IndexFunc(apps, func(k keyApp) bool { return k.Key == r.PostFormValue("app") })
	if i < 0 {
		http.Error(w, "Unknown app.", http.StatusBadRequest)
		return
	}
	l := apps[i].Link
	switch r.PostFormValue("action") {
	case "save":
		key := strings.TrimSpace(r.PostFormValue("key"))
		if msg := a.checkKey(r.Context(), l, key, keys); msg != "" {
			apps[i].Error = msg
			a.renderKeys(w, s, apps, http.StatusUnprocessableEntity)
			return
		}
		keys[l.URL] = key
	case "remove":
		delete(keys, l.URL)
	default:
		http.Error(w, "Unknown action.", http.StatusBadRequest)
		return
	}
	a.setCookie(w, keysCookie, personalKeys{s.Sub, keys}, keysTTL)
	http.Redirect(w, r, "/keys#app-"+apps[i].Key, http.StatusSeeOther) // back at the app's form
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

func (a *app) renderKeys(w http.ResponseWriter, s session, apps []keyApp, status int) {
	data := struct {
		Name          string
		Apps          []keyApp
		Background    string
		PhotoInterval int
	}{s.Name, apps, a.bg.mode, a.bg.photoInterval()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTmpl.ExecuteTemplate(w, "keys.html", data); err != nil {
		log.Printf("render keys: %v", err)
	}
}
