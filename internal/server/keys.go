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

// saveKey handles the forms on /keys.
func (a *app) saveKey(w http.ResponseWriter, r *http.Request) {
	s, ok := a.keysSession(w, r)
	if !ok {
		return
	}
	keys := a.keysToChange(r, s)
	apps := a.keyApps(s, keys)
	i, failed := a.changeKey(r, apps, keys)
	switch {
	case i < 0:
		http.Error(w, "Unknown app or action.", http.StatusBadRequest)
	case failed != "":
		apps[i].Error = failed
		a.renderKeys(w, s, apps, http.StatusUnprocessableEntity)
	default:
		a.setCookie(w, keysCookie, personalKeys{s.Sub, keys}, keysTTL)
		http.Redirect(w, r, "/keys#app-"+apps[i].Key, http.StatusSeeOther) // back at the app's form
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
	keys := a.keysToChange(r, s)
	apps := a.keyApps(s, keys)
	i, failed := a.changeKey(r, apps, keys)
	switch {
	case i < 0:
		http.Error(w, "Unknown app or action.", http.StatusBadRequest)
		return
	case failed != "":
		http.Error(w, failed, http.StatusUnprocessableEntity)
		return
	}
	a.setCookie(w, keysCookie, personalKeys{s.Sub, keys}, keysTTL)
	title := apps[i].Link.Title
	list := commandList{a.commands(s, keys), title + " now uses your key."}
	if r.PostFormValue("action") == "remove" {
		list.Message = title + " uses the shared key again."
	}
	if err := pageTmpl.ExecuteTemplate(w, "commands.html", list); err != nil {
		log.Printf("render commands: %v", err)
	}
}

// keysToChange returns a copy of the user's keys.
func (a *app) keysToChange(r *http.Request, s session) map[string]string {
	if keys := maps.Clone(a.readKeys(r, s)); keys != nil {
		return keys
	}
	return map[string]string{}
}

// changeKey applies a posted key form (app, action, key) to keys: save the
// key if the app accepts it, or remove it. It returns which of apps the form
// is for, -1 if it's invalid, and why the key wasn't saved ("" if it was).
func (a *app) changeKey(r *http.Request, apps []keyApp, keys map[string]string) (i int, failed string) {
	i = slices.IndexFunc(apps, func(k keyApp) bool { return k.Key == r.PostFormValue("app") })
	if i < 0 {
		return -1, ""
	}
	l := apps[i].Link
	switch r.PostFormValue("action") {
	case "save":
		key := strings.TrimSpace(r.PostFormValue("key"))
		if failed := a.checkKey(r.Context(), l, key, keys); failed != "" {
			return i, failed
		}
		keys[l.URL] = key
	case "remove":
		delete(keys, l.URL)
	default:
		return -1, ""
	}
	return i, ""
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
