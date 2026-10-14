package web

import (
	_ "embed"
	"html/template"

	"mymusidownloader/internal/track"
)

//go:embed page.html
var pageHTML string

// trackView adds display-only fields to a Track for the Recent page template.
// TintCSS is a "--tint: r, g, b" custom property taken from the cover.
type trackView struct {
	track.Track
	HasArtwork bool
	TintCSS    template.CSS
}

func parsePageTemplate() (*template.Template, error) {
	return template.New("recent").Funcs(template.FuncMap{
		"hasGenre": func(genres []string, genre string) bool {
			for _, item := range genres {
				if item == genre {
					return true
				}
			}
			return false
		},
		"formatDate": func(t track.Track) string {
			when := t.CreatedOn()
			if when.IsZero() {
				return ""
			}
			return when.Local().Format("2006-01-02 15:04")
		},
	}).Parse(pageHTML)
}

func newTrackViews(tracks []track.Track, artworkPath func(videoID string) (string, bool), tints *tintCache) []trackView {
	views := make([]trackView, len(tracks))
	for i, tr := range tracks {
		path, ok := artworkPath(tr.VideoID)
		view := trackView{Track: tr, HasArtwork: ok}
		if ok {
			view.TintCSS = tints.css(path)
		}
		views[i] = view
	}
	return views
}
