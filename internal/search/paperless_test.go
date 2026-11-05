package search

import (
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func TestPaperless(t *testing.T) {
	f := &fakeApp{authHeader: "Authorization", authValue: "Token tok", responses: map[string]string{
		"GET /api/documents/":          `{"count": 4, "results": [{"id": 42, "title": "Energy bill", "created": "2024-05-01"}]}`,
		"GET /api/documents/42/thumb/": "<svg/>",
	}}
	p := setup(t, config.Search{Type: "paperless", Limit: 1}, f)
	checkResults(t, search(t, p, "energy"), Results{
		Items:     []Result{{Title: "Energy bill", Detail: "1 May 2024", URL: "https://app.example/documents/42/details", Thumb: "42"}},
		Total:     4,
		SearchURL: "https://app.example/documents?query=energy",
	})
	checkQuery(t, f.requests[0], map[string]string{"query": "energy", "page_size": "1"})
	checkThumbnail(t, p, "42", "42/../1")
}
