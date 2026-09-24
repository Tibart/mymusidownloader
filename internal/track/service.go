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
	ArtworkPath   string
}

type Downloader interface {
	Download(ctx context.Context, url, library, videoID string) (DownloadedMedia, error)
}

type MediaTool interface {
	ConvertToOpus(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags Tags) error
	ConvertToAAC(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags Tags) error
	WriteTags(ctx context.Context, path string, tags Tags) error
	Remux(ctx context.Context, inputPath, outputPath string) error
	PrepareArtwork(ctx context.Context, inputPath, outputPath string) error
	LabelArtwork(ctx context.Context, inputPath, album, outputPath string) error
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

func (s *Service) Delete(videoID string) error {
	s.mu.Lock()
	tr, ok := s.tracks[videoID]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if tr.State == StateQueued || tr.State == StateDownloading || tr.State == StateConverting {
		s.mu.Unlock()
		return ErrInvalidTransition
	}
	audio := ""
	if tr.FileName != "" {
		audio = tr.AudioPath(s.library)
	}
	delete(s.tracks, videoID)
	s.queue = removeFromQueue(s.queue, videoID)
	s.mu.Unlock()

	if audio != "" {
		_ = os.Remove(audio)
	}
	_ = s.removePartials(videoID)
	return s.store.Delete(videoID)
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

func (s *Service) Update(videoID, title, artist, genre, album string, variousArtists bool) (Track, error) {
	s.mu.Lock()
	tr, ok := s.tracks[videoID]
	if !ok {
		s.mu.Unlock()
		return Track{}, ErrNotFound
	}
	if tr.State == StateConverting || tr.State == StateQueued || tr.State == StateDownloading {
		s.mu.Unlock()
		return Track{}, ErrInvalidTransition
	}
	updated := *tr
	updated.Title = title
	updated.Artist = artist
	updated.Genre = genre
	updated.Album = AlbumName(title, album)
	updated.VariousArtists = variousArtists
	s.store.RememberGenre(genre)
	updated.TouchedAt = s.now()
	path := ""
	if s.hasAudioFileLocked(tr) {
		path = tr.AudioPath(s.library)
	}
	s.mu.Unlock()

	s.refreshArtwork(&updated)
	if path != "" {
		if err := s.mediaTool.WriteTags(context.Background(), path, s.tagsFor(&updated)); err != nil {
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
	current.Album = updated.Album
	current.VariousArtists = updated.VariousArtists
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
			if tr.State == StateQueued || tr.State == StateDownloading || tr.State == StateConverting {
			refresh = true
			break
		}
	}
	return tracks, refresh
}

// ArtworkPath reports the cover JPEG path for videoID when the file exists on disk.
func (s *Service) ArtworkPath(videoID string) (string, bool) {
	path := s.store.ArtworkPath(videoID)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
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
	tags := s.tagsFor(tr)
	if mediaFile.ArtworkPath != "" {
		source := s.store.ArtworkSourcePath(videoID)
		if err := s.mediaTool.PrepareArtwork(context.Background(), mediaFile.ArtworkPath, source); err != nil {
			_ = os.Rename(mediaFile.ArtworkPath, source)
		} else if mediaFile.ArtworkPath != source {
			_ = os.Remove(mediaFile.ArtworkPath)
		}
		s.refreshArtwork(tr)
		if _, err := os.Stat(s.store.ArtworkPath(videoID)); err == nil {
			tags.ArtworkPath = s.store.ArtworkPath(videoID)
		}
	}
	return tr.Title, tags, tr.DownloadStartedAt, nil
}

func (s *Service) Genres() []string {
	return s.store.Genres()
}

func coverLabel(tr *Track) string {
	if artist := strings.TrimSpace(tr.Artist); artist != "" {
		return artist
	}
	return AlbumName(tr.Title, tr.Album)
}

func (s *Service) refreshArtwork(tr *Track) {
	label := coverLabel(tr)
	if label == "" {
		return
	}
	source := s.store.ArtworkSourcePath(tr.VideoID)
	display := s.store.ArtworkPath(tr.VideoID)
	if _, err := os.Stat(source); err != nil {
		if _, err := os.Stat(display); err != nil {
			return
		}
		data, err := os.ReadFile(display)
		if err != nil {
			return
		}
		if err := os.WriteFile(source, data, 0o644); err != nil {
			return
		}
	}
	_ = s.mediaTool.LabelArtwork(context.Background(), source, label, display)
}

func (s *Service) tagsFor(tr *Track) Tags {
	tags := tr.Tags()
	art := s.store.ArtworkPath(tr.VideoID)
	if _, err := os.Stat(art); err == nil {
		tags.ArtworkPath = art
	}
	return tags
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

func (s *Service) Convert(_ context.Context, videoID string, equivalent bool) (Track, error) {
	if !equivalent {
		return Track{}, errors.New("only equivalent aac conversion is allowed")
	}
	s.mu.Lock()
	tr, ok := s.tracks[videoID]
	if !ok {
		s.mu.Unlock()
		return Track{}, ErrNotFound
	}
	if tr.State == StateConverting {
		current := *tr
		s.mu.Unlock()
		return current, nil
	}
	if tr.State != StateDone || !s.hasAudioFileLocked(tr) {
		s.mu.Unlock()
		return Track{}, ErrInvalidTransition
	}
	bitrate := media.EquivalentAACBitrate(tr.SourceBitrateKbps)
	if bitrate <= 0 {
		s.mu.Unlock()
		return Track{}, errUnknownBitrate
	}
	sourcePath := tr.AudioPath(s.library)
	s.refreshArtwork(tr)
	tags := s.tagsFor(tr)
	title := tr.Title
	startedAt := tr.DownloadStartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	tr.State = StateConverting
	tr.Error = ""
	tr.TouchedAt = s.now()
	if err := s.store.Save(*tr); err != nil {
		tr.State = StateDone
		s.mu.Unlock()
		return Track{}, err
	}
	current := *tr
	s.mu.Unlock()

	go s.finishConvert(videoID, sourcePath, title, startedAt, bitrate, tags)
	return current, nil
}

func (s *Service) finishConvert(videoID, sourcePath, title string, startedAt time.Time, bitrate int, tags Tags) {
	tags.Codec = "aac"
	tags.BitrateKbps = bitrate
	candidateName := media.BuildFinalName(startedAt.Local(), title, videoID, "m4a", media.CandidateExistsInDir(s.library))
	finalPath := filepath.Join(s.library, candidateName)
	if err := s.mediaTool.ConvertToAAC(context.Background(), sourcePath, finalPath, bitrate, tags); err != nil {
		_ = os.Remove(finalPath)
		s.mu.Lock()
		defer s.mu.Unlock()
		tr, ok := s.tracks[videoID]
		if !ok {
			return
		}
		tr.State = StateDone
		tr.Error = err.Error()
		tr.TouchedAt = s.now()
		_ = s.store.Save(*tr)
		return
	}
	if finalPath != sourcePath {
		_ = os.Remove(sourcePath)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tracks[videoID]
	if !ok {
		_ = os.Remove(finalPath)
		return
	}
	current.FileName = candidateName
	current.StoredCodec = "aac"
	current.State = StateDone
	current.Error = ""
	current.TouchedAt = s.now()
	_ = s.store.Save(*current)
}

func (s *Service) storeAAC(ctx context.Context, videoID string, downloaded DownloadedMedia, title string, startedAt time.Time, tags Tags) (string, string, error) {
	bitrate := downloaded.BitrateKbps
	if bitrate <= 0 {
		return "", "", errUnknownBitrate
	}
	if s.stopped(videoID) || ctx.Err() != nil {
		return "", "", errStopped
	}
	candidateName := media.BuildFinalName(startedAt.Local(), title, videoID, "m4a", media.CandidateExistsInDir(s.library))
	finalPath := filepath.Join(s.library, candidateName)
	tags.Codec = "aac"
	tags.BitrateKbps = bitrate
	if err := s.mediaTool.ConvertToAAC(ctx, downloaded.Path, finalPath, bitrate, tags); err != nil {
		_ = os.Remove(finalPath)
		return "", "", fmt.Errorf("convert to aac: %w", err)
	}
	if err := os.Remove(downloaded.Path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(finalPath)
		return "", "", fmt.Errorf("remove source file: %w", err)
	}
	return candidateName, "aac", nil
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
