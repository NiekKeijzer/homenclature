package search

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// mealie searches recipes. Recipe URLs contain the group slug, which is
// looked up once.
type mealie struct {
	base

	mu        sync.Mutex
	groupSlug string
}

func newMealie(b base, s *config.Search) Provider {
	b.header = http.Header{"Authorization": {"Bearer " + s.Token}}
	return &mealie{base: b}
}

func (m *mealie) Search(ctx context.Context, q string) (Results, error) {
	group, err := m.group(ctx)
	if err != nil {
		return Results{}, err
	}
	var resp struct {
		Total int `json:"total"`
		Items []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			TotalTime   string `json:"totalTime"`
			Categories  []struct {
				Name string `json:"name"`
			} `json:"recipeCategory"`
		} `json:"items"`
	}
	err = m.getJSON(ctx, "/api/recipes", url.Values{"search": {q}, "perPage": {strconv.Itoa(m.limit)}}, &resp)
	if err != nil {
		return Results{}, err
	}
	home := m.web + "/g/" + url.PathEscape(group)
	out := Results{Total: resp.Total, SearchURL: home + "?search=" + queryEscape(q)}
	for _, r := range resp.Items {
		var category string
		if len(r.Categories) > 0 {
			category = r.Categories[0].Name
		}
		detail := join(category, r.TotalTime)
		if detail == "" {
			detail = r.Description
		}
		out.Items = append(out.Items, Result{Title: r.Name, Detail: detail, URL: home + "/r/" + url.PathEscape(r.Slug), Thumb: r.ID})
	}
	return out, nil
}

func (m *mealie) group(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groupSlug == "" {
		var user struct {
			GroupSlug string `json:"groupSlug"`
		}
		if err := m.getJSON(ctx, "/api/users/self", nil, &user); err != nil {
			return "", err
		}
		m.groupSlug = user.GroupSlug
	}
	return m.groupSlug, nil
}

func (m *mealie) Thumbnail(ctx context.Context, id string) (Image, error) {
	if !uuid.MatchString(id) {
		return Image{}, ErrInvalidID
	}
	return m.image(ctx, "/api/media/recipes/"+id+"/images/min-original.webp")
}
