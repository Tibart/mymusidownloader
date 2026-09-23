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
	case "youtu.be":
		videoID := strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
		if i := strings.Index(videoID, "/"); i >= 0 {
			videoID = videoID[:i]
		}
		if !videoIDPattern.MatchString(videoID) {
			return Target{}, errors.New("invalid video id")
		}
		return Target{VideoID: videoID, URL: WatchURL(videoID)}, nil
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
	default:
		return Target{}, errors.New("unsupported youtube host")
	}

	if u.Path == "/playlist" || strings.HasPrefix(u.Path, "/channel/") || strings.HasPrefix(u.Path, "/@") || u.Path == "/results" {
		return Target{}, errors.New("only a single youtube video is accepted")
	}
	videoID := videoIDFromPath(u)
	if !videoIDPattern.MatchString(videoID) {
		return Target{}, errors.New("only a single youtube video is accepted")
	}
	return Target{VideoID: videoID, URL: WatchURL(videoID)}, nil
}

func videoIDFromPath(u *url.URL) string {
	if u.Path == "/watch" {
		return u.Query().Get("v")
	}
	for _, prefix := range []string{"/shorts/", "/embed/", "/live/", "/v/"} {
		if strings.HasPrefix(u.Path, prefix) {
			id := strings.Trim(strings.TrimPrefix(u.Path, prefix), "/")
			if i := strings.Index(id, "/"); i >= 0 {
				id = id[:i]
			}
			return id
		}
	}
	return ""
}

func WatchURL(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}
