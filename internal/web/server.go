package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mymusidownloader/internal/track"
)

type App interface {
	Trigger(ctx context.Context, input string) (track.StartResult, error)
	Update(videoID, title, artist, genre string) (track.Track, error)
	Cancel(videoID string) (track.Track, error)
	Restart(ctx context.Context, videoID string) (track.StartResult, error)
	Recent() ([]track.Track, bool)
}

type Server struct {
	app  App
	tmpl *template.Template
}

func New(app App) (*Server, error) {
	tmpl, err := template.New("recent").Funcs(template.FuncMap{
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
	}).Parse(pageTemplate)
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
	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tracks, refresh := s.app.Recent()
	data := struct {
		Tracks  []track.Track
		Refresh bool
		Now     time.Time
		Notice  string
	}{Tracks: tracks, Refresh: refresh, Now: time.Now(), Notice: noticeFromQuery(r)}
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
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/tracks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	videoID := parts[0]
	if len(parts) == 1 {
		s.handleEdit(w, r, videoID)
		return
	}
	switch parts[1] {
	case "cancel":
		s.handleCancel(w, r, videoID)
	case "restart":
		s.handleRestart(w, r, videoID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request, videoID string) {
	payload := struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Genre  string `json:"genre"`
	}{}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, r, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeError(w, r, http.StatusBadRequest, err)
			return
		}
		payload.Title = r.FormValue("title")
		payload.Artist = r.FormValue("artist")
		payload.Genre = r.FormValue("genre")
	}
	tr, err := s.app.Update(videoID, payload.Title, payload.Artist, payload.Genre)
	if err != nil {
		writeTrackError(w, r, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, tr)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request, videoID string) {
	tr, err := s.app.Cancel(videoID)
	if err != nil {
		writeTrackError(w, r, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, tr)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request, videoID string) {
	result, err := s.app.Restart(r.Context(), videoID)
	if err != nil {
		writeTrackError(w, r, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, result)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func readURLInput(r *http.Request) (string, error) {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", fmt.Errorf("decode body: %w", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return "", errors.New("url is required")
		}
		return body.URL, nil
	}
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	input := strings.TrimSpace(r.FormValue("url"))
	if input == "" {
		return "", errors.New("url is required")
	}
	return input, nil
}

func wantsJSON(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	accept := r.Header.Get("Accept")
	return strings.Contains(contentType, "application/json") || strings.Contains(accept, "application/json")
}

func writeTrackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, track.ErrNotFound):
		writeError(w, r, http.StatusNotFound, err)
	case errors.Is(err, track.ErrInvalidTransition):
		writeError(w, r, http.StatusConflict, err)
	default:
		writeError(w, r, http.StatusInternalServerError, err)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, code int, err error) {
	if wantsJSON(r) {
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	http.Error(w, err.Error(), code)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func noticeURL(state, videoID, errText string) string {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	if videoID != "" {
		q.Set("videoId", videoID)
	}
	if errText != "" {
		q.Set("error", errText)
	}
	if len(q) == 0 {
		return "/"
	}
	return "/?" + q.Encode()
}

func noticeFromQuery(r *http.Request) string {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		return ""
	}
	notice := state
	if videoID := strings.TrimSpace(r.URL.Query().Get("videoId")); videoID != "" {
		notice += " " + videoID
	}
	if errText := strings.TrimSpace(r.URL.Query().Get("error")); errText != "" {
		notice += ": " + errText
	}
	return notice
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>My Musi Downloader</title>
  {{if .Refresh}}<meta http-equiv="refresh" content="5">{{end}}
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    table { border-collapse: collapse; width: 100%; }
    th, td { border: 1px solid #ccc; padding: 0.4rem; vertical-align: top; }
    input[type=text] { width: 100%; box-sizing: border-box; }
    form { margin: 0; }
    .actions form { display: inline-block; margin-right: 0.25rem; }
    .error { color: #900; }
  </style>
</head>
<body>
  <h1>Recent page</h1>
  {{if .Notice}}<p class="error">{{.Notice}}</p>{{end}}
  <form method="post" action="/trigger">
    <label for="url">YouTube watch URL or video id</label>
    <input id="url" type="text" name="url" required>
    <button type="submit">Trigger</button>
  </form>

  <table>
    <thead>
      <tr>
        <th>Date</th>
        <th>Title</th>
        <th>Artist</th>
        <th>Genre</th>
        <th>State</th>
        <th>File name</th>
        <th>Actions</th>
      </tr>
    </thead>
    <tbody>
      {{range .Tracks}}
      <tr>
        <td>{{formatDate .}}</td>
        <td>
          <form id="edit-{{.VideoID}}" method="post" action="/tracks/{{.VideoID}}"></form>
          <input form="edit-{{.VideoID}}" type="text" name="title" value="{{.Title}}">
        </td>
        <td><input form="edit-{{.VideoID}}" type="text" name="artist" value="{{.Artist}}"></td>
        <td><input form="edit-{{.VideoID}}" type="text" name="genre" value="{{.Genre}}"></td>
        <td>
          {{.State}}
          {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
        </td>
        <td>{{.FileName}}</td>
        <td class="actions">
            <button form="edit-{{.VideoID}}" type="submit">Save</button>
          {{if or (eq .State "queued") (eq .State "downloading")}}
          <form method="post" action="/tracks/{{.VideoID}}/cancel"><button type="submit">Cancel</button></form>
          {{end}}
          {{if or (eq .State "failed") (eq .State "cancelled")}}
          <form method="post" action="/tracks/{{.VideoID}}/restart"><button type="submit">Restart</button></form>
          {{end}}
        </td>
      </tr>
      {{else}}
      <tr><td colspan="7">No tracks touched in the last month.</td></tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`
