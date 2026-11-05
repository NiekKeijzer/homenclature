package search

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

// paperless does a full-text search of documents.
type paperless struct {
	base
}

var digit = regexp.MustCompile(`^[0-9]+$`)

func newPaperless(b base, s *config.Search) Provider {
	b.header = http.Header{"Authorization": {"Token " + s.Token}}
	return &paperless{base: b}
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
