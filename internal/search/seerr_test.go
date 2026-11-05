package search

import (
	"net/http/httptest"
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func TestSeerr(t *testing.T) {
	// Posters come from TMDB, without the Seerr key.
	tmdb := &fakeApp{authHeader: "X-Api-Key", authValue: "", responses: map[string]string{"GET /poster.jpg": "<svg/>"}}
	tmdb.t = t
	tmdbSrv := httptest.NewServer(tmdb)
	t.Cleanup(tmdbSrv.Close)
	defer func(old string) { tmdbImages = old }(tmdbImages)
	tmdbImages = tmdbSrv.URL

	f := &fakeApp{authHeader: "X-Api-Key", authValue: "tok", responses: map[string]string{
		"GET /api/v1/search": `{"page": 1, "totalPages": 1, "totalResults": 7, "results": [
			{"id": 1, "mediaType": "movie", "title": "The Secret Garden", "releaseDate": "2020-07-31", "posterPath": "/poster.jpg"},
			{"id": 2, "mediaType": "movie", "title": "The Secret Garden", "releaseDate": "1993-08-13", "mediaInfo": {"status": 5}},
			{"id": 3, "mediaType": "tv", "name": "Secret Gardens", "firstAirDate": "2019-01-01", "mediaInfo": {"status": 2}},
			{"id": 4, "mediaType": "person", "name": "Gardener"},
			{"id": 5, "mediaType": "tv", "name": "Garden Rescue", "firstAirDate": "2016-06-01", "mediaInfo": {"status": 4}},
			{"id": 6, "mediaType": "movie", "title": "Blocked", "mediaInfo": {"status": 6}},
			{"id": 7, "mediaType": "movie", "title": "Untitled", "mediaInfo": {"status": 3}}
		]}`,
	}}
	p := setup(t, config.Search{Type: "seerr", Limit: 3}, f)
	checkResults(t, search(t, p, "secret garden"), Results{
		Items: []Result{
			{Title: "The Secret Garden", Detail: "Film · 2020", URL: "https://app.example/movie/1", Thumb: "poster.jpg"},
			{Title: "Secret Gardens", Detail: "Series · 2019 · Pending approval", URL: "https://app.example/tv/3"},
			{Title: "Garden Rescue", Detail: "Series · 2016 · Partly available", URL: "https://app.example/tv/5"},
		},
		Total:     4,
		SearchURL: "https://app.example/search?query=secret%20garden",
	})
	if got := f.requests[0].URL.RawQuery; got != "query=secret%20garden&page=1" {
		t.Errorf("got query %q, want spaces as %%20", got)
	}
	checkThumbnail(t, p, "poster.jpg", "../poster.jpg")
}
