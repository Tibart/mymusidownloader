package downloader

import (
	"strings"
	"testing"
)

func TestMediaInputsPutMapsAfterInputs(t *testing.T) {
	args := mediaInputs("audio.opus", "")
	if strings.Join(args, " ") != "-y -i audio.opus -map 0:a" {
		t.Fatalf("args = %v", args)
	}
}

func TestAlbumLines(t *testing.T) {
	short := albumLines("Rick Astley Live")
	if len(short) != 1 || short[0] != "Rick Astley Live" {
		t.Fatalf("short = %#v", short)
	}
	long := albumLines("Jonathan Davis and the SFA Live")
	if len(long) != 2 {
		t.Fatalf("long = %#v", long)
	}
	if long[0] == "" || long[1] == "" {
		t.Fatalf("empty line in %#v", long)
	}
}
