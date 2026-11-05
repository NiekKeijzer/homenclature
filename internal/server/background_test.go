package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testAlbum = "1b1f2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

// setBackgroundEnv sets the background env vars, unset ones to "".
func setBackgroundEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, name := range []string{"", "_INTERVAL", "_DIR", "_IMMICH_ALBUM"} {
		t.Setenv("HOMENCLATURE_BACKGROUND"+name, vars[name])
	}
}

// withBackground returns a with the background from vars.
func withBackground(t *testing.T, a *app, vars map[string]string) *app {
	t.Helper()
	setBackgroundEnv(t, vars)
	bg, err := newBackground(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.bg = bg
	return a
}

const homeConfig = `[[dashboards.family.sections]]
title = "Home"`

func TestNewBackground(t *testing.T) {
	t.Setenv("TEST_TOKEN", "tok")
	immichConfig := homeConfig + `
[[dashboards.family.sections.links]]
title = "Immich"
url = "https://photos.example"
search = { type = "immich", token_env = "TEST_TOKEN" }`
	dir := t.TempDir()
	file := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		config   string
		vars     map[string]string
		mode     string
		interval int    // photoInterval()
		err      string // part of the error
	}{
		"unset is default":      {homeConfig, nil, "default", 0, ""},
		"landscape":             {homeConfig, map[string]string{"": "landscape", "_INTERVAL": "nonsense"}, "landscape", 0, ""},
		"unknown":               {homeConfig, map[string]string{"": "sunset"}, "", 0, "HOMENCLATURE_BACKGROUND must be"},
		"folder":                {homeConfig, map[string]string{"": "folder", "_DIR": dir}, "folder", 600, ""},
		"folder with interval":  {homeConfig, map[string]string{"": "folder", "_DIR": dir, "_INTERVAL": "90s"}, "folder", 90, ""},
		"interval too short":    {homeConfig, map[string]string{"": "folder", "_DIR": dir, "_INTERVAL": "30s"}, "", 0, "HOMENCLATURE_BACKGROUND_INTERVAL must be"},
		"interval not a time":   {homeConfig, map[string]string{"": "folder", "_DIR": dir, "_INTERVAL": "often"}, "", 0, "HOMENCLATURE_BACKGROUND_INTERVAL must be"},
		"folder missing":        {homeConfig, map[string]string{"": "folder", "_DIR": filepath.Join(dir, "nope")}, "", 0, "HOMENCLATURE_BACKGROUND_DIR must be"},
		"folder is a file":      {homeConfig, map[string]string{"": "folder", "_DIR": file}, "", 0, "HOMENCLATURE_BACKGROUND_DIR must be"},
		"immich":                {immichConfig, map[string]string{"": "immich", "_IMMICH_ALBUM": testAlbum}, "immich", 600, ""},
		"immich without album":  {immichConfig, map[string]string{"": "immich"}, "", 0, "HOMENCLATURE_BACKGROUND_IMMICH_ALBUM must be"},
		"immich bad album":      {immichConfig, map[string]string{"": "immich", "_IMMICH_ALBUM": "holiday"}, "", 0, "HOMENCLATURE_BACKGROUND_IMMICH_ALBUM must be"},
		"immich without a link": {homeConfig, map[string]string{"": "immich", "_IMMICH_ALBUM": testAlbum}, "", 0, "needs an Immich link with search"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t, tt.config)
			setBackgroundEnv(t, tt.vars)
			bg, err := newBackground(a.cfg)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("got error %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if bg.mode != tt.mode || bg.photoInterval() != tt.interval {
				t.Errorf("got %q every %ds, want %q every %ds", bg.mode, bg.photoInterval(), tt.mode, tt.interval)
			}
		})
	}
}

func TestBackgroundImmichUsesFirstLink(t *testing.T) {
	t.Setenv("FIRST_TOKEN", "first")
	t.Setenv("SECOND_TOKEN", "second")
	// The first in file order wins, whichever group it belongs to.
	a := newTestApp(t, `
[[dashboards.kids.sections]]
title = "Fun"
[[dashboards.kids.sections.links]]
title = "Immich"
url = "https://photos.example/"
search = { type = "immich", token_env = "FIRST_TOKEN", api_url = "http://immich:2283/" }

[[dashboards.family.sections]]
title = "Media"
[[dashboards.family.sections.links]]
title = "Immich for everyone"
url = "https://all.photos.example"
search = { type = "immich", token_env = "SECOND_TOKEN" }`)
	a = withBackground(t, a, map[string]string{"": "immich", "_IMMICH_ALBUM": testAlbum})
	im := a.bg.photos.source.(immichAlbum)
	if im.api != "http://immich:2283" || im.token != "first" || im.album != testAlbum {
		t.Errorf("got %s with token %q for album %s, want the first link's api_url and token", im.api, im.token, im.album)
	}
}

