package web

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeSolidPNG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}

func TestTintFromSolidCover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cover.png")
	writeSolidPNG(t, path, color.RGBA{R: 200, G: 40, B: 10, A: 255})
	if got := newTintCache().css(path); got != "--tint: 200, 40, 10" {
		t.Fatalf("tint = %q", got)
	}
}

func TestTintMissingOrBadFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	cache := newTintCache()
	if got := cache.css(filepath.Join(dir, "missing.jpg")); got != "" {
		t.Fatalf("missing file tint = %q", got)
	}
	bad := filepath.Join(dir, "bad.jpg")
	if err := os.WriteFile(bad, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cache.css(bad); got != "" {
		t.Fatalf("bad file tint = %q", got)
	}
}
