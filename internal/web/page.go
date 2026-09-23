package web

import (
	_ "embed"
	"html/template"

	"mymusidownloader/internal/track"
)

//go:embed page.html
var pageHTML string

// trackView adds display-only fields to a Track for the Recent page template.
type trackView struct {
	track.Track
	HasArtwork bool
}

func parsePageTemplate() (*template.Template, error) {
	return template.New("recent").Funcs(template.FuncMap{
		"formatDate": func(t track.Track) string {
			when := t.DownloadStartedAt
			if when.IsZero() {
				when = t.TouchedAt
			}
			if when.IsZero() {
				return ""
			}
			return when.Local().Format("2006-01-02 15:04")
		},
	}).Parse(pageHTML)
}

func newTrackViews(tracks []track.Track, hasArtwork func(videoID string) bool) []trackView {
	views := make([]trackView, len(tracks))
	for i, tr := range tracks {
		views[i] = trackView{Track: tr, HasArtwork: hasArtwork(tr.VideoID)}
	}
	return views
}
