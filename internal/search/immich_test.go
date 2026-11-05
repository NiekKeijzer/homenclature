package search

import (
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func TestImmich(t *testing.T) {
	id := "6f1e2d3c-4b5a-4987-8a6b-5c4d3e2f1a0b"
	f := &fakeApp{authHeader: "X-Api-Key", authValue: "tok", responses: map[string]string{
		"POST /api/search/smart":               `{"assets": {"items": [{"id": "` + id + `", "originalFileName": "IMG_0001.jpg", "localDateTime": "2024-07-14T10:00:00.000Z"}], "nextPage": "2"}}`,
		"GET /api/assets/" + id + "/thumbnail": "<svg/>",
	}}
	p := setup(t, config.Search{Type: "immich"}, f)
	checkResults(t, search(t, p, "beach day"), Results{
		Items:     []Result{{Title: "IMG_0001.jpg", Detail: "14 July 2024", URL: "https://app.example/photos/" + id, Thumb: id}},
		SearchURL: "https://app.example/search?query=%7B%22query%22%3A%22beach%20day%22%7D",
	})
	if got := f.bodies[0]; got != `{"query":"beach day","size":8}` {
		t.Errorf("got body %s", got)
	}
	checkThumbnail(t, p, id, "not-a-uuid")
}

func TestImmichLastPageHasTotal(t *testing.T) {
	f := &fakeApp{authHeader: "X-Api-Key", authValue: "tok", responses: map[string]string{
		"POST /api/search/smart": `{"assets": {"items": [{"id": "x"}, {"id": "y"}], "nextPage": null}}`,
	}}
	if got := search(t, setup(t, config.Search{Type: "immich"}, f), "beach").Total; got != 2 {
		t.Errorf("got total %d, want 2", got)
	}
}
