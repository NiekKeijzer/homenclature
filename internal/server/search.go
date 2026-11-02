package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NiekKeijzer/homenclature/internal/config"
	"github.com/NiekKeijzer/homenclature/internal/search"
)

const (
	searchTimeout  = 3 * time.Second
	minSearchRunes = 2
)

// searchKey identifies a link with search enabled, in URLs and a.providers.
func searchKey(l config.Link) string {
	s := *l.Search
	s.Token = "" // keys end up in URLs
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%#v", l.URL, s))
	return hex.EncodeToString(sum[:8])
}

func newProviders(cfg *config.Config) (map[string]search.Provider, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	providers := map[string]search.Provider{}
	for _, d := range cfg.Dashboards {
		for _, s := range d.Sections {
			for _, l := range s.Links {
				if l.Search == nil {
					continue
				}
				p, err := search.New(l, client)
				if err != nil {
					return nil, err
				}
				providers[searchKey(l)] = p
			}
		}
	}
	return providers, nil
}

// appResults is one app's section in the search results.
type appResults struct {
	Key  string
	Link config.Link
	search.Results
}

// Count is the number shown beside the app's name.
func (r appResults) Count() string {
	if r.Total == 0 && len(r.Items) > 0 {
		return strconv.Itoa(len(r.Items)) + "+" // the app doesn't say how many
	}
	return strconv.Itoa(r.Total)
}

// OpenURL is the search in the app itself, or the app if it has no search page.
func (r appResults) OpenURL() string {
	if r.SearchURL != "" {
		return r.SearchURL
	}
	return r.Link.URL
}

// OpenText labels OpenURL. The page adds " in <app>" where there's room.
func (r appResults) OpenText() string {
	if r.Total > len(r.Items) {
		return "All " + strconv.Itoa(r.Total)
	}
	return "Open"
}

// Rest labels the last tile of a photo grid, if the app has more.
func (r appResults) Rest() string {
	switch {
	case r.Total > len(r.Items):
		return "+" + strconv.Itoa(r.Total-len(r.Items))
	case r.Total == 0 && len(r.Items) > 0:
		return "More"
	}
	return ""
}

// searchable returns the links on the user's dashboard with search enabled,
// each once.
func (a *app) searchable(groups []string) []appResults {
	var out []appResults
	for _, sec := range a.cfg.Sections(groups) {
		for _, l := range sec.Links {
			if l.Search == nil {
				continue
			}
			key := searchKey(l)
			if !slices.ContainsFunc(out, func(r appResults) bool { return r.Key == key }) {
				out = append(out, appResults{Key: key, Link: l})
			}
		}
	}
	return out
}

// search renders results from inside the apps on the user's dashboard, plus
// the chips to filter them by app. Links are filtered in the browser.
func (a *app) search(w http.ResponseWriter, r *http.Request) {
	var s session
	if !a.readCookie(r, sessionCookie, &s) {
		w.Header().Set("HX-Redirect", "/auth/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var data struct{ Apps []appResults }
	if q := strings.TrimSpace(r.URL.Query().Get("q")); utf8.RuneCountInString(q) >= minSearchRunes {
		data.Apps = a.searchApps(r.Context(), s.Groups, q)
	}
	if err := pageTmpl.ExecuteTemplate(w, "results.html", data); err != nil {
		log.Printf("render results: %v", err)
	}
}

// searchApps searches the user's apps in parallel. Apps without results are
// left out, and so are apps that failed (the error is logged).
func (a *app) searchApps(ctx context.Context, groups []string, q string) []appResults {
	apps := a.searchable(groups)
	var wg sync.WaitGroup
	for i := range apps {
		res := &apps[i]
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, searchTimeout)
			defer cancel()
			results, err := a.providers[res.Key].Search(ctx, q)
			if err != nil {
				log.Printf("search %s: %v", res.Link.Title, err)
				return
			}
			res.Results = results
		})
	}
	wg.Wait()

	// Jellyfin links to a search in Seerr (seerr_url), but only for people who
	// can't search Seerr here: they get its own section instead.
	searchesSeerr := slices.ContainsFunc(apps, func(r appResults) bool { return r.Link.Search.Type == "seerr" })
	for i := range apps {
		if seerr := apps[i].Link.Search.SeerrURL; seerr != "" && !searchesSeerr {
			apps[i].More = append(apps[i].More, search.Link{Text: "Request it in Seerr", URL: search.SeerrSearchURL(seerr, q)})
		}
	}
	return slices.DeleteFunc(apps, func(r appResults) bool { return len(r.Items) == 0 })
}

// searchThumb proxies a result thumbnail, using the app's token. Only apps on
// the user's dashboard are allowed.
func (a *app) searchThumb(w http.ResponseWriter, r *http.Request) {
	var s session
	key := r.PathValue("key")
	if !a.readCookie(r, sessionCookie, &s) || !slices.ContainsFunc(a.searchable(s.Groups), func(r appResults) bool { return r.Key == key }) {
		http.NotFound(w, r)
		return
	}
	img, err := a.providers[key].Thumbnail(r.Context(), r.PathValue("id"))
	if err != nil {
		if !errors.Is(err, search.ErrInvalidID) {
			log.Printf("search thumbnail: %v", err)
		}
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", img.ContentType)
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Write(img.Body)
}