func TestPageBackground(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]struct {
		vars      map[string]string
		want, not []string
	}{
		"default":   {nil, []string{`data-background="default"`}, []string{"backdrop"}},
		"landscape": {map[string]string{"": "landscape"}, []string{`data-background="landscape"`, `class="backdrop landscape"`}, []string{"slideshow"}},
		"folder":    {map[string]string{"": "folder", "_DIR": dir, "_INTERVAL": "5m"}, []string{`class="backdrop slideshow" data-interval="300"`}, []string{"landscape"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			a := withBackground(t, newTestApp(t, homeConfig), tt.vars)
			html := body(t, get(a, "/", login(a, "family")))
			for _, s := range tt.want {
				if !strings.Contains(html, s) {
					t.Errorf("page lacks %s", s)
				}
			}
			for _, s := range tt.not {
				if strings.Contains(html, s) {
					t.Errorf("page has %s", s)
				}
			}
		})
	}
}

func TestBackgroundFolder(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.jpg": "jpeg", "b.PNG": "png", // photos
		"notes.txt": "text", "logo.svg": "<svg/>", "._a.jpg": "macOS metadata", "sub/c.jpg": "jpeg",
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o700)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := withBackground(t, newTestApp(t, homeConfig), map[string]string{"": "folder", "_DIR": dir})

	if resp := get(a, "/background"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("without session: got %d, want 404", resp.StatusCode)
	}

	cookie := login(a, "family")
	var got []string
	for range 4 {
		resp := get(a, "/background", cookie)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			t.Fatalf("got %d, %q, %q; want 200, no-store, an image", resp.StatusCode, resp.Header.Get("Cache-Control"), resp.Header.Get("Content-Type"))
		}
		got = append(got, body(t, resp))
	}
	// Each photo once per round, and never the same one twice in a row.
	for i, round := range [][]string{got[:2], got[2:]} {
		if r := slices.Sorted(slices.Values(round)); !slices.Equal(r, []string{"jpeg", "png"}) {
			t.Errorf("round %d: got %q, want both photos once", i+1, round)
		}
	}
	if got[1] == got[2] {
		t.Errorf("got %q, the same photo twice in a row", got)
	}
}

func TestBackgroundEmptyFolder(t *testing.T) {
	a := withBackground(t, newTestApp(t, homeConfig), map[string]string{"": "folder", "_DIR": t.TempDir()})
	if resp := get(a, "/background", login(a, "family")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
}

func TestBackgroundWithoutPhotos(t *testing.T) {
	a := withBackground(t, newTestApp(t, homeConfig), map[string]string{"": "landscape"})
	if resp := get(a, "/background", login(a, "family")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
}

func TestBackgroundImmich(t *testing.T) {
	const photo = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	const video = "ffffffff-4e5f-4a6b-8c7d-9e0f1a2b3c4d" // Immich wouldn't list it; served wrong to test the check
	immich := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "tok" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/search/random":
			var req struct {
				AlbumIDs []string `json:"albumIds"`
				Type     string   `json:"type"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if !slices.Equal(req.AlbumIDs, []string{testAlbum}) || req.Type != "IMAGE" {
				http.Error(w, "wrong search", http.StatusBadRequest)
				return
			}
			io.WriteString(w, `[{"id": "`+photo+`"}, {"id": "`+video+`"}]`)
		case "GET /api/assets/" + photo + "/thumbnail":
			if r.URL.Query().Get("size") != "preview" {
				http.Error(w, "wrong size", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			io.WriteString(w, "preview")
		case "GET /api/assets/" + video + "/thumbnail":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<script>")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(immich.Close)
	t.Setenv("TEST_TOKEN", "tok")
	a := newTestApp(t, homeConfig+`
[[dashboards.family.sections.links]]
title = "Immich"
url = "https://photos.example"
search = { type = "immich", token_env = "TEST_TOKEN", api_url = "`+immich.URL+`" }`)
	a = withBackground(t, a, map[string]string{"": "immich", "_IMMICH_ALBUM": testAlbum})

	cookie := login(a, "family")
	var got []string
	for range 2 {
		resp := get(a, "/background", cookie)
		got = append(got, resp.Status+" "+resp.Header.Get("Content-Type")+" "+body(t, resp))
	}
	slices.Sort(got)
	want := []string{
		"200 OK image/jpeg preview",
		"404 Not Found text/plain; charset=utf-8 404 page not found\n",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
