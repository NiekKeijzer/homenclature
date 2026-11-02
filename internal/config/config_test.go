package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func load(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// summary renders sections as "Title: link, link" for easy comparison.
func summary(sections []Section) []string {
	var out []string
	for _, s := range sections {
		var titles []string
		for _, l := range s.Links {
			titles = append(titles, l.Title)
		}
		out = append(out, s.Title+": "+strings.Join(titles, ", "))
	}
	return out
}

func TestExampleConfig(t *testing.T) {
	t.Setenv(DefaultDashboardEnv, "family")
	c, err := Load("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	got := summary(c.Sections([]string{"parents", "family"}))
	want := []string{
		"Media: Jellyfin, Seerr, Immich",
		"Home: Home Assistant, Mealie, Paperless",
		"Admin: Proxmox, Authentik",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]struct{ config, want string }{
		"no dashboards": {``, "no dashboards defined"},
		"unknown key": {`
[[dashboards.kids.sections]]
title = "Media"
[[dashboards.kids.sections.links]]
titel = "Typo"
url = "https://example.com"
`, "unknown keys: [dashboards.kids.sections.links.titel]"},
		"section without title": {`
[[dashboards.kids.sections]]
links = []
`, "dashboards.kids.sections[0]: title is required"},
		"link without title": {`
[[dashboards.kids.sections]]
title = "Media"
[[dashboards.kids.sections.links]]
url = "https://example.com"
`, "dashboards.kids.sections[0].links[0]: title is required"},
		"relative url": {`
[[dashboards.kids.sections]]
title = "Media"
[[dashboards.kids.sections.links]]
title = "Jellyfin"
url = "jellyfin.home"
`, `dashboards.kids.sections[0].links[0]: url must be absolute, got "jellyfin.home"`},
		"unknown layout": {`
[[dashboards.kids.sections]]
title = "Media"
layout = "grid"
`, `dashboards.kids.sections[0]: layout must be list or cards, got "grid"`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDefaultDashboardMissing(t *testing.T) {
	t.Setenv(DefaultDashboardEnv, "guests")
	_, err := load(t, `
[[dashboards.family.sections]]
title = "Home"
`)
	want := `HOMENCLATURE_DEFAULT_DASHBOARD: dashboard "guests" not found`
	if err == nil || err.Error() != want {
		t.Fatalf("got error %v, want %q", err, want)
	}
}

const sectionsConfig = `
[[dashboards.guests.sections]]
title = "Welcome"
[[dashboards.guests.sections.links]]
title = "Guest wifi"
url = "https://wifi.example"

[[dashboards.family.sections]]
title = "Home"
[[dashboards.family.sections.links]]
title = "Home Assistant"
url = "https://ha.example"

[[dashboards.parents.sections]]
title = "Admin"
[[dashboards.parents.sections.links]]
title = "Proxmox"
url = "https://pve.example"

[[dashboards.parents.sections]]
title = "Home"
[[dashboards.parents.sections.links]]
title = "Home Assistant (dupe)"
url = "https://ha.example"
[[dashboards.parents.sections.links]]
title = "Paperless"
url = "https://docs.example"
`

func TestSections(t *testing.T) {
	t.Setenv(DefaultDashboardEnv, "guests")
	c, err := load(t, sectionsConfig)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		groups []string
		want   []string
	}{
		"no groups gets default":       {nil, []string{"Welcome: Guest wifi"}},
		"unknown group gets default":   {[]string{"unknown"}, []string{"Welcome: Guest wifi"}},
		"single group":                 {[]string{"family"}, []string{"Home: Home Assistant"}},
		"merged in file order, dedupe": {[]string{"parents", "family"}, []string{"Home: Home Assistant, Paperless", "Admin: Proxmox"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := summary(c.Sections(tt.groups)); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSectionsLayout(t *testing.T) {
	t.Setenv(DefaultDashboardEnv, "")
	c, err := load(t, `
[[dashboards.family.sections]]
title = "Media"
[[dashboards.family.sections]]
title = "Home"
layout = "list"

[[dashboards.parents.sections]]
title = "Media"
layout = "cards"
[[dashboards.parents.sections]]
title = "Home"
layout = "cards"
`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range c.Sections([]string{"family", "parents"}) {
		got = append(got, s.Title+": "+s.Layout)
	}
	// Media: family sets none, so parents' cards applies. Home: family's list wins.
	if want := []string{"Media: cards", "Home: list"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSectionsWithoutDefault(t *testing.T) {
	t.Setenv(DefaultDashboardEnv, "")
	c, err := load(t, sectionsConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Sections([]string{"unknown"}); got != nil {
		t.Errorf("got %v, want nothing", got)
	}
}
