package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"weos/infrastructure/storage/local"
)

type nopLogger struct{}

func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	body := "hello world"
	result, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	if result.Filename != "test.txt" {
		t.Errorf("Filename = %q, want %q", result.Filename, "test.txt")
	}
	if result.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", result.ContentType, "text/plain")
	}
	if result.Size != int64(len(body)) {
		t.Errorf("Size = %d, want %d", result.Size, len(body))
	}
	if !strings.HasPrefix(result.URL, "/api/uploads/files/") {
		t.Errorf("URL = %q, want prefix /api/uploads/files/", result.URL)
	}
	if result.ID == "" {
		t.Error("ID is empty")
	}

	// Verify file on disk
	diskName := filepath.Base(result.URL)
	data, err := os.ReadFile(filepath.Join(dir, diskName))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != body {
		t.Errorf("file contents = %q, want %q", data, body)
	}
}

func TestUpload_SanitizesFilename(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	result, err := svc.Upload(context.Background(), "../../etc/passwd", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	if strings.Contains(result.Filename, "..") {
		t.Errorf("Filename contains path traversal: %q", result.Filename)
	}
	if strings.Contains(result.Filename, "/") {
		t.Errorf("Filename contains slash: %q", result.Filename)
	}
}

func TestUpload_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "uploads")
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	_, err := svc.Upload(context.Background(), "file.txt", "text/plain", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}
