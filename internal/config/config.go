// Package config loads the dashboard config. See config.example.toml.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"

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
	Title       string `toml:"title"`
	URL         string `toml:"url"`
	Icon        string `toml:"icon"` // dashboard-icons name or URL
	Description string `toml:"description"`
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
			}
		}
	}
	return errors.Join(errs...)
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
