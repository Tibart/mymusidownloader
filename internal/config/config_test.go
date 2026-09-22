package config

import (
	"path/filepath"
	"testing"
)

func TestValidateLibraryMissingFailsClosed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := ValidateLibrary(missing); err == nil {
		t.Fatalf("ValidateLibrary(%q) error = nil, want error", missing)
	}
}
