package search

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// homeAssistant searches entities by name or entity ID, and shows their state.
type homeAssistant struct {
	base
	domains []string // empty: all
}

func newHomeAssistant(b base, s *config.Search) Provider {
	b.header = http.Header{"Authorization": {"Bearer " + s.Token}}
	return &homeAssistant{base: b, domains: s.Domains}
}

func (h *homeAssistant) Search(ctx context.Context, q string) (Results, error) {
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
			Unit         string `json:"unit_of_measurement"`
		} `json:"attributes"`
	}
	if err := h.getJSON(ctx, "/api/states", nil, &states); err != nil {
		return Results{}, err
	}
	q = strings.ToLower(q)
	var matches []Result
	for _, s := range states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		if len(h.domains) > 0 && !slices.Contains(h.domains, domain) {
			continue
		}
		name := s.Attributes.FriendlyName
		if name == "" {
			name = s.EntityID
		}
		if !strings.Contains(strings.ToLower(name), q) && !strings.Contains(s.EntityID, q) {
			continue
		}
		matches = append(matches, Result{
			Title:  name,
			Detail: capitalize(strings.ReplaceAll(domain, "_", " ")),
			Value:  strings.TrimSpace(capitalize(s.State) + " " + s.Attributes.Unit),
			URL:    h.web + "/history?entity_id=" + queryEscape(s.EntityID),
		})
	}
	slices.SortStableFunc(matches, func(a, b Result) int { return strings.Compare(a.Title, b.Title) })
	return Results{Items: matches[:min(len(matches), h.limit)], Total: len(matches)}, nil
}

// Thumbnail always fails: entities have no thumbnails.
func (h *homeAssistant) Thumbnail(context.Context, string) (Image, error) {
	return Image{}, ErrInvalidID
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
