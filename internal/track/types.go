package track

import (
	"path/filepath"
	"time"
)

type State string

const (
	StateRejected    State = "rejected"
	StateStored      State = "stored"
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
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
	Genre             string    `json:"genre"`
	State             State     `json:"state"`
	FileName          string    `json:"fileName,omitempty"`
	Error             string    `json:"error,omitempty"`
	TouchedAt         time.Time `json:"touchedAt"`
	DownloadStartedAt time.Time `json:"downloadStartedAt,omitempty"`
	SourceCodec       string    `json:"sourceCodec,omitempty"`
	StoredCodec       string    `json:"storedCodec,omitempty"`
	SourceBitrateKbps int       `json:"sourceBitrateKbps,omitempty"`
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
	Title  string
	Artist string
	Genre  string
}

func (t Track) Tags() Tags {
	return Tags{Title: t.Title, Artist: t.Artist, Genre: t.Genre}
}
