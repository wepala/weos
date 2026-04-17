package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/domain/services"
	"github.com/wepala/weos/infrastructure/storage/local"
)

type nopLogger struct{}

func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	body := "hello world"
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader(body))
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

func TestUpload_UsesCallerSuppliedID(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	params := services.UploadParams{
		Filename:    "test.txt",
		ContentType: "text/plain",
		ID:          "fixed-id-123",
	}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if result.ID != "fixed-id-123" {
		t.Errorf("ID = %q, want %q", result.ID, "fixed-id-123")
	}
	if !strings.Contains(result.URL, "fixed-id-123") {
		t.Errorf("URL = %q, should contain caller-supplied ID", result.URL)
	}
}

func TestUpload_SanitizesFilename(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	params := services.UploadParams{Filename: "../../etc/passwd", ContentType: "text/plain"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("x"))
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

func TestUpload_RejectsPathTraversalID(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	tests := []struct {
		name string
		id   string
	}{
		{"dot-dot-slash", "../../etc/passwd"},
		{"absolute path", "/tmp/evil"},
		{"backslash", `foo\bar`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := services.UploadParams{
				Filename:    "test.txt",
				ContentType: "text/plain",
				ID:          tt.id,
			}
			_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
			if err == nil {
				t.Fatal("expected error for unsafe ID")
			}
			if !strings.Contains(err.Error(), "invalid upload ID") {
				t.Errorf("error = %q, want to contain 'invalid upload ID'", err.Error())
			}
		})
	}
}

func TestUpload_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "uploads")
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	params := services.UploadParams{Filename: "file.txt", ContentType: "text/plain"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
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
