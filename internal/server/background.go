package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// Backgrounds, set for everyone with HOMENCLATURE_BACKGROUND.
const (
	bgDefault   = "default"   // glow blobs, see style.css
	bgLandscape = "landscape" // drawn in the browser, by the visitor's time of day
	bgFolder    = "folder"    // photos from HOMENCLATURE_BACKGROUND_DIR
	bgImmich    = "immich"    // photos from an Immich album
)

const (
	defaultPhotoInterval = 10 * time.Minute
	minPhotoInterval     = time.Minute
	immichPhotos         = 1000 // photos asked for at once, Immich's maximum
)

// photoExts are the files a background folder may show. Not SVG: it can
// carry scripts.
var photoExts = []string{".jpg", ".jpeg", ".png", ".webp", ".avif"}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var errNoPhotos = errors.New("no photos")

// background is the page background, the same for everyone.
type background struct {
	mode     string
	interval time.Duration // between photos
	photos   *photoBag     // nil unless the mode shows photos
}

// newBackground reads the background settings from env vars. Settings for
// other modes are ignored. Immich photos use the first Immich link with
// search in cfg: its API URL and token.
func newBackground(cfg *config.Config) (background, error) {
	bg := background{mode: getenv("HOMENCLATURE_BACKGROUND", bgDefault), interval: defaultPhotoInterval}
	var src photoSource
	switch bg.mode {
	case bgDefault, bgLandscape:
		return bg, nil
	case bgFolder:
		dir := getenv("HOMENCLATURE_BACKGROUND_DIR", "backgrounds")
		f := folder{os.DirFS(dir)}
		if _, err := fs.ReadDir(f.fsys, "."); err != nil {
			return bg, fmt.Errorf("HOMENCLATURE_BACKGROUND_DIR must be a readable folder: %w", err)
		}
		src = f
	case bgImmich:
		album := os.Getenv("HOMENCLATURE_BACKGROUND_IMMICH_ALBUM")
		if !uuidPattern.MatchString(album) {
			return bg, fmt.Errorf("HOMENCLATURE_BACKGROUND_IMMICH_ALBUM must be an album ID, got %q", album)
		}
		i := slices.IndexFunc(cfg.Links(), func(l config.Link) bool { return l.Search != nil && l.Search.Type == "immich" })
		if i < 0 {
			return bg, errors.New(`HOMENCLATURE_BACKGROUND=immich needs an Immich link with search (type = "immich") in the config`)
		}
		l := cfg.Links()[i]
		api := l.URL
		if l.Search.APIURL != "" {
			api = l.Search.APIURL
		}
		src = immichAlbum{
			client: &http.Client{Timeout: 30 * time.Second},
			api:    strings.TrimSuffix(api, "/"),
			token:  l.Search.Token,
			album:  album,
		}
	default:
		return bg, fmt.Errorf("HOMENCLATURE_BACKGROUND must be %s, %s, %s or %s, got %q", bgDefault, bgLandscape, bgFolder, bgImmich, bg.mode)
	}

	if v := os.Getenv("HOMENCLATURE_BACKGROUND_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < minPhotoInterval {
			return bg, fmt.Errorf("HOMENCLATURE_BACKGROUND_INTERVAL must be a duration of at least %v, got %q", minPhotoInterval, v)
		}
		bg.interval = d
	}
	bg.photos = &photoBag{source: src}
	return bg, nil
}

// photoInterval is the time between photos in seconds, for the page; 0
// without photos.
func (bg background) photoInterval() int {
	if bg.photos == nil {
		return 0
	}
	return int(bg.interval.Seconds())
}

// photoSource is where background photos come from.
type photoSource interface {
	// photos returns the IDs of the photos to show.
	photos(ctx context.Context) ([]string, error)
	// serve writes a photo, or returns an error before writing anything.
	serve(w http.ResponseWriter, r *http.Request, id string) error
}

// photoBag hands out photos in random order, each once before any repeats.
// Photos added in the meantime show up in the next round.
type photoBag struct {
	source photoSource

	mu   sync.Mutex
	left []string
	last string
}

func (b *photoBag) next(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.left) == 0 {
		ids, err := b.source.photos(ctx)
		if err != nil {
			return "", err
		}
		rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		if len(ids) > 1 && ids[0] == b.last { // don't show the same one twice in a row
			ids[0], ids[len(ids)-1] = ids[len(ids)-1], ids[0]
		}
		b.left = ids
	}
	if len(b.left) == 0 {
		return "", errNoPhotos
	}
	b.last, b.left = b.left[0], b.left[1:]
	return b.last, nil
}

// backgroundPhoto serves the next background photo to logged-in users.
func (a *app) backgroundPhoto(w http.ResponseWriter, r *http.Request) {
	var s session
	if a.bg.photos == nil || !a.readCookie(r, sessionCookie, &s) {
		http.NotFound(w, r)
		return
	}
	id, err := a.bg.photos.next(r.Context())
	if err == nil {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		err = a.bg.photos.source.serve(w, r, id)
	}
	if err != nil {
		log.Printf("background: %v", err)
		http.NotFound(w, r) // the page keeps the photo it shows
	}
}

// folder serves the photos in a folder, not its subfolders.
type folder struct {
	fsys fs.FS
}

func (f folder) photos(context.Context) ([]string, error) {
	entries, err := fs.ReadDir(f.fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		// Dot files include macOS's "._photo.jpg", which aren't photos.
		if !e.IsDir() && !strings.HasPrefix(name, ".") && slices.Contains(photoExts, strings.ToLower(path.Ext(name))) {
			names = append(names, name)
		}
	}
	return names, nil
}

func (f folder) serve(w http.ResponseWriter, r *http.Request, name string) error {
	http.ServeFileFS(w, r, f.fsys, name) // name comes from photos, not the request
	return nil
}

// immichAlbum serves the photos in an Immich album, as previews (about
// 1440px). It only reads.
type immichAlbum struct {
	client       *http.Client
	api          string
	token, album string
}

func (im immichAlbum) photos(ctx context.Context) ([]string, error) {
	// albumIds and type are deprecated since Immich 3.2 in favour of filter,
	// but unlike filter they also work on older versions.
	body, _ := json.Marshal(map[string]any{"albumIds": []string{im.album}, "type": "IMAGE", "size": immichPhotos})
	resp, err := im.do(ctx, http.MethodPost, "/api/search/random", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var assets []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return nil, fmt.Errorf("immich album: %w", err)
	}
	ids := make([]string, len(assets))
	for i, a := range assets {
		ids[i] = a.ID
	}
	return ids, nil
}

func (im immichAlbum) serve(w http.ResponseWriter, r *http.Request, id string) error {
	resp, err := im.do(r.Context(), http.MethodGet, "/api/assets/"+url.PathEscape(id)+"/thumbnail?size=preview", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("immich photo %s: content type %q is not an image", id, ct)
	}
	w.Header().Set("Content-Type", ct)
	io.Copy(w, resp.Body) // an error here is the browser going away
	return nil
}

func (im immichAlbum) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, im.api+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", im.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := im.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("immich %s %s: status %d", method, path, resp.StatusCode)
	}
	return resp, nil
}
