package track

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mymusidownloader/internal/youtube"
)

type fakeDownloadBehavior struct {
	title   string
	codec   string
	ext     string
	bitrate int
	block   chan struct{}
	err     error
}

type fakeDownloader struct {
	mu        sync.Mutex
	behaviors map[string]fakeDownloadBehavior
	started   []string
}

func (f *fakeDownloader) Download(ctx context.Context, url, library, videoID string) (DownloadedMedia, error) {
	f.mu.Lock()
	behavior, ok := f.behaviors[videoID]
	if !ok {
		behavior = fakeDownloadBehavior{title: "Untitled", codec: "mp3", ext: "mp3", bitrate: 128}
	}
	f.started = append(f.started, videoID)
	f.mu.Unlock()

	ext := behavior.ext
	if ext == "" {
		ext = "bin"
	}
	path := filepath.Join(library, videoID+"."+ext)
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		return DownloadedMedia{}, err
	}
	if behavior.block != nil {
		select {
		case <-behavior.block:
		case <-ctx.Done():
			return DownloadedMedia{Path: path}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return DownloadedMedia{Path: path}, err
	}
	media := DownloadedMedia{
		Path:        path,
		Title:       behavior.title,
		Codec:       behavior.codec,
		Ext:         ext,
		BitrateKbps: behavior.bitrate,
	}
	if behavior.err != nil {
		return media, behavior.err
	}
	return media, nil
}

func (f *fakeDownloader) StartedCount(videoID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, id := range f.started {
		if id == videoID {
			count++
		}
	}
	return count
}

type convertCall struct {
	input   string
	output  string
	bitrate int
	tags    Tags
}

type writeCall struct {
	path string
	tags Tags
}

type fakeMediaTool struct {
	mu           sync.Mutex
	convertCalls []convertCall
	writeCalls   []writeCall
	holdWrite    chan struct{}
}

func (f *fakeMediaTool) ConvertToOpus(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags Tags) error {
	f.mu.Lock()
	f.convertCalls = append(f.convertCalls, convertCall{input: inputPath, output: outputPath, bitrate: bitrateKbps, tags: tags})
	f.mu.Unlock()
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append([]byte("opus:"), data...), 0o644)
}

func (f *fakeMediaTool) Remux(ctx context.Context, inputPath, outputPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0o644)
}

func (f *fakeMediaTool) WriteTags(ctx context.Context, path string, tags Tags) error {
	f.mu.Lock()
	f.writeCalls = append(f.writeCalls, writeCall{path: path, tags: tags})
	hold := f.holdWrite
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

func (f *fakeMediaTool) ConvertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.convertCalls)
}

func (f *fakeMediaTool) WriteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writeCalls)
}

func (f *fakeMediaTool) LastConvertCall() convertCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.convertCalls[len(f.convertCalls)-1]
}

