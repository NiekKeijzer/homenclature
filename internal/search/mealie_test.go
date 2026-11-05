package search

import (
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func TestMealie(t *testing.T) {
	id := "1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d"
	f := &fakeApp{authHeader: "Authorization", authValue: "Bearer tok", responses: map[string]string{
		"GET /api/users/self": `{"groupSlug": "home"}`,
		"GET /api/recipes": `{"total": 12, "items": [
			{"id": "` + id + `", "name": "Pea soup", "slug": "pea-soup", "description": "Fresh", "totalTime": "35 min", "recipeCategory": [{"name": "Soup"}]},
			{"id": "` + id + `", "name": "Pancakes", "slug": "pancakes", "description": "Fluffy", "totalTime": null, "recipeCategory": []}
		]}`,
		"GET /api/media/recipes/" + id + "/images/min-original.webp": "<svg/>",
	}}
	p := setup(t, config.Search{Type: "mealie"}, f)
	want := Results{
		Items: []Result{
			{Title: "Pea soup", Detail: "Soup · 35 min", URL: "https://app.example/g/home/r/pea-soup", Thumb: id},
			{Title: "Pancakes", Detail: "Fluffy", URL: "https://app.example/g/home/r/pancakes", Thumb: id},
		},
		Total:     12,
		SearchURL: "https://app.example/g/home?search=pea%20soup",
	}
	checkResults(t, search(t, p, "pea soup"), want)
	checkResults(t, search(t, p, "pea soup"), want)
	if n := len(f.requests); n != 3 {
		t.Errorf("got %d requests, want 3 (group slug looked up once)", n)
	}
	checkQuery(t, f.requests[1], map[string]string{"search": "pea soup", "perPage": "5"})
	checkThumbnail(t, p, id, "../users/self")
}
