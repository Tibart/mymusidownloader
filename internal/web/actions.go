package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request, videoID string) {
	payload := struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Genre  string `json:"genre"`
		Album  string `json:"album"`
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
		payload.Album = r.FormValue("album")
	}
	tr, err := s.app.Update(videoID, payload.Title, payload.Artist, payload.Genre, payload.Album)
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

func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request, videoID string) {
	equivalent := true
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Equivalent *bool `json:"equivalent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			writeError(w, r, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
			return
		}
		if body.Equivalent != nil {
			equivalent = *body.Equivalent
		}
	} else if err := r.ParseForm(); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	} else if value := r.FormValue("equivalent"); value != "" {
		equivalent = value != "false"
	}
	if !equivalent {
		writeError(w, r, http.StatusBadRequest, errors.New("only equivalent aac conversion is allowed"))
		return
	}
	tr, err := s.app.Convert(r.Context(), videoID, true)
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

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, videoID string) {
	if err := s.app.Delete(videoID); err != nil {
		writeTrackError(w, r, err)
		return
	}
	if wantsJSON(r) {
		w.WriteHeader(http.StatusNoContent)
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
