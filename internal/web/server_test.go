package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

	artworkPath string
	artworkOK   bool
}

func (s *stubApp) Trigger(ctx context.Context, input string) (track.StartResult, error) {
	s.triggerInput = input
	return s.triggerResult, nil
}

func (s *stubApp) Update(videoID, title, artist, genre, album string) (track.Track, error) {
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

func (s *stubApp) Delete(videoID string) error {
	return nil
}

func (s *stubApp) Convert(ctx context.Context, videoID string, equivalent bool) (track.Track, error) {
	return s.trackResult, nil
}

func (s *stubApp) Restart(ctx context.Context, videoID string) (track.StartResult, error) {
	s.restartVideo = videoID
	return s.triggerResult, nil
}

func (s *stubApp) Recent() ([]track.Track, bool) {
	return s.recentTracks, s.recentReload
}

func (s *stubApp) ArtworkPath(videoID string) (string, bool) {
	return s.artworkPath, s.artworkOK
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

func TestServerArtwork(t *testing.T) {
	dir := t.TempDir()
	artworkFile := dir + "/abc123def45.jpg"
	if err := os.WriteFile(artworkFile, []byte("fake-jpeg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &stubApp{artworkPath: artworkFile, artworkOK: true}
	server, err := New(app)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	t.Run("existing artwork served", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tracks/abc123def45/artwork", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != "fake-jpeg-bytes" {
			t.Fatalf("body = %q", rec.Body.String())
		}
	})

	t.Run("missing artwork is 404", func(t *testing.T) {
		missing := &stubApp{artworkOK: false}
		missingServer, err := New(missing)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/tracks/abc123def45/artwork", nil)
		rec := httptest.NewRecorder()
		missingServer.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("invalid video id is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tracks/not-valid/artwork", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("post method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/tracks/abc123def45/artwork", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestServerStaticAssets(t *testing.T) {
	server, err := New(&stubApp{})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	for _, path := range []string{"/static/style.css", "/static/manifest.json", "/static/icon-192.png", "/static/apple-touch-icon.png"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("empty body for %s", path)
			}
		})
	}
}

func TestRecentPageAppShellMeta(t *testing.T) {
	app := &stubApp{}
	server, err := New(app)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{
		`name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"`,
		`name="apple-mobile-web-app-capable" content="yes"`,
		`rel="manifest" href="/static/manifest.json"`,
		`name="theme-color" media="(prefers-color-scheme: dark)"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q\n%s", want, body)
		}
	}
}

func TestRecentPageArtworkRendering(t *testing.T) {
	tracks := []track.Track{{VideoID: "abc123def45", Title: "Has Art", State: track.StateDone}}

	withArt := &stubApp{recentTracks: tracks, artworkOK: true}
	withArtServer, err := New(withArt)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	withArtServer.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `<img src="/tracks/abc123def45/artwork"`) {
		t.Fatalf("expected artwork img tag when artwork exists: %s", body)
	}

	withoutArt := &stubApp{recentTracks: tracks, artworkOK: false}
	withoutArtServer, err := New(withoutArt)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	withoutArtServer.Handler().ServeHTTP(rec, req)
	body = rec.Body.String()
	if strings.Contains(body, "<img") {
		t.Fatalf("expected placeholder, not an img tag, when no artwork exists: %s", body)
	}
	if !strings.Contains(body, "art-placeholder") {
		t.Fatalf("expected placeholder element: %s", body)
	}
}
