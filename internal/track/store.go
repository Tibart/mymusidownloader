package track

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileStore struct {
	library string
}

func NewFileStore(library string) *FileStore {
	return &FileStore{library: library}
}

func (s *FileStore) LoadAll() ([]Track, error) {
	entries, err := os.ReadDir(s.library)
	if err != nil {
		return nil, fmt.Errorf("read library: %w", err)
	}
	tracks := make([]Track, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.library, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read sidecar %s: %w", entry.Name(), err)
		}
		var tr Track
		if err := json.Unmarshal(data, &tr); err != nil {
			return nil, fmt.Errorf("parse sidecar %s: %w", entry.Name(), err)
		}
		if tr.VideoID == "" {
			continue
		}
		tracks = append(tracks, tr)
	}
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].TouchedAt.After(tracks[j].TouchedAt)
	})
	return tracks, nil
}

func (s *FileStore) Save(tr Track) error {
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal track %s: %w", tr.VideoID, err)
	}
	tmpPath := filepath.Join(s.library, tr.VideoID+".json.tmp")
	finalPath := filepath.Join(s.library, tr.VideoID+".json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write sidecar temp %s: %w", tr.VideoID, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename sidecar %s: %w", tr.VideoID, err)
	}
	return nil
}

func RecentTracks(all map[string]*Track, since time.Time) []Track {
	tracks := make([]Track, 0, len(all))
	for _, tr := range all {
		if tr.TouchedAt.Before(since) {
			continue
		}
		tracks = append(tracks, *tr)
	}
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].TouchedAt.Equal(tracks[j].TouchedAt) {
			return tracks[i].VideoID < tracks[j].VideoID
		}
		return tracks[i].TouchedAt.After(tracks[j].TouchedAt)
	})
	return tracks
}
