package web

import (
	"context"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mymusidownloader/internal/track"
	"mymusidownloader/internal/version"
)

type App interface {
	Trigger(ctx context.Context, input string) (track.StartResult, error)
	Update(videoID, title, artist, genre, album string) (track.Track, error)
	Cancel(videoID string) (track.Track, error)
	Restart(ctx context.Context, videoID string) (track.StartResult, error)
	Convert(ctx context.Context, videoID string, equivalent bool) (track.Track, error)
	Delete(videoID string) error
	Recent() ([]track.Track, bool)
	ArtworkPath(videoID string) (string, bool)
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Server struct {
	app  App
	tmpl *template.Template
}

func New(app App) (*Server, error) {
	tmpl, err := parsePageTemplate()
	if err != nil {
		return nil, err
	}
	return &Server{app: app, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/trigger", s.handleTrigger)
	mux.HandleFunc("/tracks/", s.handleTrackAction)
	mux.Handle("/static/", staticHandler())
	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tracks, refresh := s.app.Recent()
	views := newTrackViews(tracks, func(videoID string) bool {
		_, ok := s.app.ArtworkPath(videoID)
		return ok
	})
	data := struct {
		Tracks  []trackView
		Refresh bool
		Now     time.Time
		Notice  string
		Version string
	}{Tracks: views, Refresh: refresh, Now: time.Now(), Notice: noticeFromQuery(r), Version: version.Current}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	input, err := readURLInput(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	result, err := s.app.Trigger(r.Context(), input)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, result)
		return
	}
	http.Redirect(w, r, noticeURL(string(result.State), result.VideoID, result.Error), http.StatusSeeOther)
}

func (s *Server) handleTrackAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tracks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	videoID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		s.handleEdit(w, r, videoID)
		return
	}
	if parts[1] == "artwork" {
		s.handleArtwork(w, r, videoID)
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "cancel":
		s.handleCancel(w, r, videoID)
	case "restart":
		s.handleRestart(w, r, videoID)
	case "convert":
		s.handleConvert(w, r, videoID)
	case "delete":
		s.handleDelete(w, r, videoID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleArtwork(w http.ResponseWriter, r *http.Request, videoID string) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if !videoIDPattern.MatchString(videoID) {
		http.NotFound(w, r)
		return
	}
	path, ok := s.app.ArtworkPath(videoID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
