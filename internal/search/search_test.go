package search

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// fakeApp serves canned responses per "METHOD /path" and checks the auth header.
type fakeApp struct {
	t          *testing.T
	authHeader string // header name
	authValue  string
	responses  map[string]string // "GET /path" -> body; bodies starting with "<svg" are images
	requests   []*http.Request
	bodies     []string
}

func (f *fakeApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, r)
	f.bodies = append(f.bodies, string(body))
	if got := r.Header.Get(f.authHeader); got != f.authValue {
		f.t.Errorf("%s %s: %s header %q, want %q", r.Method, r.URL.Path, f.authHeader, got, f.authValue)
	}
	resp, ok := f.responses[r.Method+" "+r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(resp, "<svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	io.WriteString(w, resp)
}

// setup starts a fake API and returns a provider for it. The link URL (used
// for deep links) differs from api_url, to check both are used correctly.
func setup(t *testing.T, s config.Search, f *fakeApp) Provider {
	t.Helper()
	f.t = t
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s.Token, s.APIURL = "tok", srv.URL
	p, err := New(config.Link{Title: "App", URL: "https://app.example/", Search: &s}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func search(t *testing.T, p Provider, q string) Results {
	t.Helper()
	results, err := p.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func checkResults(t *testing.T, got, want Results) {
	t.Helper()
	g, _ := json.MarshalIndent(got, "", "  ")
	w, _ := json.MarshalIndent(want, "", "  ")
	if string(g) != string(w) {
		t.Errorf("got %s\nwant %s", g, w)
	}
}

func checkQuery(t *testing.T, r *http.Request, want map[string]string) {
	t.Helper()
	q := r.URL.Query()
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s: got %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestHomeAssistant(t *testing.T) {
	f := &fakeApp{authHeader: "Authorization", authValue: "Bearer tok", responses: map[string]string{
		"GET /api/states": `[
			{"entity_id": "sensor.garden_temperature", "state": "11.4", "attributes": {"friendly_name": "Garden temperature", "unit_of_measurement": "°C"}},
			{"entity_id": "light.garden_lights", "state": "on", "attributes": {"friendly_name": "Garden lights"}},
			{"entity_id": "binary_sensor.garden_gate", "state": "off", "attributes": {}},
			{"entity_id": "light.kitchen", "state": "off", "attributes": {"friendly_name": "Kitchen"}},
			{"entity_id": "automation.water_garden", "state": "on", "attributes": {"friendly_name": "Water the plants"}}
		]`,
	}}
	p := setup(t, config.Search{Type: "home-assistant", Limit: 2, Domains: []string{"light", "sensor", "binary_sensor"}}, f)
	// Sorted by name, matched on name or entity ID, automation filtered out by domains.
	checkResults(t, search(t, p, "Garden"), Results{
		Items: []Result{
			{Title: "Garden lights", Detail: "Light", Value: "On", URL: "https://app.example/history?entity_id=light.garden_lights"},
			{Title: "Garden temperature", Detail: "Sensor", Value: "11.4 °C", URL: "https://app.example/history?entity_id=sensor.garden_temperature"},
		},
		Total: 3,
	})
	checkResults(t, search(t, p, "gate"), Results{
		Items: []Result{{Title: "binary_sensor.garden_gate", Detail: "Binary sensor", Value: "Off", URL: "https://app.example/history?entity_id=binary_sensor.garden_gate"}},
		Total: 1,
	})
	if _, err := p.Thumbnail(context.Background(), "x"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("thumbnail: got %v, want ErrInvalidID", err)
	}
}

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

func TestJellyfin(t *testing.T) {
	f := &fakeApp{authHeader: "Authorization", authValue: `MediaBrowser Token="tok"`, responses: map[string]string{
		"GET /Items": `{"TotalRecordCount": 9, "Items": [
			{"Id": "0123456789abcdef0123456789abcdef", "Name": "The Secret Garden", "Type": "Movie", "ServerId": "srv", "ProductionYear": 1993, "RunTimeTicks": 60600000000},
			{"Id": "fedcba9876543210fedcba9876543210", "Name": "Pilot", "Type": "Episode", "ServerId": "srv", "SeriesName": "Gardeners", "ParentIndexNumber": 1, "IndexNumber": 2},
			{"Id": "00000000000000000000000000000000", "Name": "Garden Songs", "Type": "MusicAlbum", "ServerId": "srv", "AlbumArtist": "The Band"}
		]}`,
		"GET /Items/0123456789abcdef0123456789abcdef/Images/Primary": "<svg/>",
	}}
	p := setup(t, config.Search{Type: "jellyfin"}, f)
	checkResults(t, search(t, p, "secret garden"), Results{
		Items: []Result{
			{Title: "The Secret Garden", Detail: "Film · 1993 · 1 h 41 min", URL: "https://app.example/web/#/details?id=0123456789abcdef0123456789abcdef&serverId=srv", Thumb: "0123456789abcdef0123456789abcdef"},
			{Title: "Pilot", Detail: "Gardeners · S1E2", URL: "https://app.example/web/#/details?id=fedcba9876543210fedcba9876543210&serverId=srv", Thumb: "fedcba9876543210fedcba9876543210"},
			{Title: "Garden Songs", Detail: "Album · The Band", URL: "https://app.example/web/#/details?id=00000000000000000000000000000000&serverId=srv", Thumb: "00000000000000000000000000000000"},
		},
		Total:     9,
		SearchURL: "https://app.example/web/#/search?query=secret%20garden",
		More:      []Link{{"Search all of App", "https://app.example/web/#/search?query=secret%20garden"}},
	})
	checkQuery(t, f.requests[0], map[string]string{
		"searchTerm": "secret garden", "recursive": "true", "limit": "5", "includeItemTypes": "Movie,Series,Episode,MusicAlbum,Audio",
	})
	checkThumbnail(t, p, "0123456789abcdef0123456789abcdef", "../../etc/passwd")
}

func TestJellyfinOptions(t *testing.T) {
	f := &fakeApp{authHeader: "Authorization", authValue: `MediaBrowser Token="tok"`, responses: map[string]string{
		"GET /Items": `{"TotalRecordCount": 0, "Items": []}`,
	}}
	p := setup(t, config.Search{Type: "jellyfin", Limit: 3, Types: []string{"Movie", "BoxSet"}}, f)
	checkResults(t, search(t, p, "nothing"), Results{
		SearchURL: "https://app.example/web/#/search?query=nothing",
		More:      []Link{{"Search all of App", "https://app.example/web/#/search?query=nothing"}},
	})
	checkQuery(t, f.requests[0], map[string]string{"limit": "3", "includeItemTypes": "Movie,BoxSet"})
}

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

func checkThumbnail(t *testing.T, p Provider, valid, invalid string) {
	t.Helper()
	img, err := p.Thumbnail(context.Background(), valid)
	if err != nil || img.ContentType != "image/svg+xml" || string(img.Body) != "<svg/>" {
		t.Errorf("thumbnail %q: got %+v, %v", valid, img, err)
	}
	if _, err := p.Thumbnail(context.Background(), invalid); !errors.Is(err, ErrInvalidID) {
		t.Errorf("thumbnail %q: got %v, want ErrInvalidID", invalid, err)
	}
}

func TestErrorStatus(t *testing.T) {
	p := setup(t, config.Search{Type: "paperless"}, &fakeApp{authHeader: "Authorization", authValue: "Token tok"})
	if _, err := p.Search(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Errorf("got %v, want a status 404 error", err)
	}
}
