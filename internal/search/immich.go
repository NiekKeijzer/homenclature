package search

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// DefaultImmichLimit is how many results Immich shows, unless its config sets
// limit. It shows a grid of photos, so it gets more than DefaultLimit.
const DefaultImmichLimit = 8

// immich does a smart (CLIP) search of photos and videos.
type immich struct {
	base
}

func newImmich(b base, s *config.Search) Provider {
	b.header = http.Header{"X-Api-Key": {s.Token}}
	if s.Limit == 0 {
		b.limit = DefaultImmichLimit
	}
	return &immich{base: b}
}

func (im *immich) Search(ctx context.Context, q string) (Results, error) {
	var resp struct {
		Assets struct {
			Items []struct {
				ID            string `json:"id"`
				FileName      string `json:"originalFileName"`
				LocalDateTime string `json:"localDateTime"`
			} `json:"items"`
			NextPage *string `json:"nextPage"`
		} `json:"assets"`
	}
	body := map[string]any{"query": q, "size": im.limit}
	if err := im.json(ctx, http.MethodPost, "/api/search/smart", body, &resp); err != nil {
		return Results{}, err
	}
	// The web app's search page takes the search as JSON.
	search, _ := json.Marshal(map[string]string{"query": q})
	out := Results{SearchURL: im.web + "/search?query=" + queryEscape(string(search))}
	for _, a := range resp.Assets.Items {
		out.Items = append(out.Items, Result{Title: a.FileName, Detail: date(a.LocalDateTime), URL: im.web + "/photos/" + a.ID, Thumb: a.ID})
	}
	if resp.Assets.NextPage == nil {
		out.Total = len(out.Items) // otherwise unknown: Immich doesn't count matches
	}
	return out, nil
}

func (im *immich) Thumbnail(ctx context.Context, id string) (Image, error) {
	if !uuid.MatchString(id) {
		return Image{}, ErrInvalidID
	}
	return im.image(ctx, "/api/assets/"+id+"/thumbnail?size=thumbnail")
}
