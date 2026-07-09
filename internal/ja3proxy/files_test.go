package ja3proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(existing, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if !fileExists(existing) {
		t.Fatalf("fileExists(%q) = false, want true", existing)
	}

	missing := filepath.Join(dir, "missing.txt")
	if fileExists(missing) {
		t.Fatalf("fileExists(%q) = true, want false", missing)
	}
}
