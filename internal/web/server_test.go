package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mymusidownloader/internal/track"
)

type stubApp struct {
	triggerInput string
	editVideoID  string
	editTitle    string
	editArtist   string
	editGenre    string
	cancelVideo  string
	restartVideo string

	triggerResult track.StartResult
	trackResult   track.Track
	recentTracks  []track.Track
	recentReload  bool
}

func (s *stubApp) Trigger(ctx context.Context, input string) (track.StartResult, error) {
	s.triggerInput = input
	return s.triggerResult, nil
}

func (s *stubApp) Update(videoID, title, artist, genre string) (track.Track, error) {
	s.editVideoID = videoID
	s.editTitle = title
	s.editArtist = artist
	s.editGenre = genre
	return s.trackResult, nil
}

func (s *stubApp) Cancel(videoID string) (track.Track, error) {
	s.cancelVideo = videoID
	return s.trackResult, nil
}

func (s *stubApp) Restart(ctx context.Context, videoID string) (track.StartResult, error) {
	s.restartVideo = videoID
	return s.triggerResult, nil
}

func (s *stubApp) Recent() ([]track.Track, bool) {
	return s.recentTracks, s.recentReload
}

func TestServerRoutes(t *testing.T) {
	app := &stubApp{
		triggerResult: track.StartResult{VideoID: "abc123def45", State: track.StateDownloading},
		trackResult: track.Track{
			VideoID:           "abc123def45",
			Title:             "Edited",
			Artist:            "Artist",
			Genre:             "Genre",
			State:             track.StateCancelled,
			FileName:          "2024-07-06_Edited.mp3",
			DownloadStartedAt: time.Date(2024, 7, 6, 12, 0, 0, 0, time.Local),
		},
		recentTracks: []track.Track{{
			VideoID:           "abc123def45",
			Title:             "Visible",
			Artist:            "Artist",
			Genre:             "Genre",
			State:             track.StateDownloading,
			FileName:          "2024-07-06_Visible.mp3",
			DownloadStartedAt: time.Date(2024, 7, 6, 12, 0, 0, 0, time.Local),
		}},
		recentReload: true,
	}
	server, err := New(app)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		contentTyp string
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "trigger json",
			method:     http.MethodPost,
			target:     "/trigger",
			body:       `{"url":"https://www.youtube.com/watch?v=abc123def45"}`,
			contentTyp: "application/json",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				var result track.StartResult
				if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
					t.Fatal(err)
				}
				if result.VideoID != "abc123def45" || result.State != track.StateDownloading {
					t.Fatalf("unexpected result: %+v", result)
				}
				if app.triggerInput != "https://www.youtube.com/watch?v=abc123def45" {
					t.Fatalf("trigger input = %q", app.triggerInput)
				}
			},
		},
		{
			name:       "edit json",
			method:     http.MethodPost,
			target:     "/tracks/abc123def45",
			body:       `{"title":"Edited","artist":"Artist","genre":"Genre"}`,
			contentTyp: "application/json",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				if app.editVideoID != "abc123def45" || app.editTitle != "Edited" || app.editArtist != "Artist" || app.editGenre != "Genre" {
					t.Fatalf("unexpected edit call: %q %q %q %q", app.editVideoID, app.editTitle, app.editArtist, app.editGenre)
				}
			},
		},
		{
			name:       "cancel route",
			method:     http.MethodPost,
			target:     "/tracks/abc123def45/cancel",
			contentTyp: "application/json",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				if app.cancelVideo != "abc123def45" {
					t.Fatalf("cancel video = %q", app.cancelVideo)
				}
			},
		},
		{
			name:       "restart route",
			method:     http.MethodPost,
			target:     "/tracks/abc123def45/restart",
			contentTyp: "application/json",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				if app.restartVideo != "abc123def45" {
					t.Fatalf("restart video = %q", app.restartVideo)
				}
			},
		},
		{
			name:   "recent page",
			method: http.MethodGet,
			target: "/",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				body := rec.Body.String()
				if !strings.Contains(body, "http-equiv=\"refresh\" content=\"5\"") {
					t.Fatalf("page missing refresh meta tag: %s", body)
				}
				if !strings.Contains(body, "Visible") || !strings.Contains(body, "2024-07-06_Visible.mp3") {
					t.Fatalf("page missing track content: %s", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *bytes.Reader
			if tt.body == "" {
				body = bytes.NewReader(nil)
			} else {
				body = bytes.NewReader([]byte(tt.body))
			}
			req := httptest.NewRequest(tt.method, tt.target, body)
			if tt.contentTyp != "" {
				req.Header.Set("Content-Type", tt.contentTyp)
				req.Header.Set("Accept", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			tt.check(t, rec)
		})
	}
}