func TestServiceStoredRestartAndDuplicateInFlight(t *testing.T) {
	t.Run("stored when audio exists", func(t *testing.T) {
		library := t.TempDir()
		store := NewFileStore(library)
		audioName := "2024-07-06_StoredTrack.mp3"
		if err := os.WriteFile(filepath.Join(library, audioName), []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		existing := Track{
			VideoID:   "dQw4w9WgXcQ",
			URL:       youtube.WatchURL("dQw4w9WgXcQ"),
			Title:     "Stored Track",
			State:     StateDone,
			FileName:  audioName,
			TouchedAt: time.Now(),
		}
		if err := store.Save(existing); err != nil {
			t.Fatal(err)
		}
		downloader := &fakeDownloader{}
		service, err := NewService(library, 1, store, downloader, &fakeMediaTool{})
		if err != nil {
			t.Fatal(err)
		}

		result, err := service.Trigger(context.Background(), "dQw4w9WgXcQ")
		if err != nil {
			t.Fatal(err)
		}
		if result.State != StateStored {
			t.Fatalf("state = %q, want %q", result.State, StateStored)
		}
		if downloader.StartedCount("dQw4w9WgXcQ") != 0 {
			t.Fatalf("unexpected download start count = %d", downloader.StartedCount("dQw4w9WgXcQ"))
		}
	})

	t.Run("failed or cancelled restarts same track", func(t *testing.T) {
		states := []State{StateFailed, StateCancelled}
		for _, state := range states {
			t.Run(string(state), func(t *testing.T) {
				library := t.TempDir()
				store := NewFileStore(library)
				existing := Track{
					VideoID:   "dQw4w9WgXcQ",
					URL:       youtube.WatchURL("dQw4w9WgXcQ"),
					Title:     "Before Restart",
					State:     state,
					TouchedAt: time.Now(),
				}
				if err := store.Save(existing); err != nil {
					t.Fatal(err)
				}
				downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
					"dQw4w9WgXcQ": {title: "Restarted Title", codec: "mp3", ext: "mp3", bitrate: 128},
				}}
				service, err := NewService(library, 1, store, downloader, &fakeMediaTool{})
				if err != nil {
					t.Fatal(err)
				}

				result, err := service.Trigger(context.Background(), youtube.WatchURL("dQw4w9WgXcQ"))
				if err != nil {
					t.Fatal(err)
				}
				if result.VideoID != "dQw4w9WgXcQ" {
					t.Fatalf("video id = %q", result.VideoID)
				}
				if result.State != StateDownloading {
					t.Fatalf("state = %q, want %q", result.State, StateDownloading)
				}
				waitForState(t, service, "dQw4w9WgXcQ", StateDone)
				if downloader.StartedCount("dQw4w9WgXcQ") != 1 {
					t.Fatalf("download starts = %d, want 1", downloader.StartedCount("dQw4w9WgXcQ"))
				}
			})
		}
	})

	t.Run("duplicate in flight returns downloading without second copy", func(t *testing.T) {
		library := t.TempDir()
		store := NewFileStore(library)
		block := make(chan struct{})
		downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
			"dQw4w9WgXcQ": {title: "Slow Track", codec: "mp3", ext: "mp3", bitrate: 128, block: block},
		}}
		service, err := NewService(library, 1, store, downloader, &fakeMediaTool{})
		if err != nil {
			t.Fatal(err)
		}

		first, err := service.Trigger(context.Background(), "dQw4w9WgXcQ")
		if err != nil {
			t.Fatal(err)
		}
		second, err := service.Trigger(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		if err != nil {
			t.Fatal(err)
		}
		if first.State != StateDownloading || second.State != StateDownloading {
			t.Fatalf("states = %q and %q, want downloading", first.State, second.State)
		}
		waitFor(t, func() bool { return downloader.StartedCount("dQw4w9WgXcQ") == 1 })
		if downloader.StartedCount("dQw4w9WgXcQ") != 1 {
			t.Fatalf("download starts = %d, want 1", downloader.StartedCount("dQw4w9WgXcQ"))
		}
		close(block)
		waitForState(t, service, "dQw4w9WgXcQ", StateDone)
	})
}

func TestServiceQueueLimitAndSlotRelease(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	block := make(chan struct{})
	downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "First", codec: "mp3", ext: "mp3", bitrate: 128, block: block},
		"9bZkp7q19f0": {title: "Second", codec: "mp3", ext: "mp3", bitrate: 128},
	}}
	service, err := NewService(library, 1, store, downloader, &fakeMediaTool{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.Trigger(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Trigger(context.Background(), "9bZkp7q19f0")
	if err != nil {
		t.Fatal(err)
	}
	if first.State != StateDownloading {
		t.Fatalf("first state = %q, want downloading", first.State)
	}
	if second.State != StateQueued {
		t.Fatalf("second state = %q, want queued", second.State)
	}
	waitForTrackStateRead(t, service, "9bZkp7q19f0", StateQueued)
	close(block)
	waitForState(t, service, "dQw4w9WgXcQ", StateDone)
	waitForState(t, service, "9bZkp7q19f0", StateDone)
	if downloader.StartedCount("9bZkp7q19f0") != 1 {
		t.Fatalf("second download starts = %d, want 1", downloader.StartedCount("9bZkp7q19f0"))
	}
}

func TestServiceCodecHandlingAndTagEdits(t *testing.T) {
	tests := []struct {
		name             string
		codec            string
		ext              string
		bitrate          int
		wantConvertCount int
		wantWriteCount   int
		wantStoredCodec  string
		wantBitrate      int
	}{
		{name: "keep source codec", codec: "mp3", ext: "mp3", bitrate: 192, wantConvertCount: 0, wantWriteCount: 1, wantStoredCodec: "mp3"},
		{name: "convert once to opus", codec: "vorbis", ext: "webm", bitrate: 96, wantConvertCount: 1, wantWriteCount: 0, wantStoredCodec: "opus", wantBitrate: 96},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			library := t.TempDir()
			store := NewFileStore(library)
			mediaTool := &fakeMediaTool{}
			downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
				"dQw4w9WgXcQ": {title: "A Test Title", codec: tt.codec, ext: tt.ext, bitrate: tt.bitrate},
			}}
			service, err := NewService(library, 1, store, downloader, mediaTool)
			if err != nil {
				t.Fatal(err)
			}

			result, err := service.Trigger(context.Background(), "dQw4w9WgXcQ")
			if err != nil {
				t.Fatal(err)
			}
			if result.State != StateDownloading {
				t.Fatalf("start state = %q, want downloading", result.State)
			}
			waitForState(t, service, "dQw4w9WgXcQ", StateDone)
			tr, err := service.Get("dQw4w9WgXcQ")
			if err != nil {
				t.Fatal(err)
			}
			if tr.StoredCodec != tt.wantStoredCodec {
				t.Fatalf("stored codec = %q, want %q", tr.StoredCodec, tt.wantStoredCodec)
			}
			if mediaTool.ConvertCount() != tt.wantConvertCount {
				t.Fatalf("convert count = %d, want %d", mediaTool.ConvertCount(), tt.wantConvertCount)
			}
			if mediaTool.WriteCount() != tt.wantWriteCount {
				t.Fatalf("write count = %d, want %d", mediaTool.WriteCount(), tt.wantWriteCount)
			}
			if tt.wantConvertCount == 1 {
				call := mediaTool.LastConvertCall()
				if call.bitrate != tt.wantBitrate {
					t.Fatalf("convert bitrate = %d, want %d", call.bitrate, tt.wantBitrate)
				}
			}

			if _, err := service.Update("dQw4w9WgXcQ", "Edited Title", "Edited Artist", "Edited Genre"); err != nil {
				t.Fatal(err)
			}
			if mediaTool.ConvertCount() != tt.wantConvertCount {
				t.Fatalf("convert count after tag edit = %d, want %d", mediaTool.ConvertCount(), tt.wantConvertCount)
			}
			if mediaTool.WriteCount() != tt.wantWriteCount+1 {
				t.Fatalf("write count after tag edit = %d, want %d", mediaTool.WriteCount(), tt.wantWriteCount+1)
			}
		})
	}
}

