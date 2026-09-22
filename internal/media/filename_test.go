package media

import (
	"testing"
	"time"
)

func TestPascalTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "hello world", want: "HelloWorld"},
		{input: "already-LOUD_title", want: "AlreadyLoudTitle"},
		{input: "  99 luft balloons  ", want: "99LuftBalloons"},
		{input: "!!!", want: "Track"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := PascalTitle(tt.input); got != tt.want {
				t.Fatalf("PascalTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildFinalName(t *testing.T) {
	date := time.Date(2024, 7, 6, 13, 14, 0, 0, time.FixedZone("local", 0))
	tests := []struct {
		name   string
		title  string
		video  string
		ext    string
		exists map[string]bool
		want   string
	}{
		{
			name:  "plain filename",
			title: "hello world",
			video: "dQw4w9WgXcQ",
			ext:   "mp3",
			want:  "2024-07-06_HelloWorld.mp3",
		},
		{
			name:   "collision adds video id",
			title:  "hello world",
			video:  "dQw4w9WgXcQ",
			ext:    "opus",
			exists: map[string]bool{"2024-07-06_HelloWorld.opus": true},
			want:   "2024-07-06_HelloWorld_dQw4w9WgXcQ.opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := func(name string) bool { return tt.exists[name] }
			if got := BuildFinalName(date, tt.title, tt.video, tt.ext, exists); got != tt.want {
				t.Fatalf("BuildFinalName(...) = %q, want %q", got, tt.want)
			}
		})
	}
}
