package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"weos/domain/services"
	"weos/infrastructure/storage"

	"github.com/segmentio/ksuid"
)

type nopLogger struct{}

func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

type capturingFileService struct {
	url      string
	gotID    string
	gotFname string
	gotCType string
	gotBody  []byte
	err      error
}

func (m *capturingFileService) Upload(
	_ context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	m.gotID = params.ID
	m.gotFname = params.Filename
	m.gotCType = params.ContentType
	if reader != nil {
		m.gotBody, _ = io.ReadAll(reader)
	}
	if m.err != nil {
		return nil, m.err
	}
	id := params.ID
	if id == "" {
		id = ksuid.New().String()
	}
	url := m.url
	if url == "" {
		url = "https://example/" + id
	}
	return &services.UploadResult{ID: id, URL: url}, nil
}

func TestComposite_PrimaryResultReturned(t *testing.T) {
	primary := &capturingFileService{url: "https://cloud/obj"}
	secondary := &capturingFileService{url: "/api/uploads/files/local"}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	// Composite should prefer the secondary (local) URL.
	if result.URL != "/api/uploads/files/local" {
		t.Errorf("URL = %q, want %q", result.URL, "/api/uploads/files/local")
	}
}

func TestComposite_SharedIDacrossBackends(t *testing.T) {
	primary := &capturingFileService{}
	secondary := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	// Both backends must receive the same pre-generated ID.
	if primary.gotID == "" {
		t.Fatal("primary received empty ID")
	}
	if primary.gotID != secondary.gotID {
		t.Errorf("IDs differ: primary=%q secondary=%q", primary.gotID, secondary.gotID)
	}
}

func TestComposite_SecondariesReceiveCorrectData(t *testing.T) {
	primary := &capturingFileService{}
	secondary := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	body := "file-content-here"
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader(body))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	if primary.gotFname != "photo.jpg" {
		t.Errorf("primary filename = %q, want %q", primary.gotFname, "photo.jpg")
	}
	if string(primary.gotBody) != body {
		t.Errorf("primary body = %q, want %q", primary.gotBody, body)
	}
	if secondary.gotFname != "photo.jpg" {
		t.Errorf("secondary filename = %q, want %q", secondary.gotFname, "photo.jpg")
	}
	if string(secondary.gotBody) != body {
		t.Errorf("secondary body = %q, want %q", secondary.gotBody, body)
	}
}

func TestComposite_PrimaryFailure(t *testing.T) {
	primary := &capturingFileService{err: errors.New("primary failure")}

	svc := storage.NewComposite(primary, nil, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from primary failure")
	}
}

func TestComposite_SecondaryFailureNonFatal(t *testing.T) {
	primary := &capturingFileService{url: "https://cloud/ok"}
	failing := &capturingFileService{err: errors.New("secondary down")}

	svc := storage.NewComposite(primary, []services.FileService{failing}, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() should not fail on secondary error: %v", err)
	}
	// With no successful secondary, falls back to primary result.
	if result.URL != "https://cloud/ok" {
		t.Errorf("URL = %q, want %q", result.URL, "https://cloud/ok")
	}
}

type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestComposite_BufferingFailure(t *testing.T) {
	primary := &capturingFileService{}
	svc := storage.NewComposite(primary, nil, nopLogger{})

	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	_, err := svc.Upload(context.Background(), params, errReader{errors.New("stream broken")})
	if err == nil {
		t.Fatal("expected error from broken reader")
	}
	if !strings.Contains(err.Error(), "spool upload data") {
		t.Errorf("error = %q, want to contain 'spool upload data'", err.Error())
	}
}
