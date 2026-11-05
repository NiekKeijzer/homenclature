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
