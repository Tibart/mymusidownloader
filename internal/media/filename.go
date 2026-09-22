package media

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var splitPattern = regexp.MustCompile(`[^\pL\pN]+`)

func PascalTitle(title string) string {
	parts := splitPattern.Split(strings.TrimSpace(title), -1)
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		b.WriteRune(unicode.ToUpper(runes[0]))
		if len(runes) > 1 {
			b.WriteString(string(runes[1:]))
		}
	}
	if b.Len() == 0 {
		return "Track"
	}
	return b.String()
}

func BuildFinalName(downloadDate time.Time, title, videoID, ext string, exists func(string) bool) string {
	base := fmt.Sprintf("%s_%s", downloadDate.Format("2006-01-02"), PascalTitle(title))
	candidate := base + "." + strings.TrimPrefix(ext, ".")
	if !exists(candidate) {
		return candidate
	}
	return fmt.Sprintf("%s_%s.%s", base, videoID, strings.TrimPrefix(ext, "."))
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func CandidateExistsInDir(dir string) func(string) bool {
	return func(name string) bool {
		return FileExists(filepath.Join(dir, name))
	}
}
