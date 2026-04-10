package storage_test

import (
	"strings"
	"testing"

	"weos/infrastructure/storage"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "photo.jpg", "photo.jpg"},
		{"path traversal", "../../etc/passwd", "passwd"},
		{"special chars", "hello world (1).txt", "hello_world_1_.txt"},
		{"empty", "", "unnamed"},
		{"dot only", ".", "unnamed"},
		{"long name", strings.Repeat("a", 300) + ".txt", strings.Repeat("a", 200)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storage.SanitizeFilename(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerateObjectKey(t *testing.T) {
	key := storage.GenerateObjectKey("uploads", "photo.jpg")
	if !strings.HasPrefix(key, "uploads/") {
		t.Errorf("key = %q, want prefix 'uploads/'", key)
	}
	if !strings.HasSuffix(key, "-photo.jpg") {
		t.Errorf("key = %q, want suffix '-photo.jpg'", key)
	}
	// KSUID is 27 chars
	parts := strings.SplitN(key, "/", 2)
	if len(parts[1]) < 28 {
		t.Errorf("key after prefix too short: %q", parts[1])
	}
}
