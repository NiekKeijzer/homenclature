package search

import (
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

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
