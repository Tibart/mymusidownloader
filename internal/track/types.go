package track

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type State string

const (
	StateRejected    State = "rejected"
	StateStored      State = "stored"
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StateConverting  State = "converting"
	StateDone        State = "done"
	StateFailed      State = "failed"
	StateCancelled   State = "cancelled"
)

type Track struct {
	VideoID           string    `json:"videoId"`
	URL               string    `json:"url"`
	Title             string    `json:"title"`
	SourceTitle       string    `json:"sourceTitle,omitempty"`
	Artist            string    `json:"artist"`
	Album             string    `json:"album"`
	VariousArtists    bool      `json:"variousArtists"`
	Genre             string    `json:"genre"`
	Channel           string    `json:"channel,omitempty"`
	UploadDate        string    `json:"uploadDate,omitempty"`
	DurationSec       int       `json:"durationSec,omitempty"`
	SampleRate        int       `json:"sampleRate,omitempty"`
	Channels          int       `json:"channels,omitempty"`
	State             State     `json:"state"`
	FileName          string    `json:"fileName,omitempty"`
	Event             string    `json:"event,omitempty"`
	Cut               Cut       `json:"cut"`
	Error             string    `json:"error,omitempty"`
	TouchedAt         time.Time `json:"touchedAt"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
	DownloadStartedAt time.Time `json:"downloadStartedAt,omitempty"`
	SourceCodec       string    `json:"sourceCodec,omitempty"`
	StoredCodec       string    `json:"storedCodec,omitempty"`
	SourceBitrateKbps int       `json:"sourceBitrateKbps,omitempty"`
}

func (t Track) CreatedOn() time.Time {
	if !t.CreatedAt.IsZero() {
		return t.CreatedAt
	}
	if !t.DownloadStartedAt.IsZero() {
		return t.DownloadStartedAt
	}
	base := filepath.Base(t.FileName)
	if len(base) >= 10 {
		if parsed, err := time.Parse("2006-01-02", base[:10]); err == nil {
			return parsed
		}
	}
	return t.TouchedAt
}

func AlbumName(title, album string) string {
	album = strings.TrimSpace(album)
	if album != "" {
		return album
	}
	return strings.TrimSpace(title)
}

// artistTitlePattern needs spaces around the dash, so "Jay-Z" is not split.
var artistTitlePattern = regexp.MustCompile(`^(.+?)\s+[-\x{2013}\x{2014}]\s+\S`)

func ArtistFromTitle(title string) string {
	m := artistTitlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func AlbumArtist(artist string, various bool) string {
	if various {
		return "Various Artists"
	}
	return strings.TrimSpace(artist)
}

func (t Track) AudioPath(library string) string {
	if t.FileName == "" {
		return ""
	}
	return filepath.Join(library, t.FileName)
}

type StartResult struct {
	VideoID string `json:"videoId"`
	State   State  `json:"state"`
	Error   string `json:"error,omitempty"`
}

type Tags struct {
	Title       string
	Artist      string
	Genre       string
	Album       string
	AlbumArtist string
	Channel     string
	UploadDate  string
	SourceURL   string
	Codec       string
	BitrateKbps int
	DurationSec int
	SampleRate  int
	Channels    int
	ArtworkPath string
	Event       string
}

func (t Track) Tags() Tags {
	return Tags{
		Title:       t.Title,
		Artist:      t.Artist,
		Genre:       t.Genre,
		Album:       AlbumName(t.Title, t.Album),
		AlbumArtist: AlbumArtist(t.Artist, t.VariousArtists),
		Channel:     t.Channel,
		UploadDate:  t.UploadDate,
		SourceURL:   t.URL,
		Event:       t.Event,
		Codec:       t.StoredCodec,
		BitrateKbps: t.SourceBitrateKbps,
		DurationSec: t.DurationSec,
		SampleRate:  t.SampleRate,
		Channels:    t.Channels,
	}
}

// Cut limits a download to [Start, End) in seconds. End 0 means to the end.
type Cut struct {
	Start int `json:"start,omitempty"`
	End   int `json:"end,omitempty"`
}

func (c Cut) Active() bool {
	return c.Start > 0 || c.End > 0
}

// Length returns the cut duration in seconds for a source of full seconds.
func (c Cut) Length(full int) int {
	if c.End > 0 {
		end := c.End
		if full > 0 && end > full {
			end = full
		}
		if end > c.Start {
			return end - c.Start
		}
		return 0
	}
	if full > c.Start {
		return full - c.Start
	}
	return 0
}

// ParseCut reads optional start and end values such as "90", "1:30" or "1:02:03".
func ParseCut(start, end string) (Cut, error) {
	s, err := parseCutTime(start)
	if err != nil {
		return Cut{}, fmt.Errorf("start: %w", err)
	}
	e, err := parseCutTime(end)
	if err != nil {
		return Cut{}, fmt.Errorf("end: %w", err)
	}
	if e > 0 && e <= s {
		return Cut{}, errors.New("end must be after start")
	}
	return Cut{Start: s, End: e}, nil
}

func parseCutTime(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("%q is not a time", value)
	}
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a time", value)
		}
		total = total*60 + n
	}
	return total, nil
}
