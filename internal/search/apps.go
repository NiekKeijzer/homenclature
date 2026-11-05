package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
	uuid  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	digit = regexp.MustCompile(`^[0-9]+$`)
)

// homeAssistant searches entities by name or entity ID, and shows their state.
type homeAssistant struct {
	base
	domains []string // empty: all
}

func (h *homeAssistant) Search(ctx context.Context, q string) (Results, error) {
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
			Unit         string `json:"unit_of_measurement"`
		} `json:"attributes"`
	}
	if err := h.getJSON(ctx, "/api/states", nil, &states); err != nil {
		return Results{}, err
	}
	q = strings.ToLower(q)
	var matches []Result
	for _, s := range states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		if len(h.domains) > 0 && !slices.Contains(h.domains, domain) {
			continue
		}
		name := s.Attributes.FriendlyName
		if name == "" {
			name = s.EntityID
		}
		if !strings.Contains(strings.ToLower(name), q) && !strings.Contains(s.EntityID, q) {
			continue
		}
		matches = append(matches, Result{
			Title:  name,
			Detail: capitalize(strings.ReplaceAll(domain, "_", " ")),
			Value:  strings.TrimSpace(capitalize(s.State) + " " + s.Attributes.Unit),
			URL:    h.web + "/history?entity_id=" + queryEscape(s.EntityID),
		})
	}
	slices.SortStableFunc(matches, func(a, b Result) int { return strings.Compare(a.Title, b.Title) })
	return Results{Items: matches[:min(len(matches), h.limit)], Total: len(matches)}, nil
}

// Thumbnail always fails: entities have no thumbnails.
func (h *homeAssistant) Thumbnail(context.Context, string) (Image, error) {
	return Image{}, ErrInvalidID
}

