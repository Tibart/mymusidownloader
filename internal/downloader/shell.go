package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		"--write-thumbnail",
		"--convert-thumbnails", "jpg",
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
		ArtworkPath:   findThumbnail(library, videoID),
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
	artwork, cleanup := t.labeledArtwork(ctx, tags)
	defer cleanup()
	args := append(append(mediaInputs(inputPath, artwork),
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-movflags", "+faststart",
	), metadataArgs(tags)...)
	args = append(args, artworkCodecArgs(artwork)...)
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

func (t FFmpegMediaTool) PrepareArtwork(ctx context.Context, inputPath, outputPath string) error {
	return runFFmpeg(ctx, t.FFmpegPath,
		"-y", "-i", inputPath,
		"-vf", "crop='min(iw,ih)':'min(iw,ih)',scale='min(1024,iw)':-1:flags=lanczos",
		"-q:v", "3",
		outputPath,
	)
}

func (t FFmpegMediaTool) Remux(ctx context.Context, inputPath, outputPath string) error {
	return runFFmpeg(ctx, t.FFmpegPath, "-y", "-i", inputPath, "-map", "0:a", "-c", "copy", outputPath)
}

func (t FFmpegMediaTool) WriteTags(ctx context.Context, path string, tags track.Tags) error {
	ext := filepath.Ext(path)
	tmpPath := filepath.Join(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), ext)+".tagtmp"+ext)
	tags = mergeProbe(tags, t.probe(ctx, path))
	artwork, cleanup := t.labeledArtwork(ctx, tags)
	defer cleanup()
	if !containerSupportsArtwork(ext) {
		artwork = ""
	}
	args := append(mediaInputs(path, artwork), "-c:a", "copy")
	args = append(args, artworkCodecArgs(artwork)...)
	args = append(args, metadataArgs(tags)...)
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
		{"album_artist", tags.AlbumArtist},
		{"genre", tags.Genre},
		{"album", tags.Album},
		{"date", tags.UploadDate},
		{"codec", tags.Codec},
		{"bitrate", bitrateTag(tags.BitrateKbps)},
		{"length", durationTag(tags.DurationSec)},
		{"sample_rate", numberTag(tags.SampleRate)},
		{"channels", numberTag(tags.Channels)},
		{"source", tags.SourceURL},
		{"copyright", tags.SourceURL},
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

func artworkSource(displayPath string) string {
	if displayPath == "" {
		return ""
	}
	candidate := strings.TrimSuffix(displayPath, ".jpg") + ".source.jpg"
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return displayPath
}

func fontFile() string {
	candidates := []string{
		"/usr/share/fonts/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	out, err := exec.Command("fc-match", "-f", "%{file}", "sans:bold").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func findThumbnail(library, videoID string) string {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		path := filepath.Join(library, videoID+ext)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (t FFmpegMediaTool) labeledArtwork(ctx context.Context, tags track.Tags) (string, func()) {
	nop := func() {}
	label := strings.TrimSpace(tags.Artist)
	if label == "" {
		label = strings.TrimSpace(tags.Album)
	}
	source := artworkSource(tags.ArtworkPath)
	if tags.ArtworkPath == "" || label == "" {
		return tags.ArtworkPath, nop
	}
	if _, err := os.Stat(source); err != nil {
		return tags.ArtworkPath, nop
	}
	tmp, err := os.CreateTemp("", "cover-*.jpg")
	if err != nil {
		return tags.ArtworkPath, nop
	}
	tmp.Close()
	if err := t.LabelArtwork(ctx, source, label, tmp.Name()); err != nil {
		_ = os.Remove(tmp.Name())
		return tags.ArtworkPath, nop
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }
}

func (t FFmpegMediaTool) LabelArtwork(ctx context.Context, inputPath, album, outputPath string) error {
	font := fontFile()
	if font == "" {
		return errors.New("no font for cover text")
	}
	lines := albumLines(album)
	boxY, boxH := "ih*0.78", "ih*0.16"
	textY := []string{"h*0.83"}
	size := "h/15"
	if len(lines) == 2 {
		boxY, boxH = "ih*0.68", "ih*0.26"
		textY = []string{"h*0.73", "h*0.84"}
		size = "h/17"
	}
	vf := "drawbox=y=" + boxY + ":w=iw:h=" + boxH + ":color=black@0.62:t=fill"
	for i, line := range lines {
		vf += ",drawtext=fontfile=" + font +
			":text=" + drawtextLiteral(line) +
			":fontcolor=white:fontsize=" + size +
			":x=(w-text_w)/2:y=" + textY[i] +
			":borderw=3:bordercolor=black@0.85"
	}
	return runFFmpeg(ctx, t.FFmpegPath, "-y", "-i", inputPath, "-vf", vf, "-q:v", "3", outputPath)
}

func albumLines(album string) []string {
	album = strings.TrimSpace(album)
	if len([]rune(album)) <= 16 {
		return []string{album}
	}
	words := strings.Fields(album)
	if len(words) < 2 {
		runes := []rune(album)
		mid := len(runes) / 2
		return []string{strings.TrimSpace(string(runes[:mid])), strings.TrimSpace(string(runes[mid:]))}
	}
	best := 1
	bestDiff := len(album)
	for i := 1; i < len(words); i++ {
		left := len([]rune(strings.Join(words[:i], " ")))
		right := len([]rune(strings.Join(words[i:], " ")))
		diff := left - right
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			best = i
		}
	}
	return []string{strings.Join(words[:best], " "), strings.Join(words[best:], " ")}
}

func drawtextLiteral(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `:`, `\:`, `'`, `\'`, `%`, `\%`)
	return "'" + replacer.Replace(text) + "'"
}

func mediaInputs(audioPath, artworkPath string) []string {
	args := []string{"-y", "-i", audioPath}
	maps := []string{"-map", "0:a"}
	if artworkPath != "" {
		if _, err := os.Stat(artworkPath); err == nil {
			args = append(args, "-i", artworkPath)
			maps = append(maps, "-map", "1:v")
		}
	}
	return append(args, maps...)
}

func containerSupportsArtwork(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "m4a", "mp4", "mp3":
		return true
	default:
		return false
	}
}

func artworkCodecArgs(artworkPath string) []string {
	if artworkPath == "" {
		return nil
	}
	if _, err := os.Stat(artworkPath); err != nil {
		return nil
	}
	return []string{"-c:v", "mjpeg", "-disposition:v:0", "attached_pic", "-metadata:s:v", "comment=Cover (front)"}
}

func findDownloadedFile(library, videoID string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(library, videoID+".*"))
	if err != nil {
		return "", err
	}
	filtered := matches[:0]
	for _, match := range matches {
		base := filepath.Base(match)
		ext := strings.ToLower(filepath.Ext(base))
		if strings.HasSuffix(base, ".part") || strings.HasSuffix(base, ".ytdl") || strings.HasSuffix(base, ".json") || ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
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
