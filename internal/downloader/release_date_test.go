package downloader

import "testing"

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
