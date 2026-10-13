package track

import (
	"context"
	"testing"

	"mymusidownloader/internal/youtube"
)

func TestArtistFromTitle(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "Daft Punk - One More Time", want: "Daft Punk"},
		{title: "Artist – Song (Official)", want: "Artist"},
		{title: "Jay-Z - Hard Knock Life", want: "Jay-Z"},
		{title: "Song Without Dash", want: ""},
		{title: "Hyphenated-Title", want: ""},
		{title: "Trailing dash -", want: ""},
		{title: "  Spaced Artist  -  Song  ", want: "Spaced Artist"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := ArtistFromTitle(tt.title); got != tt.want {
				t.Fatalf("ArtistFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestGuessArtistPrefersMetadata(t *testing.T) {
	if got := guessArtist("Meta Artist", "Title Artist - Song"); got != "Meta Artist" {
		t.Fatalf("guessArtist with metadata = %q, want Meta Artist", got)
	}
	if got := guessArtist("", "Title Artist - Song"); got != "Title Artist" {
		t.Fatalf("guessArtist from title = %q, want Title Artist", got)
	}
	if got := guessArtist("", "Song Only"); got != "" {
		t.Fatalf("guessArtist without dash = %q, want empty", got)
	}
}

func TestServiceFillsArtistFromMetadataThenTitle(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "Band - Song", artist: "Meta Band", codec: "mp3", ext: "mp3", bitrate: 128},
		"aaaaaaaaaaa": {title: "Band - Other", codec: "mp3", ext: "mp3", bitrate: 128},
	}}
	service, err := NewService(library, 2, store, downloader, &fakeMediaTool{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dQw4w9WgXcQ", "aaaaaaaaaaa"} {
		if _, err := service.Trigger(context.Background(), id, Cut{}); err != nil {
			t.Fatal(err)
		}
	}
	waitForState(t, service, "dQw4w9WgXcQ", StateDone)
	waitForState(t, service, "aaaaaaaaaaa", StateDone)
	meta, _ := service.Get("dQw4w9WgXcQ")
	fromTitle, _ := service.Get("aaaaaaaaaaa")
	if meta.Artist != "Meta Band" {
		t.Fatalf("metadata artist = %q, want Meta Band", meta.Artist)
	}
	if fromTitle.Artist != "Band" {
		t.Fatalf("title artist = %q, want Band", fromTitle.Artist)
	}
}

func TestParseCut(t *testing.T) {
	tests := []struct {
		start, end string
		want       Cut
		wantErr    bool
	}{
		{start: "", end: "", want: Cut{}},
		{start: "90", end: "", want: Cut{Start: 90}},
		{start: "1:30", end: "2:00", want: Cut{Start: 90, End: 120}},
		{start: "", end: "1:02:03", want: Cut{End: 3723}},
		{start: "0:10", end: "0:05", wantErr: true},
		{start: "abc", end: "", wantErr: true},
		{start: "1:2:3:4", end: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseCut(tt.start, tt.end)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseCut(%q,%q) err = %v, wantErr %v", tt.start, tt.end, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("ParseCut(%q,%q) = %+v, want %+v", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestCutLength(t *testing.T) {
	if got := (Cut{Start: 10, End: 25}).Length(300); got != 15 {
		t.Fatalf("Length with end = %d, want 15", got)
	}
	if got := (Cut{Start: 60}).Length(200); got != 140 {
		t.Fatalf("Length to end = %d, want 140", got)
	}
	if got := (Cut{Start: 10, End: 500}).Length(120); got != 110 {
		t.Fatalf("Length with end past file = %d, want 110", got)
	}
}

func TestServiceAppliesCutToDownload(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	mediaTool := &fakeMediaTool{}
	downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "Cut Me", codec: "mp3", ext: "mp3", bitrate: 128},
	}}
	service, err := NewService(library, 1, store, downloader, mediaTool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Trigger(context.Background(), youtube.WatchURL("dQw4w9WgXcQ"), Cut{Start: 10, End: 25}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, service, "dQw4w9WgXcQ", StateDone)
	mediaTool.mu.Lock()
	defer mediaTool.mu.Unlock()
	if len(mediaTool.cutCalls) != 1 || mediaTool.cutCalls[0] != (Cut{Start: 10, End: 25}) {
		t.Fatalf("cut calls = %+v, want one {10 25}", mediaTool.cutCalls)
	}
	tr, _ := service.Get("dQw4w9WgXcQ")
	if tr.Cut != (Cut{Start: 10, End: 25}) {
		t.Fatalf("track cut = %+v", tr.Cut)
	}
}