// immich does a smart (CLIP) search of photos and videos.
type immich struct {
	base
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

// jellyfin searches the library. An API key searches everything; a user's
// access token searches what that user may see.
type jellyfin struct {
	base
	types []string
}

var jellyfinDefaultTypes = []string{"Movie", "Series", "Episode", "MusicAlbum", "Audio"}

// jellyfinLabels names item types for people; other types show as is.
var jellyfinLabels = map[string]string{
	"Audio": "Song", "AudioBook": "Audiobook", "BoxSet": "Collection", "Movie": "Film",
	"MusicAlbum": "Album", "MusicArtist": "Artist", "MusicVideo": "Music video",
}

func (j *jellyfin) Search(ctx context.Context, q string) (Results, error) {
	var resp struct {
		Items []struct {
			ID                string `json:"Id"`
			Name              string `json:"Name"`
			Type              string `json:"Type"`
			ServerID          string `json:"ServerId"`
			ProductionYear    int    `json:"ProductionYear"`
			RunTimeTicks      int64  `json:"RunTimeTicks"`
			SeriesName        string `json:"SeriesName"`
			ParentIndexNumber int    `json:"ParentIndexNumber"`
			IndexNumber       int    `json:"IndexNumber"`
			Album             string `json:"Album"`
			AlbumArtist       string `json:"AlbumArtist"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	err := j.getJSON(ctx, "/Items", url.Values{
		"searchTerm":       {q},
		"recursive":        {"true"},
		"limit":            {strconv.Itoa(j.limit)},
		"includeItemTypes": {strings.Join(j.types, ",")},
	}, &resp)
	if err != nil {
		return Results{}, err
	}
	out := Results{
		Total:     resp.TotalRecordCount,
		SearchURL: j.web + "/web/#/search?query=" + queryEscape(q),
	}
	for _, it := range resp.Items {
		label := jellyfinLabels[it.Type]
		if label == "" {
			label = it.Type
		}
		var detail string
		switch it.Type {
		case "Episode":
			detail = join(it.SeriesName, fmt.Sprintf("S%dE%d", it.ParentIndexNumber, it.IndexNumber))
		case "MusicAlbum":
			detail = join(label, it.AlbumArtist)
		case "Audio":
			detail = join(label, it.Album)
		case "Movie":
			detail = join(label, year(it.ProductionYear), runtime(it.RunTimeTicks))
		default:
			detail = join(label, year(it.ProductionYear))
		}
		out.Items = append(out.Items, Result{
			Title:  it.Name,
			Detail: detail,
			URL:    j.web + "/web/#/details?id=" + url.QueryEscape(it.ID) + "&serverId=" + url.QueryEscape(it.ServerID),
			Thumb:  it.ID,
		})
	}
	out.More = []Link{{"Search all of " + j.title, out.SearchURL}}
	return out, nil
}

func (j *jellyfin) Thumbnail(ctx context.Context, id string) (Image, error) {
	if !hex32.MatchString(id) {
		return Image{}, ErrInvalidID
	}
	return j.image(ctx, "/Items/"+id+"/Images/Primary?fillHeight=96&quality=80")
}

func year(y int) string {
	if y == 0 {
		return ""
	}
	return strconv.Itoa(y)
}

// runtime formats Jellyfin ticks (100 ns) as "1 h 41 min".
func runtime(ticks int64) string {
	m := ticks / 600_000_000
	switch {
	case m == 0:
		return ""
	case m < 60:
		return fmt.Sprintf("%d min", m)
	default:
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	}
}

// mealie searches recipes. Recipe URLs contain the group slug, which is
// looked up once.
type mealie struct {
	base

	mu        sync.Mutex
	groupSlug string
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

// paperless does a full-text search of documents.
type paperless struct {
	base
}

func (p *paperless) Search(ctx context.Context, q string) (Results, error) {
	var resp struct {
		Count   int `json:"count"`
		Results []struct {
			ID      int    `json:"id"`
			Title   string `json:"title"`
			Created string `json:"created"`
		} `json:"results"`
	}
	err := p.getJSON(ctx, "/api/documents/", url.Values{"query": {q}, "page_size": {strconv.Itoa(p.limit)}}, &resp)
	if err != nil {
		return Results{}, err
	}
	out := Results{Total: resp.Count, SearchURL: p.web + "/documents?query=" + queryEscape(q)}
	for _, d := range resp.Results {
		id := strconv.Itoa(d.ID)
		out.Items = append(out.Items, Result{Title: d.Title, Detail: date(d.Created), URL: p.web + "/documents/" + id + "/details", Thumb: id})
	}
	return out, nil
}

func (p *paperless) Thumbnail(ctx context.Context, id string) (Image, error) {
	if !digit.MatchString(id) {
		return Image{}, ErrInvalidID
	}
	return p.image(ctx, "/api/documents/"+id+"/thumb/")
}

// seerr finds films and shows that aren't in the library yet, to request.
// Results open the title in Seerr, where people request it as themselves.
type seerr struct {
	base
	tmdb apiClient // posters, from TMDB without credentials
}

// tmdbImages serves the posters Seerr links to, sized for thumbnails.
var tmdbImages = "https://image.tmdb.org/t/p/w92"

var posterFile = regexp.MustCompile(`^[A-Za-z0-9]+\.(jpg|png)$`)

// seerrStatus labels Seerr media statuses. Missing ones (unknown, deleted)
// can be requested and get no label.
var seerrStatus = map[int]string{2: "Pending approval", 3: "Requested", 4: "Partly available"}

const (
	seerrAvailable   = 5
	seerrBlocklisted = 6
)

func (s *seerr) Search(ctx context.Context, q string) (Results, error) {
	var resp struct {
		TotalPages int `json:"totalPages"`
		Results    []struct {
			ID           int    `json:"id"`
			MediaType    string `json:"mediaType"`
			Title        string `json:"title"` // films
			Name         string `json:"name"`  // shows
			ReleaseDate  string `json:"releaseDate"`
			FirstAirDate string `json:"firstAirDate"`
			PosterPath   string `json:"posterPath"`
			MediaInfo    *struct {
				Status int `json:"status"`
			} `json:"mediaInfo"`
		} `json:"results"`
	}
	// Seerr rejects "+" for spaces, so the query isn't built with url.Values.
	if err := s.getJSON(ctx, "/api/v1/search?query="+queryEscape(q)+"&page=1", nil, &resp); err != nil {
		return Results{}, err
	}
	var matches []Result
	for _, r := range resp.Results {
		var status int
		if r.MediaInfo != nil {
			status = r.MediaInfo.Status
		}
		if status == seerrAvailable || status == seerrBlocklisted {
			continue
		}
		var res Result
		switch r.MediaType {
		case "movie":
			res = Result{Title: r.Title, Detail: join("Film", year4(r.ReleaseDate), seerrStatus[status])}
		case "tv":
			res = Result{Title: r.Name, Detail: join("Series", year4(r.FirstAirDate), seerrStatus[status])}
		default:
			continue // people and collections
		}
		res.URL = s.web + "/" + r.MediaType + "/" + strconv.Itoa(r.ID)
		res.Thumb = strings.TrimPrefix(r.PosterPath, "/")
		matches = append(matches, res)
	}
	out := Results{Items: matches[:min(len(matches), s.limit)], SearchURL: SeerrSearchURL(s.web, q)}
	if resp.TotalPages <= 1 {
		out.Total = len(matches) // otherwise unknown: only the first page was read
	}
	return out, nil
}

// SeerrSearchURL is a search for q in Seerr's web app at seerrURL.
func SeerrSearchURL(seerrURL, q string) string {
	return strings.TrimSuffix(seerrURL, "/") + "/search?query=" + queryEscape(q)
}

func (s *seerr) Thumbnail(ctx context.Context, id string) (Image, error) {
	if !posterFile.MatchString(id) {
		return Image{}, ErrInvalidID
	}
	return s.tmdb.image(ctx, "/"+id)
}

// year4 returns the year of an ISO date, or "".
func year4(date string) string {
	if len(date) < 4 {
		return ""
	}
	return date[:4]
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
