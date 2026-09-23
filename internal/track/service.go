package track

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mymusidownloader/internal/media"
	"mymusidownloader/internal/youtube"
)

var (
	ErrNotFound          = errors.New("track not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	errStopped           = errors.New("track stopped")
	errUnknownBitrate    = errors.New("source bitrate unknown, refusing to convert above the source")
)

type DownloadedMedia struct {
	Path          string
	Title         string
	Codec         string
	Ext           string
	BitrateKbps   int
	OriginalURL   string
	NormalizedURL string
	Channel       string
	UploadDate    string
	DurationSec   int
	SampleRate    int
	Channels      int
}

type Downloader interface {
	Download(ctx context.Context, url, library, videoID string) (DownloadedMedia, error)
}

type MediaTool interface {
	ConvertToOpus(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags Tags) error
	WriteTags(ctx context.Context, path string, tags Tags) error
	Remux(ctx context.Context, inputPath, outputPath string) error
}

type Service struct {
	mu            sync.Mutex
	library       string
	maxConcurrent int
	store         *FileStore
	downloader    Downloader
	mediaTool     MediaTool
	now           func() time.Time

	tracks  map[string]*Track
	queue   []string
	active  int
	running map[string]context.CancelFunc
}

func NewService(library string, maxConcurrent int, store *FileStore, downloader Downloader, mediaTool MediaTool) (*Service, error) {
	if maxConcurrent <= 0 {
		return nil, errors.New("maxConcurrent must be greater than zero")
	}
	loaded, err := store.LoadAll()
	if err != nil {
		return nil, err
	}
	svc := &Service{
		library:       library,
		maxConcurrent: maxConcurrent,
		store:         store,
		downloader:    downloader,
		mediaTool:     mediaTool,
		now:           time.Now,
		tracks:        make(map[string]*Track, len(loaded)),
		running:       make(map[string]context.CancelFunc),
	}
	for _, tr := range loaded {
		copy := tr
		if copy.State == StateQueued || copy.State == StateDownloading {
			copy.State = StateFailed
			copy.Error = "interrupted by daemon restart"
			copy.TouchedAt = svc.now()
			if err := svc.store.Save(copy); err != nil {
				return nil, err
			}
		}
		svc.tracks[copy.VideoID] = &copy
	}
	return svc, nil
}

func (s *Service) SetNow(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Service) Trigger(_ context.Context, input string) (StartResult, error) {
	target, err := youtube.Parse(input)
	if err != nil {
		return StartResult{State: StateRejected, Error: err.Error()}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.tracks[target.VideoID]; ok {
		existing.URL = target.URL
		if s.hasAudioFileLocked(existing) {
			existing.TouchedAt = s.now()
			if err := s.store.Save(*existing); err != nil {
				return StartResult{}, err
			}
			return StartResult{VideoID: existing.VideoID, State: StateStored}, nil
		}
		switch existing.State {
		case StateQueued:
			return StartResult{VideoID: existing.VideoID, State: StateQueued}, nil
		case StateDownloading:
			return StartResult{VideoID: existing.VideoID, State: StateDownloading}, nil
		default:
			return s.startLocked(existing, target.URL)
		}
	}

	tr := &Track{
		VideoID:   target.VideoID,
		URL:       target.URL,
		TouchedAt: s.now(),
	}
	s.tracks[tr.VideoID] = tr
	return s.startLocked(tr, target.URL)
}

func (s *Service) Restart(_ context.Context, videoID string) (StartResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tr, ok := s.tracks[videoID]
	if !ok {
		return StartResult{}, ErrNotFound
	}
	if tr.State != StateFailed && tr.State != StateCancelled {
		return StartResult{}, ErrInvalidTransition
	}
	if tr.URL == "" {
		return StartResult{}, errors.New("track has no stored url")
	}
	if s.hasAudioFileLocked(tr) {
		tr.TouchedAt = s.now()
		if err := s.store.Save(*tr); err != nil {
			return StartResult{}, err
		}
		return StartResult{VideoID: tr.VideoID, State: StateStored}, nil
	}
	return s.startLocked(tr, tr.URL)
}

func (s *Service) Cancel(videoID string) (Track, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tr, ok := s.tracks[videoID]
	if !ok {
		return Track{}, ErrNotFound
	}
	switch tr.State {
	case StateQueued:
		s.queue = removeFromQueue(s.queue, videoID)
		tr.State = StateCancelled
		tr.Error = ""
		tr.TouchedAt = s.now()
		if err := s.store.Save(*tr); err != nil {
			return Track{}, err
		}
		if err := s.removePartials(videoID); err != nil {
			return Track{}, err
		}
		return *tr, nil
	case StateDownloading:
		cancel := s.running[videoID]
		tr.State = StateCancelled
		tr.Error = ""
		tr.TouchedAt = s.now()
		if err := s.store.Save(*tr); err != nil {
			return Track{}, err
		}
		if cancel != nil {
			cancel()
		}
		return *tr, nil
	default:
		return Track{}, ErrInvalidTransition
	}
}

func (s *Service) Update(videoID, title, artist, genre string) (Track, error) {
	s.mu.Lock()
	tr, ok := s.tracks[videoID]
	if !ok {
		s.mu.Unlock()
		return Track{}, ErrNotFound
	}
	updated := *tr
	updated.Title = title
	updated.Artist = artist
	updated.Genre = genre
	updated.TouchedAt = s.now()
	path := ""
	if s.hasAudioFileLocked(tr) {
		path = tr.AudioPath(s.library)
	}
	s.mu.Unlock()

	if path != "" {
		if err := s.mediaTool.WriteTags(context.Background(), path, updated.Tags()); err != nil {
			return Track{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tracks[videoID]
	if !ok {
		return Track{}, ErrNotFound
	}
	current.Title = updated.Title
	current.Artist = updated.Artist
	current.Genre = updated.Genre
	current.TouchedAt = updated.TouchedAt
	if err := s.store.Save(*current); err != nil {
		return Track{}, err
	}
	return *current, nil
}

func (s *Service) Recent() ([]Track, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	since := s.now().AddDate(0, -1, 0)
	tracks := RecentTracks(s.tracks, since)
	refresh := false
	for _, tr := range tracks {
		if tr.State == StateQueued || tr.State == StateDownloading {
			refresh = true
			break
		}
	}
	return tracks, refresh
}

func (s *Service) Get(videoID string) (Track, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tr, ok := s.tracks[videoID]
	if !ok {
		return Track{}, ErrNotFound
	}
	return *tr, nil
}

func (s *Service) startLocked(tr *Track, url string) (StartResult, error) {
	tr.URL = url
	tr.Error = ""
	tr.FileName = ""
	tr.StoredCodec = ""
	tr.TouchedAt = s.now()

	if s.active < s.maxConcurrent {
		tr.State = StateDownloading
		tr.DownloadStartedAt = s.now()
		s.active++
		ctx, cancel := context.WithCancel(context.Background())
		s.running[tr.VideoID] = cancel
		if err := s.store.Save(*tr); err != nil {
			delete(s.running, tr.VideoID)
			s.active--
			return StartResult{}, err
		}
		go s.runTrack(ctx, tr.VideoID, tr.URL)
		return StartResult{VideoID: tr.VideoID, State: StateDownloading}, nil
	}

	tr.State = StateQueued
	tr.DownloadStartedAt = time.Time{}
	if !contains(s.queue, tr.VideoID) {
		s.queue = append(s.queue, tr.VideoID)
	}
	if err := s.store.Save(*tr); err != nil {
		s.queue = removeFromQueue(s.queue, tr.VideoID)
		return StartResult{}, err
	}
	return StartResult{VideoID: tr.VideoID, State: StateQueued}, nil
}

func (s *Service) runTrack(ctx context.Context, videoID, url string) {
	defer s.completeRun(videoID)

	mediaFile, err := s.downloader.Download(ctx, url, s.library, videoID)
	if err != nil {
		s.failRun(videoID, mediaFile.Path, ctx.Err(), err)
		return
	}

	if s.stopped(videoID) || ctx.Err() != nil {
		s.failRun(videoID, mediaFile.Path, context.Canceled, context.Canceled)
		return
	}

	titleForName, tags, startedAt, err := s.captureMetadata(videoID, mediaFile)
	if err != nil {
		s.failRun(videoID, mediaFile.Path, ctx.Err(), err)
		return
	}

	finalName, storedCodec, storeErr := s.storeMedia(ctx, videoID, mediaFile, titleForName, startedAt, tags)
	if storeErr != nil {
		cleanup := mediaFile.Path
		if finalName != "" {
			cleanup = filepath.Join(s.library, finalName)
		}
		s.failRun(videoID, cleanup, ctx.Err(), storeErr)
		_ = os.Remove(mediaFile.Path)
		return
	}

	if err := s.markDone(videoID, finalName, storedCodec); err != nil {
		s.failRun(videoID, filepath.Join(s.library, finalName), ctx.Err(), err)
		return
	}
}

func (s *Service) captureMetadata(videoID string, mediaFile DownloadedMedia) (string, Tags, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tr, ok := s.tracks[videoID]
	if !ok {
		return "", Tags{}, time.Time{}, errors.New("track disappeared")
	}
	if tr.State == StateCancelled {
		return "", Tags{}, time.Time{}, errStopped
	}
	oldSourceTitle := tr.SourceTitle
	tr.SourceTitle = mediaFile.Title
	if tr.Title == "" || tr.Title == oldSourceTitle {
		tr.Title = mediaFile.Title
	}
	tr.SourceCodec = strings.ToLower(mediaFile.Codec)
	tr.SourceBitrateKbps = mediaFile.BitrateKbps
	if mediaFile.Channel != "" {
		tr.Channel = mediaFile.Channel
	}
	if mediaFile.UploadDate != "" {
		tr.UploadDate = mediaFile.UploadDate
	}
	if mediaFile.DurationSec > 0 {
		tr.DurationSec = mediaFile.DurationSec
	}
	if mediaFile.SampleRate > 0 {
		tr.SampleRate = mediaFile.SampleRate
	}
	if mediaFile.Channels > 0 {
		tr.Channels = mediaFile.Channels
	}
	tr.TouchedAt = s.now()
	if err := s.store.Save(*tr); err != nil {
		return "", Tags{}, time.Time{}, err
	}
	return tr.Title, tr.Tags(), tr.DownloadStartedAt, nil
}

func (s *Service) markDone(videoID, finalName, storedCodec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tr, ok := s.tracks[videoID]
	if !ok {
		return errors.New("track disappeared")
	}
	if tr.State == StateCancelled {
		_ = os.Remove(filepath.Join(s.library, finalName))
		return errStopped
	}
	tr.FileName = finalName
	tr.StoredCodec = storedCodec
	tr.State = StateDone
	tr.Error = ""
	tr.TouchedAt = s.now()
	return s.store.Save(*tr)
}

func (s *Service) failRun(videoID, path string, cancelErr, runErr error) {
	_ = s.removePartials(videoID)
	if path != "" {
		_ = os.Remove(path)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tr, ok := s.tracks[videoID]
	if !ok {
		return
	}
	if tr.State == StateCancelled || cancelErr != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, errStopped) {
		tr.State = StateCancelled
		tr.Error = ""
	} else {
		tr.State = StateFailed
		tr.Error = runErr.Error()
	}
	tr.FileName = ""
	tr.StoredCodec = ""
	tr.TouchedAt = s.now()
	_ = s.store.Save(*tr)
}

func (s *Service) completeRun(videoID string) {
	var (
		nextID  string
		nextURL string
		nextCtx context.Context
	)

	s.mu.Lock()
	delete(s.running, videoID)
	if s.active > 0 {
		s.active--
	}
	for len(s.queue) > 0 && s.active < s.maxConcurrent {
		nextID, s.queue = s.queue[0], s.queue[1:]
		tr, ok := s.tracks[nextID]
		if !ok || tr.State != StateQueued {
			nextID = ""
			continue
		}
		tr.State = StateDownloading
		tr.Error = ""
		tr.TouchedAt = s.now()
		tr.DownloadStartedAt = s.now()
		s.active++
		nextURL = tr.URL
		var cancel context.CancelFunc
		nextCtx, cancel = context.WithCancel(context.Background())
		s.running[nextID] = cancel
		if err := s.store.Save(*tr); err != nil {
			delete(s.running, nextID)
			s.active--
			tr.State = StateFailed
			tr.Error = err.Error()
			tr.TouchedAt = s.now()
			_ = s.store.Save(*tr)
			nextID = ""
			nextURL = ""
			continue
		}
		break
	}
	s.mu.Unlock()

	if nextID != "" {
		go s.runTrack(nextCtx, nextID, nextURL)
	}
}

func (s *Service) stopped(videoID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	tr, ok := s.tracks[videoID]
	return ok && tr.State == StateCancelled
}

func (s *Service) storeMedia(ctx context.Context, videoID string, downloaded DownloadedMedia, title string, startedAt time.Time, tags Tags) (string, string, error) {
	if s.stopped(videoID) || ctx.Err() != nil {
		return "", "", errStopped
	}
	codec := strings.ToLower(downloaded.Codec)
	ext := media.ExtensionForCodec(codec, downloaded.Ext)
	if startedAt.IsZero() {
		startedAt = s.now()
	}

	if media.KeepSourceCodec(codec) {
		candidateName := media.BuildFinalName(startedAt.Local(), title, videoID, ext, media.CandidateExistsInDir(s.library))
		finalPath := filepath.Join(s.library, candidateName)
		sourceExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(downloaded.Path)), ".")
		if sourceExt != "" && sourceExt != ext {
			if err := s.mediaTool.Remux(ctx, downloaded.Path, finalPath); err != nil {
				_ = os.Remove(finalPath)
				return "", "", fmt.Errorf("remux audio file: %w", err)
			}
			_ = os.Remove(downloaded.Path)
		} else if err := os.Rename(downloaded.Path, finalPath); err != nil {
			return "", "", fmt.Errorf("rename audio file: %w", err)
		}
		if err := s.mediaTool.WriteTags(ctx, finalPath, tags); err != nil {
			_ = os.Remove(finalPath)
			return "", "", fmt.Errorf("write tags: %w", err)
		}
		return candidateName, codec, nil
	}

	candidateName := media.BuildFinalName(startedAt.Local(), title, videoID, "opus", media.CandidateExistsInDir(s.library))
	finalPath := filepath.Join(s.library, candidateName)
	bitrate := downloaded.BitrateKbps
	if bitrate <= 0 {
		return "", "", errUnknownBitrate
	}
	if s.stopped(videoID) || ctx.Err() != nil {
		return "", "", errStopped
	}
	if err := s.mediaTool.ConvertToOpus(ctx, downloaded.Path, finalPath, bitrate, tags); err != nil {
		_ = os.Remove(finalPath)
		return "", "", fmt.Errorf("convert to opus: %w", err)
	}
	if err := os.Remove(downloaded.Path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(finalPath)
		return "", "", fmt.Errorf("remove source file: %w", err)
	}
	return candidateName, "opus", nil
}

func (s *Service) hasAudioFileLocked(tr *Track) bool {
	if tr.FileName == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(s.library, tr.FileName))
	return err == nil
}

func (s *Service) removePartials(videoID string) error {
	matches, err := filepath.Glob(filepath.Join(s.library, videoID+"*"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for _, match := range matches {
		base := filepath.Base(match)
		if base == videoID+".json" || strings.HasSuffix(base, ".json.tmp") {
			continue
		}
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func removeFromQueue(queue []string, videoID string) []string {
	filtered := queue[:0]
	for _, item := range queue {
		if item != videoID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
