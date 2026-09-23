package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

const (
	DefaultPort          = 6874
	DefaultMaxConcurrent = 2
	DefaultLibrary       = "./download"
	DefaultMetadata      = "./metadata"
	DefaultYTDLPPath     = "/usr/local/bin/yt-dlp"
	DefaultFFmpegPath    = "/usr/bin/ffmpeg"
)

// Config holds runtime settings for the daemon.
type Config struct {
	LibraryPath   string `json:"libraryPath"`
	MetadataPath  string `json:"metadataPath"`
	Port          int    `json:"port"`
	MaxConcurrent int    `json:"maxConcurrent"`
	Bind          string `json:"bind"`
	YTDLPPath     string `json:"ytDlpPath"`
	FFmpegPath    string `json:"ffmpegPath"`
}

func Load(path string) (Config, error) {
	cfg := Config{}
	if path == "" {
		cfg.ApplyDefaults(DefaultBindAddress)
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.ApplyDefaults(DefaultBindAddress)
	return cfg, nil
}

func (c *Config) ApplyDefaults(bindResolver func() string) {
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	if c.LibraryPath == "" {
		c.LibraryPath = DefaultLibrary
	}
	if c.MetadataPath == "" {
		c.MetadataPath = DefaultMetadata
	}
	if c.Bind == "" {
		c.Bind = bindResolver()
	}
	if c.YTDLPPath == "" {
		c.YTDLPPath = DefaultYTDLPPath
	}
	if c.FFmpegPath == "" {
		c.FFmpegPath = DefaultFFmpegPath
	}
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Bind, fmt.Sprintf("%d", c.Port))
}

func (c Config) Validate() error {
	if c.MaxConcurrent <= 0 {
		return errors.New("maxConcurrent must be greater than zero")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if c.LibraryPath == "" {
		return errors.New("libraryPath is required")
	}
	if c.MetadataPath == "" {
		return errors.New("metadataPath is required")
	}
	if err := ValidateLibrary(c.LibraryPath); err != nil {
		return err
	}
	return ValidateLibrary(c.MetadataPath)
}

func ValidateLibrary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("library path does not exist: %s", path)
		}
		return fmt.Errorf("stat library path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("library path is not a directory: %s", path)
	}

	probe := filepath.Join(path, ".mymusidownloader-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("library path is not writable: %w", err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("cleanup library write probe: %w", err)
	}
	return nil
}

func DefaultBindAddress() string {
	if addr := detectTailscaleIPv4(); addr != "" {
		return addr
	}
	return "127.0.0.1"
}

func detectTailscaleIPv4() string {
	iface, err := net.InterfaceByName("tailscale0")
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}
