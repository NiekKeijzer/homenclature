package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

const (
	dashboardIcons = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
	maxIconSize    = 1 << 20
)

// iconURL is where the page loads a link's icon from. Icons are addressed by
// a hash of their config value, so only icons from the config can be fetched.
func iconURL(icon string) string {
	sum := sha256.Sum256([]byte(icon))
	return "/icons/" + hex.EncodeToString(sum[:8])
}

// iconProxy fetches icons on first request and caches them in memory.
type iconProxy struct {
	client  *http.Client
	sources map[string][]string // iconURL -> URLs to try, in order

	mu    sync.Mutex
	cache map[string]cachedIcon
}

type cachedIcon struct {
	contentType string
	body        []byte
}

func newIconProxy(cfg *config.Config) *iconProxy {
	p := &iconProxy{
		client:  &http.Client{Timeout: 10 * time.Second},
		sources: map[string][]string{},
		cache:   map[string]cachedIcon{},
	}
	for _, d := range cfg.Dashboards {
		for _, s := range d.Sections {
			for _, l := range s.Links {
				if l.Icon != "" {
					p.sources[iconURL(l.Icon)] = iconSources(l.Icon)
				}
			}
		}
	}
	return p
}

// iconSources returns the URL itself, or for a dashboard-icons name its SVG
// with the PNG as fallback (not every icon has an SVG).
func iconSources(icon string) []string {
	if strings.Contains(icon, "://") {
		return []string{icon}
	}
	name := url.PathEscape(icon)
	return []string{dashboardIcons + "/svg/" + name + ".svg", dashboardIcons + "/png/" + name + ".png"}
}

func (p *iconProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path
	sources, ok := p.sources[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	p.mu.Lock()
	icon, ok := p.cache[key]
	p.mu.Unlock()
	if !ok {
		var err error
		if icon, err = p.fetch(r.Context(), sources); err != nil {
			log.Printf("icon: %v", err)
			http.NotFound(w, r)
			return
		}
		p.mu.Lock()
		p.cache[key] = icon
		p.mu.Unlock()
	}

	h := w.Header()
	h.Set("Content-Type", icon.contentType)
	h.Set("Cache-Control", "public, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	// SVGs can contain scripts. Never let them run.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Write(icon.body)
}

func (p *iconProxy) fetch(ctx context.Context, sources []string) (cachedIcon, error) {
	var errs []error
	for _, src := range sources {
		icon, err := p.get(ctx, src)
		if err == nil {
			return icon, nil
		}
		errs = append(errs, err)
	}
	return cachedIcon{}, errors.Join(errs...)
}

func (p *iconProxy) get(ctx context.Context, src string) (cachedIcon, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return cachedIcon{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return cachedIcon{}, err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		return cachedIcon{}, fmt.Errorf("%s: status %d, content type %q", src, resp.StatusCode, ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIconSize+1))
	if err != nil {
		return cachedIcon{}, fmt.Errorf("%s: %w", src, err)
	}
	if len(body) > maxIconSize {
		return cachedIcon{}, fmt.Errorf("%s: larger than %d bytes", src, maxIconSize)
	}
	return cachedIcon{contentType: ct, body: body}, nil
}
