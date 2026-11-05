package search

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

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

func newSeerr(b base, s *config.Search) Provider {
	b.header = http.Header{"X-Api-Key": {s.Token}}
	return &seerr{base: b, tmdb: apiClient{http: b.http, base: tmdbImages}}
}

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
