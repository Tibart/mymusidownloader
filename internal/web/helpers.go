package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"mymusidownloader/internal/track"
)

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

type triggerInput struct {
	URL   string `json:"url"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// readTriggerInput reads the URL and optional cut times from JSON or a form.
func readTriggerInput(r *http.Request) (string, track.Cut, error) {
	var in triggerInput
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return "", track.Cut{}, fmt.Errorf("decode body: %w", err)
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return "", track.Cut{}, err
		}
		in = triggerInput{URL: r.FormValue("url"), Start: r.FormValue("start"), End: r.FormValue("end")}
	}
	if strings.TrimSpace(in.URL) == "" {
		return "", track.Cut{}, errors.New("url is required")
	}
	cut, err := track.ParseCut(in.Start, in.End)
	if err != nil {
		return "", track.Cut{}, err
	}
	return strings.TrimSpace(in.URL), cut, nil
}
