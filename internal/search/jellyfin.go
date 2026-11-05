package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

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

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func newJellyfin(b base, s *config.Search) Provider {
	b.header = http.Header{"Authorization": {`MediaBrowser Token="` + s.Token + `"`}}
	types := s.Types
	if len(types) == 0 {
		types = jellyfinDefaultTypes
	}
	return &jellyfin{base: b, types: types}
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
