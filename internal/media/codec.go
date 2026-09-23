package media

import "strings"

func EquivalentAACBitrate(opusKbps int) int {
	switch {
	case opusKbps <= 0:
		return 0
	case opusKbps <= 96:
		return 128
	case opusKbps <= 128:
		return 160
	case opusKbps <= 160:
		return 192
	default:
		return 224
	}
}

func KeepSourceCodec(codec string) bool {
	switch strings.ToLower(codec) {
	case "mp3", "aac", "flac", "alac", "opus", "mp4a.40.2", "mp4a":
		return true
	default:
		return false
	}
}

func ExtensionForCodec(codec, ext string) string {
	codec = strings.ToLower(codec)
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext != "" {
		switch codec {
		case "mp3", "aac", "flac", "alac", "opus", "mp4a.40.2", "mp4a":
			return canonicalExt(codec, ext)
		}
	}
	return canonicalExt(codec, ext)
}

func canonicalExt(codec, ext string) string {
	switch codec {
	case "mp3":
		return "mp3"
	case "aac", "mp4a.40.2", "mp4a":
		if ext == "m4a" || ext == "aac" {
			return ext
		}
		return "m4a"
	case "flac":
		return "flac"
	case "alac":
		if ext == "m4a" || ext == "caf" {
			return ext
		}
		return "m4a"
	case "opus":
		return "opus"
	default:
		if ext != "" {
			return ext
		}
		return "bin"
	}
}
