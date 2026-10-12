package track

import (
	"context"
	"testing"
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
		if _, err := service.Trigger(context.Background(), id); err != nil {
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
