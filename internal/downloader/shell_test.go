package downloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mymusidownloader/internal/track"
)

func TestReleaseDatePrecedence(t *testing.T) {
	tests := []struct {
		name string
		info ytDLPInfo
		want string
	}{
		{name: "release date wins", info: ytDLPInfo{ReleaseDate: "20220520", ReleaseYear: 2022, UploadDate: "20091025"}, want: "20220520"},
		{name: "release year next", info: ytDLPInfo{ReleaseYear: 1987, UploadDate: "20091025"}, want: "1987"},
		{name: "upload date last", info: ytDLPInfo{UploadDate: "20091025"}, want: "20091025"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := releaseDate(tt.info); got != tt.want {
				t.Fatalf("releaseDate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCutArgsSeekAfterInput(t *testing.T) {
	got := strings.Join(cutArgs("in.webm", "out.webm", track.Cut{Start: 60, End: 70}), " ")
	want := "-y -i in.webm -ss 60 -to 70 -map 0:a -c copy out.webm"
	if got != want {
		t.Fatalf("cutArgs = %q, want %q", got, want)
	}
	if open := strings.Join(cutArgs("in.webm", "out.webm", track.Cut{Start: 60}), " "); strings.Contains(open, "-to") {
		t.Fatalf("open-ended cut has -to: %q", open)
	}
}

func TestFFmpegCutLive(t *testing.T) {
	src := os.Getenv("CUT_SOURCE")
	if src == "" {
		t.Skip("CUT_SOURCE not set")
	}
	out := filepath.Join(t.TempDir(), "cut.webm")
	if err := (FFmpegMediaTool{FFmpegPath: "ffmpeg"}).Cut(context.Background(), src, out, track.Cut{Start: 60, End: 70}); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil || seconds < 9.9 || seconds > 10.1 {
		t.Fatalf("cut duration = %q, want about 10s", raw)
	}
}
