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
	"strconv"
	"strings"

	"mymusidownloader/internal/track"
)

type ShellDownloader struct {
	YTDLPPath string
}

type ytDLPInfo struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	ACodec     string  `json:"acodec"`
	Ext        string  `json:"ext"`
	ABR        float64 `json:"abr"`
	TBR        float64 `json:"tbr"`
	Uploader   string  `json:"uploader"`
	Channel    string  `json:"channel"`
	UploadDate string  `json:"upload_date"`
	Duration   float64 `json:"duration"`
	WebpageURL string  `json:"webpage_url"`
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
	channel := info.Channel
	if channel == "" {
		channel = info.Uploader
	}
	sourceURL := info.WebpageURL
	if sourceURL == "" {
		sourceURL = url
	}
	return track.DownloadedMedia{
		Path:          path,
		Title:         info.Title,
		Codec:         info.ACodec,
		Ext:           strings.TrimPrefix(filepath.Ext(path), "."),
		BitrateKbps:   bitrateKbps(info),
		OriginalURL:   url,
		NormalizedURL: sourceURL,
		Channel:       channel,
		UploadDate:    info.UploadDate,
		DurationSec:   int(info.Duration),
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

func (t FFmpegMediaTool) ConvertToAAC(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags track.Tags) error {
	tags.Codec = "aac"
	tags.BitrateKbps = bitrateKbps
	args := append([]string{
		"-y",
		"-i", inputPath,
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-movflags", "+faststart",
	}, metadataArgs(tags)...)
	args = append(args, outputPath)
	return runFFmpeg(ctx, t.FFmpegPath, args...)
}

func (t FFmpegMediaTool) ConvertToOpus(ctx context.Context, inputPath, outputPath string, bitrateKbps int, tags track.Tags) error {
	tags.Codec = "opus"
	tags.BitrateKbps = bitrateKbps
	args := append([]string{
		"-y",
		"-i", inputPath,
		"-c:a", "libopus",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
	}, metadataArgs(tags)...)
	args = append(args, outputPath)
	return runFFmpeg(ctx, t.FFmpegPath, args...)
}

func (t FFmpegMediaTool) Remux(ctx context.Context, inputPath, outputPath string) error {
	return runFFmpeg(ctx, t.FFmpegPath, "-y", "-i", inputPath, "-map", "0:a", "-c", "copy", outputPath)
}

func (t FFmpegMediaTool) WriteTags(ctx context.Context, path string, tags track.Tags) error {
	ext := filepath.Ext(path)
	tmpPath := filepath.Join(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), ext)+".tagtmp"+ext)
	tags = mergeProbe(tags, t.probe(ctx, path))
	args := append([]string{
		"-y",
		"-i", path,
		"-map", "0",
		"-codec", "copy",
	}, metadataArgs(tags)...)
	args = append(args, tmpPath)
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

type audioProbe struct {
	Codec       string
	BitrateKbps int
	DurationSec int
	SampleRate  int
	Channels    int
}

func metadataArgs(tags track.Tags) []string {
	comment := audioComment(tags)
	pairs := [][2]string{
		{"title", tags.Title},
		{"artist", tags.Artist},
		{"album_artist", tags.Artist},
		{"genre", tags.Genre},
		{"album", tags.Album},
		{"date", tags.UploadDate},
		{"codec", tags.Codec},
		{"bitrate", bitrateTag(tags.BitrateKbps)},
		{"length", durationTag(tags.DurationSec)},
		{"sample_rate", numberTag(tags.SampleRate)},
		{"channels", numberTag(tags.Channels)},
		{"source", tags.SourceURL},
		{"comment", comment},
		{"description", comment},
	}
	args := make([]string, 0, len(pairs)*4)
	for _, pair := range pairs {
		value := strings.TrimSpace(pair[1])
		if value == "" {
			continue
		}
		args = append(args, "-metadata", pair[0]+"="+value, "-metadata:s:a", pair[0]+"="+value)
	}
	return args
}

func bitrateTag(kbps int) string {
	if kbps <= 0 {
		return ""
	}
	return strconv.Itoa(kbps) + "kbps"
}

func durationTag(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return strconv.Itoa(seconds) + "s"
}

func numberTag(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func audioComment(tags track.Tags) string {
	parts := make([]string, 0, 7)
	if tags.Codec != "" {
		parts = append(parts, "codec="+tags.Codec)
	}
	if tags.BitrateKbps > 0 {
		parts = append(parts, "bitrate="+strconv.Itoa(tags.BitrateKbps)+"kbps")
	}
	if tags.DurationSec > 0 {
		parts = append(parts, "duration="+strconv.Itoa(tags.DurationSec)+"s")
	}
	if tags.SampleRate > 0 {
		parts = append(parts, "sample_rate="+strconv.Itoa(tags.SampleRate))
	}
	if tags.Channels > 0 {
		parts = append(parts, "channels="+strconv.Itoa(tags.Channels))
	}
	if tags.SourceURL != "" {
		parts = append(parts, "source="+tags.SourceURL)
	}
	return strings.Join(parts, "; ")
}

func mergeProbe(tags track.Tags, probed audioProbe) track.Tags {
	if tags.Codec == "" {
		tags.Codec = probed.Codec
	}
	if tags.BitrateKbps == 0 {
		tags.BitrateKbps = probed.BitrateKbps
	}
	if tags.DurationSec == 0 {
		tags.DurationSec = probed.DurationSec
	}
	if tags.SampleRate == 0 {
		tags.SampleRate = probed.SampleRate
	}
	if tags.Channels == 0 {
		tags.Channels = probed.Channels
	}
	return tags
}

func (t FFmpegMediaTool) probe(ctx context.Context, path string) audioProbe {
	ffprobe := filepath.Join(filepath.Dir(t.FFmpegPath), "ffprobe")
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "format=duration,bit_rate:stream=codec_name,sample_rate,channels",
		path,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return audioProbe{}
	}
	var parsed struct {
		Streams []struct {
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return audioProbe{}
	}
	probed := audioProbe{}
	if len(parsed.Streams) > 0 {
		probed.Codec = parsed.Streams[0].CodecName
		probed.Channels = parsed.Streams[0].Channels
		probed.SampleRate, _ = strconv.Atoi(parsed.Streams[0].SampleRate)
	}
	if bitrate, err := strconv.Atoi(parsed.Format.BitRate); err == nil && bitrate > 0 {
		probed.BitrateKbps = bitrate / 1000
	}
	if duration, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil && duration > 0 {
		probed.DurationSec = int(duration)
	}
	return probed
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
