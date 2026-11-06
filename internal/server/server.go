// Package server serves the homepage: OIDC login, the links page, icons and
// search inside apps.
package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
	"github.com/NiekKeijzer/homenclature/internal/search"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type app struct {
	cfg       *config.Config
	aead      cipher.AEAD // encrypts cookies
	secure    bool        // set the Secure flag on cookies (HTTPS)
	icons     *iconProxy
	providers map[string]search.Provider // by searchKey
	store     *keyStore                  // personal keys kept on the server; nil if off
	keyLocks  sync.Map                   // by sub: a *sync.Mutex, see lockKeys
	bg        background
	oauth     *oauth2.Config
	verifier  *oidc.IDTokenVerifier
}

type env struct {
	configPath, listen, baseURL    string
	issuer, clientID, clientSecret string
	sessionSecret                  string
	dataDir                        string // for the key store; empty: off
}

// Run reads settings from env vars, loads the config and serves until the
// server fails.
func Run() error {
	e, err := readEnv()
	if err != nil {
		return err
	}
	cfg, err := config.Load(e.configPath)
	if err != nil {
		return fmt.Errorf("config %s: %w", e.configPath, err)
	}
	a := &app{
		cfg:    cfg,
		aead:   newAEAD(e.sessionSecret),
		secure: strings.HasPrefix(e.baseURL, "https://"),
		icons:  newIconProxy(cfg),
	}
	if a.providers, err = newProviders(cfg); err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if a.store, err = newKeyStore(e.dataDir, e.sessionSecret); err != nil {
		return fmt.Errorf("HOMENCLATURE_DATA_DIR: %w", err)
	}
	if a.bg, err = newBackground(cfg); err != nil {
		return fmt.Errorf("background: %w", err)
	}
	if err := a.setupOIDC(context.Background(), e); err != nil {
		return fmt.Errorf("oidc: %w", err)
	}

	srv := &http.Server{Addr: e.listen, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on %s", e.listen)
	return srv.ListenAndServe()
}

func readEnv() (env, error) {
	e := env{
		configPath:    getenv("HOMENCLATURE_CONFIG", "config.toml"),
		listen:        getenv("HOMENCLATURE_LISTEN", ":8000"),
		baseURL:       strings.TrimSuffix(os.Getenv("HOMENCLATURE_URL"), "/"),
		issuer:        os.Getenv("HOMENCLATURE_OIDC_ISSUER"),
		clientID:      os.Getenv("HOMENCLATURE_OIDC_CLIENT_ID"),
		clientSecret:  os.Getenv("HOMENCLATURE_OIDC_CLIENT_SECRET"),
		sessionSecret: os.Getenv("HOMENCLATURE_SESSION_SECRET"),
		dataDir:       os.Getenv("HOMENCLATURE_DATA_DIR"),
	}
	var errs []error
	for _, v := range []struct{ name, value string }{
		{"HOMENCLATURE_URL", e.baseURL},
		{"HOMENCLATURE_OIDC_ISSUER", e.issuer},
		{"HOMENCLATURE_OIDC_CLIENT_ID", e.clientID},
		{"HOMENCLATURE_OIDC_CLIENT_SECRET", e.clientSecret},
	} {
		if v.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", v.name))
		}
	}
	if len(e.sessionSecret) < 32 {
		errs = append(errs, errors.New("HOMENCLATURE_SESSION_SECRET must be at least 32 characters"))
	}
	return e, errors.Join(errs...)
}

func getenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// newAEAD derives an AES-256-GCM cipher from the session secret.
func newAEAD(secret string) cipher.AEAD {
	key := sha256.Sum256([]byte(secret))
	block, _ := aes.NewCipher(key[:]) // can't fail: key is 32 bytes
	aead, _ := cipher.NewGCM(block)   // can't fail for AES
	return aead
}