func TestServiceCancelRemovesPartialAndSetsCancelled(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	block := make(chan struct{})
	downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "Slow Track", codec: "mp3", ext: "mp3", bitrate: 128, block: block},
	}}
	service, err := NewService(library, 1, store, downloader, &fakeMediaTool{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Trigger(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(library, "dQw4w9WgXcQ.mp3")
	waitFor(t, func() bool {
		_, err := os.Stat(partialPath)
		return err == nil
	})
	tr, err := service.Cancel("dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != StateCancelled {
		t.Fatalf("cancel returned state %q, want cancelled", tr.State)
	}
	waitForState(t, service, "dQw4w9WgXcQ", StateCancelled)
	waitFor(t, func() bool {
		_, err := os.Stat(partialPath)
		return os.IsNotExist(err)
	})
	close(block)
	waitIdle(t, service)
}

func TestServiceRestartOnlyFromFailedOrCancelled(t *testing.T) {
	tests := []struct {
		name      string
		build     func(t *testing.T) (*Service, chan struct{})
		wantErr   error
		wantState State
	}{
		{
			name: "failed",
			build: func(t *testing.T) (*Service, chan struct{}) {
				library := t.TempDir()
				store := NewFileStore(library)
				if err := store.Save(Track{VideoID: "dQw4w9WgXcQ", URL: youtube.WatchURL("dQw4w9WgXcQ"), Title: "Restartable", State: StateFailed, TouchedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
				block := make(chan struct{})
				service, err := NewService(library, 1, store, &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
					"dQw4w9WgXcQ": {title: "Restarted", codec: "mp3", ext: "mp3", bitrate: 128, block: block},
				}}, &fakeMediaTool{})
				if err != nil {
					t.Fatal(err)
				}
				return service, block
			},
			wantState: StateDownloading,
		},
		{
			name: "cancelled",
			build: func(t *testing.T) (*Service, chan struct{}) {
				library := t.TempDir()
				store := NewFileStore(library)
				if err := store.Save(Track{VideoID: "dQw4w9WgXcQ", URL: youtube.WatchURL("dQw4w9WgXcQ"), Title: "Restartable", State: StateCancelled, TouchedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
				block := make(chan struct{})
				service, err := NewService(library, 1, store, &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
					"dQw4w9WgXcQ": {title: "Restarted", codec: "mp3", ext: "mp3", bitrate: 128, block: block},
				}}, &fakeMediaTool{})
				if err != nil {
					t.Fatal(err)
				}
				return service, block
			},
			wantState: StateDownloading,
		},
		{
			name: "done",
			build: func(t *testing.T) (*Service, chan struct{}) {
				library := t.TempDir()
				store := NewFileStore(library)
				if err := store.Save(Track{VideoID: "dQw4w9WgXcQ", URL: youtube.WatchURL("dQw4w9WgXcQ"), Title: "Done", State: StateDone, TouchedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
				service, err := NewService(library, 1, store, &fakeDownloader{}, &fakeMediaTool{})
				if err != nil {
					t.Fatal(err)
				}
				return service, nil
			},
			wantErr: ErrInvalidTransition,
		},
		{
			name: "queued",
			build: func(t *testing.T) (*Service, chan struct{}) {
				library := t.TempDir()
				store := NewFileStore(library)
				firstBlock := make(chan struct{})
				service, err := NewService(library, 1, store, &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
					"dQw4w9WgXcQ": {title: "First", codec: "mp3", ext: "mp3", bitrate: 128, block: firstBlock},
					"9bZkp7q19f0": {title: "Queued", codec: "mp3", ext: "mp3", bitrate: 128},
				}}, &fakeMediaTool{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.Trigger(context.Background(), "dQw4w9WgXcQ"); err != nil {
					t.Fatal(err)
				}
				if _, err := service.Trigger(context.Background(), "9bZkp7q19f0"); err != nil {
					t.Fatal(err)
				}
				waitForTrackStateRead(t, service, "9bZkp7q19f0", StateQueued)
				return service, firstBlock
			},
			wantErr: ErrInvalidTransition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, cleanupBlock := tt.build(t)
			result, err := service.Restart(context.Background(), "dQw4w9WgXcQ")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Restart error = %v, want %v", err, tt.wantErr)
				}
				if cleanupBlock != nil {
					close(cleanupBlock)
				}
				waitIdle(t, service)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.State != tt.wantState {
				t.Fatalf("Restart state = %q, want %q", result.State, tt.wantState)
			}
			if _, err := service.Cancel("dQw4w9WgXcQ"); err != nil {
				t.Fatal(err)
			}
			if cleanupBlock != nil {
				close(cleanupBlock)
			}
			waitIdle(t, service)
		})
	}
}

func TestServiceUnknownBitrateDoesNotUpgrade(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	mediaTool := &fakeMediaTool{}
	downloader := &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "Unknown Rate", codec: "vorbis", ext: "webm", bitrate: 0},
	}}
	service, err := NewService(library, 1, store, downloader, mediaTool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Trigger(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	waitForState(t, service, "dQw4w9WgXcQ", StateFailed)
	if mediaTool.ConvertCount() != 0 {
		t.Fatalf("convert count = %d, want 0", mediaTool.ConvertCount())
	}
	tr, err := service.Get("dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(errors.New(tr.Error), errUnknownBitrate) && !strings.Contains(tr.Error, "source bitrate unknown") {
		t.Fatalf("error = %q, want unknown bitrate", tr.Error)
	}
}

func TestServiceCancelDuringTagWriteStaysCancelled(t *testing.T) {
	library := t.TempDir()
	store := NewFileStore(library)
	hold := make(chan struct{})
	mediaTool := &fakeMediaTool{holdWrite: hold}
	service, err := NewService(library, 1, store, &fakeDownloader{behaviors: map[string]fakeDownloadBehavior{
		"dQw4w9WgXcQ": {title: "Slow Tags", codec: "mp3", ext: "mp3", bitrate: 128},
	}}, mediaTool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Trigger(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return mediaTool.WriteCount() == 1 })
	tr, err := service.Cancel("dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != StateCancelled {
		t.Fatalf("cancel returned %q, want cancelled", tr.State)
	}
	close(hold)
	waitForState(t, service, "dQw4w9WgXcQ", StateCancelled)
	time.Sleep(50 * time.Millisecond)
	tr, err = service.Get("dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != StateCancelled {
		t.Fatalf("state after finalize = %q, want cancelled", tr.State)
	}
	if tr.FileName != "" {
		t.Fatalf("file name = %q, want empty", tr.FileName)
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			t.Fatalf("leftover audio file %s", entry.Name())
		}
	}
}

func waitIdle(t *testing.T, service *Service) {
	t.Helper()
	waitFor(t, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return service.active == 0 && len(service.running) == 0
	})
}

func waitForState(t *testing.T, service *Service, videoID string, want State) {
	t.Helper()
	waitFor(t, func() bool {
		tr, err := service.Get(videoID)
		return err == nil && tr.State == want
	})
}

func waitForTrackStateRead(t *testing.T, service *Service, videoID string, want State) {
	t.Helper()
	tr, err := service.Get(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != want {
		t.Fatalf("state = %q, want %q", tr.State, want)
	}
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
