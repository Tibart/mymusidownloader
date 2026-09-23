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
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) ArtworkPath(videoID string) string {
	return filepath.Join(s.dir, videoID+".jpg")
}

func (s *FileStore) LoadAll() ([]Track, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read metadata path: %w", err)
	}
	tracks := make([]Track, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
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
	tmpPath := filepath.Join(s.dir, tr.VideoID+".json.tmp")
	finalPath := filepath.Join(s.dir, tr.VideoID+".json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write sidecar temp %s: %w", tr.VideoID, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename sidecar %s: %w", tr.VideoID, err)
	}
	return nil
}

func (s *FileStore) Delete(videoID string) error {
	for _, name := range []string{videoID + ".json", videoID + ".json.tmp", videoID + ".jpg"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
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
