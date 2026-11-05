// Package search queries apps (Home Assistant, Immich, Jellyfin, Mealie,
// Paperless, Seerr) through their APIs. It only reads; it never changes
// anything in an app.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// DefaultLimit is how many results an app shows, unless its config sets
// limit. Immich has its own, DefaultImmichLimit.
const DefaultLimit = 5

// ErrInvalidID is returned for thumbnail IDs the app could never have produced.
var ErrInvalidID = errors.New("invalid id")

// uuid matches the thumbnail IDs of apps that use UUIDs (Immich, Mealie).
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Result is one hit inside an app.
type Result struct {
	Title  string
	Detail string // e.g. year, date or description
	Value  string // shown on the right, e.g. a sensor reading
	URL    string // deep link into the app
	Thumb  string // ID for Provider.Thumbnail; empty if none
}

// Link is an extra link shown below an app's results.
type Link struct {
	Text, URL string
}

// Results is what an app found for a query.
type Results struct {
	Items     []Result
	Total     int    // matches in the app (at least len(Items)); 0 if the app doesn't say
	SearchURL string // the same search in the app itself; empty if it has no search page
	More      []Link // shown below the results
}

// Image is a fetched thumbnail.
type Image struct {
	ContentType string
	Body        []byte
}

// Provider searches one app.
type Provider interface {
	Search(ctx context.Context, query string) (Results, error)
	// Thumbnail fetches the image for a Result.Thumb. It validates id, so
	// only thumbnails can be fetched with the app's token.
	Thumbnail(ctx context.Context, id string) (Image, error)
}

// apps builds each search type's provider. To add an app: a file with its
// provider and constructor, an entry here, and its type in config.SearchTypes.
var apps = map[string]func(b base, s *config.Search) Provider{
	"home-assistant": newHomeAssistant,
	"immich":         newImmich,
	"jellyfin":       newJellyfin,
	"mealie":         newMealie,
	"paperless":      newPaperless,
	"seerr":          newSeerr,
}

// New returns the provider for a link with search enabled.
func New(link config.Link, client *http.Client) (Provider, error) {
	s := link.Search
	newApp, ok := apps[s.Type]
	if !ok {
		return nil, fmt.Errorf("unknown search type %q", s.Type)
	}
	b := base{
		apiClient: apiClient{http: client, base: strings.TrimSuffix(link.URL, "/")},
		web:       strings.TrimSuffix(link.URL, "/"),
		title:     link.Title,
		limit:     s.Limit,
	}
	if s.APIURL != "" {
		b.base = strings.TrimSuffix(s.APIURL, "/")
	}
	if b.limit == 0 {
		b.limit = DefaultLimit
	}
	return newApp(b, s), nil
}

// base is what every provider has.
type base struct {
	apiClient
	web   string // the app's URL for people (the link URL), for deep links
	title string // the link title
	limit int
}

// apiClient makes authenticated requests to an app's API.
type apiClient struct {
	http   *http.Client
	base   string
	header http.Header // credentials
}

func (c apiClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	for k, v := range c.header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
	return resp, nil
}

func (c apiClient) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	if query != nil {
		path += "?" + query.Encode()
	}
	return c.json(ctx, http.MethodGet, path, nil, v)
}

func (c apiClient) json(ctx context.Context, method, path string, body, v any) error {
	resp, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

const maxImageSize = 2 << 20

func (c apiClient) image(ctx context.Context, path string) (Image, error) {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return Image{}, fmt.Errorf("GET %s: content type %q is not an image", path, ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return Image{}, err
	}
	if len(body) > maxImageSize {
		return Image{}, fmt.Errorf("GET %s: image larger than %d bytes", path, maxImageSize)
	}
	return Image{ContentType: ct, Body: body}, nil
}

// queryEscape escapes s for a URL query, with spaces as %20: some app
// frontends don't read "+" as a space.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// date shortens an ISO timestamp to "2 January 2006"; other input is
// returned as is.
func date(s string) string {
	if len(s) < 10 {
		return s
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return s
	}
	return t.Format("2 January 2006")
}

// join joins the non-empty parts with " · ".
func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}
