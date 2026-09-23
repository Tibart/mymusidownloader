package track

import (
	"path/filepath"
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
	Genre             string    `json:"genre"`
	Channel           string    `json:"channel,omitempty"`
	UploadDate        string    `json:"uploadDate,omitempty"`
	DurationSec       int       `json:"durationSec,omitempty"`
	SampleRate        int       `json:"sampleRate,omitempty"`
	Channels          int       `json:"channels,omitempty"`
	State             State     `json:"state"`
	FileName          string    `json:"fileName,omitempty"`
	Error             string    `json:"error,omitempty"`
	TouchedAt         time.Time `json:"touchedAt"`
	DownloadStartedAt time.Time `json:"downloadStartedAt,omitempty"`
	SourceCodec       string    `json:"sourceCodec,omitempty"`
	StoredCodec       string    `json:"storedCodec,omitempty"`
	SourceBitrateKbps int       `json:"sourceBitrateKbps,omitempty"`
}

func AlbumName(artist, album string) string {
	album = strings.TrimSpace(album)
	if album != "" {
		return album
	}
	artist = strings.TrimSpace(artist)
	if artist == "" {
		return ""
	}
	return artist + " Live"
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
	Channel     string
	UploadDate  string
	SourceURL   string
	Codec       string
	BitrateKbps int
	DurationSec int
	SampleRate  int
	Channels    int
	ArtworkPath string
}

func (t Track) Tags() Tags {
	return Tags{
		Title:       t.Title,
		Artist:      t.Artist,
		Genre:       t.Genre,
		Album:       AlbumName(t.Artist, t.Album),
		Channel:     t.Channel,
		UploadDate:  t.UploadDate,
		SourceURL:   t.URL,
		Codec:       t.StoredCodec,
		BitrateKbps: t.SourceBitrateKbps,
		DurationSec: t.DurationSec,
		SampleRate:  t.SampleRate,
		Channels:    t.Channels,
	}
}
