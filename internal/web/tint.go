package web

import (
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"
)

type tintEntry struct {
	modified int64
	css      template.CSS
}

// tintCache keeps one average colour per cover file, keyed by path and mtime.
type tintCache struct {
	mu      sync.Mutex
	entries map[string]tintEntry
}

func newTintCache() *tintCache {
	return &tintCache{entries: make(map[string]tintEntry)}
}

// css returns "--tint: r, g, b" for the cover at path, or "" when it cannot be read.
func (c *tintCache) css(path string) template.CSS {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	modified := info.ModTime().UnixNano()
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[path]; ok && entry.modified == modified {
		return entry.css
	}
	css := averageTint(path)
	c.entries[path] = tintEntry{modified: modified, css: css}
	return css
}

// averageTint samples a coarse grid of the image and averages it.
func averageTint(path string) template.CSS {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return ""
	}
	bounds := img.Bounds()
	const grid = 16
	var r, g, b, n uint64
	for y := 0; y < grid; y++ {
		for x := 0; x < grid; x++ {
			px := bounds.Min.X + (x*2+1)*bounds.Dx()/(grid*2)
			py := bounds.Min.Y + (y*2+1)*bounds.Dy()/(grid*2)
			cr, cg, cb, _ := img.At(px, py).RGBA()
			r += uint64(cr >> 8)
			g += uint64(cg >> 8)
			b += uint64(cb >> 8)
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return template.CSS(fmt.Sprintf("--tint: %d, %d, %d", r/n, g/n, b/n))
}
