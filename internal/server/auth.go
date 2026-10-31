package server

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "homenclature_session"
	loginCookie   = "homenclature_login"
	sessionTTL    = 24 * time.Hour
	loginTTL      = 10 * time.Minute
)

type session struct {
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

// loginState carries the OIDC flow values from /auth/login to /auth/callback.
type loginState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"` // PKCE
}

func (a *app) setupOIDC(ctx context.Context, e env) error {
	provider, err := oidc.NewProvider(ctx, e.issuer)
	if err != nil {
		return err
	}
	a.oauth = &oauth2.Config{
		ClientID:     e.clientID,
		ClientSecret: e.clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  e.baseURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: e.clientID})
	return nil
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	ls := loginState{State: rand.Text(), Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier()}
	a.setCookie(w, loginCookie, ls, loginTTL)
	url := a.oauth.AuthCodeURL(ls.State, oidc.Nonce(ls.Nonce), oauth2.S256ChallengeOption(ls.Verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

func (a *app) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var ls loginState
	if !a.readCookie(r, loginCookie, &ls) || q.Get("state") != ls.State {
		http.Error(w, "Login expired or invalid. Please try again.", http.StatusBadRequest)
		return
	}
	a.clearCookie(w, loginCookie)
	if e := q.Get("error"); e != "" {
		http.Error(w, "Login failed: "+e, http.StatusForbidden)
		return
	}

	token, err := a.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(ls.Verifier))
	if err != nil {
		log.Printf("oidc exchange: %v", err)
		http.Error(w, "Login failed.", http.StatusBadGateway)
		return
	}
	raw, _ := token.Extra("id_token").(string)
	idToken, err := a.verifier.Verify(r.Context(), raw)
	if err != nil || idToken.Nonce != ls.Nonce {
		log.Printf("oidc verify: %v (nonce match: %t)", err, err == nil && idToken.Nonce == ls.Nonce)
		http.Error(w, "Login failed.", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Groups            []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		log.Printf("oidc claims: %v", err)
		http.Error(w, "Login failed.", http.StatusUnauthorized)
		return
	}

	// Greet by first name: "Niek Keijzer" -> "Niek".
	name := claims.PreferredUsername
	if f := strings.Fields(claims.Name); len(f) > 0 {
		name = f[0]
	}
	a.setCookie(w, sessionCookie, session{Name: name, Groups: claims.Groups}, sessionTTL)
	http.Redirect(w, r, "/", http.StatusFound)
}
