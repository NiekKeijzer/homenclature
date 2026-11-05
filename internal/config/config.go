// Package config loads the dashboard config. See config.example.toml.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultDashboardEnv names the dashboard shown to users who match no group
// dashboard. Unset means those users see nothing.
const DefaultDashboardEnv = "HOMENCLATURE_DEFAULT_DASHBOARD"

type Config struct {
	// Dashboards are keyed by OIDC group name.
	Dashboards map[string]Dashboard `toml:"dashboards"`

	order            []string // dashboard names in file order
	defaultDashboard string
}

type Dashboard struct {
	Sections []Section `toml:"sections"`
}

type Section struct {
	Title  string `toml:"title"`
	Layout string `toml:"layout"` // "list" (default) or "cards"
	Links  []Link `toml:"links"`
}

type Link struct {
	Title       string  `toml:"title"`
	URL         string  `toml:"url"`
	Icon        string  `toml:"icon"` // dashboard-icons name or URL
	Description string  `toml:"description"`
	Search      *Search `toml:"search"` // nil: the app isn't searched
}

// SearchTypes are the apps that can be searched.
var SearchTypes = []string{"home-assistant", "immich", "jellyfin", "mealie", "paperless", "seerr"}

// JellyfinTypes are the item types a Jellyfin search can include.
var JellyfinTypes = []string{
	"Audio", "AudioBook", "Book", "BoxSet", "Episode", "Movie", "MusicAlbum",
	"MusicArtist", "MusicVideo", "Playlist", "Season", "Series", "Video",
}

// MaxSearchLimit caps Search.Limit.
const MaxSearchLimit = 50

// Search opts a link in to searching inside the app, through its API.
type Search struct {
	Type     string `toml:"type"`      // one of SearchTypes
	TokenEnv string `toml:"token_env"` // env var holding the API token
	APIURL   string `toml:"api_url"`   // where the server reaches the API; default: the link URL
	Limit    int    `toml:"limit"`     // results shown; 0: the app's default

	Types    []string `toml:"types"`     // jellyfin: item types (JellyfinTypes); empty: a default set
	SeerrURL string   `toml:"seerr_url"` // jellyfin: also link to a Seerr search
	Domains  []string `toml:"domains"`   // home-assistant: entity domains, e.g. "light"; empty: all

	Token string `toml:"-"` // read from TokenEnv by Load
}

// Load reads and validates a config file, plus DefaultDashboardEnv. Unknown
// keys are an error, so typos don't go unnoticed.
func Load(path string) (*Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return nil, err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("unknown keys: %v", keys)
	}
	for _, k := range md.Keys() {
		if len(k) >= 2 && k[0] == "dashboards" && !slices.Contains(c.order, k[1]) {
			c.order = append(c.order, k[1])
		}
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if name := os.Getenv(DefaultDashboardEnv); name != "" {
		if _, ok := c.Dashboards[name]; !ok {
			return nil, fmt.Errorf("%s: dashboard %q not found", DefaultDashboardEnv, name)
		}
		c.defaultDashboard = name
	}
	return &c, nil
}

func (c *Config) validate() error {
	if len(c.Dashboards) == 0 {
		return errors.New("no dashboards defined")
	}
	var errs []error
	for _, name := range c.order {
		for i, s := range c.Dashboards[name].Sections {
			at := fmt.Sprintf("dashboards.%s.sections[%d]", name, i)
			if s.Title == "" {
				errs = append(errs, fmt.Errorf("%s: title is required", at))
			}
			if s.Layout != "" && s.Layout != "list" && s.Layout != "cards" {
				errs = append(errs, fmt.Errorf("%s: layout must be list or cards, got %q", at, s.Layout))
			}
			for j, l := range s.Links {
				at := fmt.Sprintf("%s.links[%d]", at, j)
				if l.Title == "" {
					errs = append(errs, fmt.Errorf("%s: title is required", at))
				}
				if u, err := url.Parse(l.URL); err != nil || u.Scheme == "" {
					errs = append(errs, fmt.Errorf("%s: url must be absolute, got %q", at, l.URL))
				}
				if l.Search != nil {
					errs = append(errs, l.Search.load(at+".search")...)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// load validates s and reads its token from the environment.
func (s *Search) load(at string) []error {
	var errs []error
	if !slices.Contains(SearchTypes, s.Type) {
		errs = append(errs, fmt.Errorf("%s: type must be one of %s, got %q", at, strings.Join(SearchTypes, ", "), s.Type))
	}
	if s.TokenEnv == "" {
		errs = append(errs, fmt.Errorf("%s: token_env is required", at))
	} else if s.Token = os.Getenv(s.TokenEnv); s.Token == "" {
		errs = append(errs, fmt.Errorf("%s: env var %s (token_env) is not set", at, s.TokenEnv))
	}
	for _, u := range []struct{ key, value string }{{"api_url", s.APIURL}, {"seerr_url", s.SeerrURL}} {
		if u.value != "" && !isHTTPURL(u.value) {
			errs = append(errs, fmt.Errorf("%s: %s must be an http(s) URL, got %q", at, u.key, u.value))
		}
	}
	if s.Limit < 0 || s.Limit > MaxSearchLimit {
		errs = append(errs, fmt.Errorf("%s: limit must be between 1 and %d, got %d", at, MaxSearchLimit, s.Limit))
	}
	for _, t := range s.Types {
		if !slices.Contains(JellyfinTypes, t) {
			errs = append(errs, fmt.Errorf("%s: types must be from %s, got %q", at, strings.Join(JellyfinTypes, ", "), t))
		}
	}
	for _, o := range []struct {
		key, onlyFor string
		set          bool
	}{
		{"types", "jellyfin", len(s.Types) > 0},
		{"seerr_url", "jellyfin", s.SeerrURL != ""},
		{"domains", "home-assistant", len(s.Domains) > 0},
	} {
		if o.set && s.Type != o.onlyFor {
			errs = append(errs, fmt.Errorf("%s: %s only applies to type %s", at, o.key, o.onlyFor))
		}
	}
	return errs
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

// Links returns every link in the file, in file order.
func (c *Config) Links() []Link {
	var out []Link
	for _, name := range c.order {
		for _, s := range c.Dashboards[name].Sections {
			out = append(out, s.Links...)
		}
	}
	return out
}

// Sections returns what a user in the given groups sees: their group
// dashboards merged in file order, or the default dashboard if none match.
// Sections with the same title are merged (the first layout set wins), and
// within a section links with the same URL appear once.
func (c *Config) Sections(groups []string) []Section {
	var names []string
	for _, name := range c.order {
		if slices.Contains(groups, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 && c.defaultDashboard != "" {
		names = []string{c.defaultDashboard}
	}

	var out []Section
	for _, name := range names {
		for _, s := range c.Dashboards[name].Sections {
			i := slices.IndexFunc(out, func(o Section) bool { return o.Title == s.Title })
			if i < 0 {
				out = append(out, Section{Title: s.Title})
				i = len(out) - 1
			}
			if out[i].Layout == "" {
				out[i].Layout = s.Layout
			}
			for _, l := range s.Links {
				if !slices.ContainsFunc(out[i].Links, func(o Link) bool { return o.URL == l.URL }) {
					out[i].Links = append(out[i].Links, l)
				}
			}
		}
	}
	return out
}
