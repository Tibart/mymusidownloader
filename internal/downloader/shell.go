package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mymusidownloader/internal/track"
)

type ShellDownloader struct {
	YTDLPPath string
}

type ytDLPInfo struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	ACodec string  `json:"acodec"`
	Ext    string  `json:"ext"`
	ABR    float64 `json:"abr"`
	TBR    float64 `json:"tbr"`
}

func (d ShellDownloader) Download(ctx context.Context, url, library, videoID string) (track.DownloadedMedia, error) {
	info, err := d.fetchInfo(ctx, url)
	if err != nil {
		return track.DownloadedMedia{}, err
	}
	outputTemplate := filepath.Join(library, videoID+".%(ext)s")
	cmd := exec.CommandContext(ctx, d.YTDLPPath,
		"--no-playlist",
		"-f", "bestaudio/best",
		"-o", outputTemplate,
		url,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return track.DownloadedMedia{}, fmt.Errorf("yt-dlp download: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	path, err := findDownloadedFile(library, videoID)
	if err != nil {
		return track.DownloadedMedia{}, err
	}
	return track.DownloadedMedia{
		Path:          path,
		Title:         info.Title,
		Codec:         info.ACodec,
		Ext:           strings.TrimPrefix(filepath.Ext(path), "."),
		BitrateKbps:   bitrateKbps(info),
		OriginalURL:   url,
		NormalizedURL: url,
	}, nil
}

func (d ShellDownloader) fetchInfo(ctx context.Context, url string) (ytDLPInfo, error) {
	cmd := exec.CommandContext(ctx, d.YTDLPPath,
		"--skip-download",
		"--no-playlist",
		"--dump-single-json",
		url,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ytDLPInfo{}, fmt.Errorf("yt-dlp info: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var info ytDLPInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return ytDLPInfo{}, fmt.Errorf("parse yt-dlp info: %w", err)
	}
	return info, nil
}

type FFmpegMediaTool struct {
	FFmpegPath string
}

func (t FFmpegMediaTool) ConvertToOpus(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags track.Tags) error {
	args := []string{
		"-y",
		"-i", inputPath,
		"-c:a", "libopus",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-metadata", "title=" + tags.Title,
		"-metadata", "artist=" + tags.Artist,
		"-metadata", "genre=" + tags.Genre,
		outputPath,
	}
	return runFFmpeg(ctx, t.FFmpegPath, args...)
}

func (t FFmpegMediaTool) WriteTags(ctx context.Context, path string, tags track.Tags) error {
	ext := filepath.Ext(path)
	tmpPath := filepath.Join(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), ext)+".tagtmp"+ext)
	args := []string{
		"-y",
		"-i", path,
		"-map", "0",
		"-codec", "copy",
		"-metadata", "title=" + tags.Title,
		"-metadata", "artist=" + tags.Artist,
		"-metadata", "genre=" + tags.Genre,
		tmpPath,
	}
	if err := runFFmpeg(ctx, t.FFmpegPath, args...); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace tagged file: %w", err)
	}
	return nil
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args ...string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func bitrateKbps(info ytDLPInfo) int {
	if info.ABR > 0 {
		return int(info.ABR)
	}
	if info.TBR > 0 {
		return int(info.TBR)
	}
	return 0
}

func findDownloadedFile(library, videoID string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(library, videoID+".*"))
	if err != nil {
		return "", err
	}
	filtered := matches[:0]
	for _, match := range matches {
		base := filepath.Base(match)
		if strings.HasSuffix(base, ".part") || strings.HasSuffix(base, ".ytdl") || strings.HasSuffix(base, ".json") {
			continue
		}
		filtered = append(filtered, match)
	}
	matches = filtered
	if len(matches) == 0 {
		return "", fmt.Errorf("downloaded file for %s not found", videoID)
	}
	sort.Strings(matches)
	return matches[0], nil
}
