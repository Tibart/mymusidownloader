package youtube

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Target struct {
	VideoID string
	URL     string
}

func Parse(input string) (Target, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Target{}, errors.New("empty input")
	}
	if videoIDPattern.MatchString(trimmed) {
		return Target{VideoID: trimmed, URL: WatchURL(trimmed)}, nil
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return Target{}, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Target{}, errors.New("unsupported url scheme")
	}

	host := strings.ToLower(u.Hostname())
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
	default:
		return Target{}, errors.New("unsupported youtube host")
	}

	if u.Path != "/watch" {
		return Target{}, errors.New("only youtube watch urls are accepted")
	}
	query := u.Query()
	if query.Get("list") != "" {
		return Target{}, errors.New("playlists are not accepted")
	}
	videoID := query.Get("v")
	if !videoIDPattern.MatchString(videoID) {
		return Target{}, errors.New("invalid video id")
	}
	return Target{VideoID: videoID, URL: WatchURL(videoID)}, nil
}

func WatchURL(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}
