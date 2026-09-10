package local_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage/local"
)

const testAccount = "acct_1"

type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	body := "hello world"
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain", AccountID: testAccount}
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
	if !strings.HasPrefix(result.URL, "/api/uploads/files/accounts/"+testAccount+"/uploads/") {
		t.Errorf("URL = %q, want prefix /api/uploads/files/accounts/%s/uploads/", result.URL, testAccount)
	}
	if result.ID == "" {
		t.Error("ID is empty")
	}

	// Verify the file is on disk where its URL says.
	stored := strings.TrimPrefix(result.URL, "/api/uploads/files/")
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(stored)))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != body {
		t.Errorf("file contents = %q, want %q", data, body)
	}
}

func TestUpload_WritesUnderAccountFolder(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	params := services.UploadParams{
		Filename:    "My Photo.jpg",
		ContentType: "image/jpeg",
		ID:          "fixed-id-123",
		AccountID:   testAccount,
	}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("jpeg-bytes"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "accounts", testAccount, "uploads", "fixed-id-123-My_Photo.jpg"))
	if err != nil {
		t.Fatalf("the file is not in the account folder: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("file contents = %q, want %q", data, "jpeg-bytes")
	}
	if want := "/api/uploads/files/accounts/" + testAccount + "/uploads/fixed-id-123-My_Photo.jpg"; result.URL != want {
		t.Errorf("URL = %q, want %q", result.URL, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "fixed-id-123-My_Photo.jpg")); !os.IsNotExist(err) {
		t.Errorf("a flat copy was written beside the account folder (stat error %v)", err)
	}
}

func TestUpload_RefusesUnsafeAccountID(t *testing.T) {
	for _, accountID := range []string{"", ".", "..", "../acct_2", "acct_1/../acct_2", `acct\1`, "acct 1"} {
		t.Run(fmt.Sprintf("%q", accountID), func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "uploads")
			svc := local.New(dir, "/api/uploads/files", nopLogger{})

			params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain", AccountID: accountID}
			_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
			if err == nil || !strings.Contains(err.Error(), "invalid account ID") {
				t.Fatalf("Upload() error = %v, want an invalid account ID error", err)
			}

			var written []string
			walkErr := filepath.WalkDir(parent, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					written = append(written, p)
				}
				return nil
			})
			if walkErr != nil {
				t.Fatalf("WalkDir() error: %v", walkErr)
			}
			if len(written) != 0 {
				t.Errorf("files written for a refused upload: %v", written)
			}
		})
	}
}

func TestUpload_UsesCallerSuppliedID(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})

	params := services.UploadParams{
		Filename:    "test.txt",
		ContentType: "text/plain",
		ID:          "fixed-id-123",
		AccountID:   testAccount,
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

	params := services.UploadParams{Filename: "../../etc/passwd", ContentType: "text/plain", AccountID: testAccount}
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
				AccountID:   testAccount,
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

	params := services.UploadParams{Filename: "file.txt", ContentType: "text/plain", AccountID: testAccount}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "accounts", testAccount, "uploads"))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}
