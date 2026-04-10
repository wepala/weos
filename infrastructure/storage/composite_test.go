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
	id          string
	gotFilename string
	gotCType    string
	gotBody     []byte
	err         error
}

func (m *capturingFileService) Upload(
	_ context.Context, filename, contentType string, reader io.Reader,
) (*services.UploadResult, error) {
	m.gotFilename = filename
	m.gotCType = contentType
	if reader != nil {
		m.gotBody, _ = io.ReadAll(reader)
	}
	if m.err != nil {
		return nil, m.err
	}
	if m.id == "" {
		m.id = ksuid.New().String()
	}
	return &services.UploadResult{ID: m.id, URL: "https://example/" + m.id}, nil
}

func TestComposite_PrimaryResultReturned(t *testing.T) {
	primary := &capturingFileService{id: "primary-id"}
	secondary := &capturingFileService{id: "secondary-id"}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	result, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if result.ID != "primary-id" {
		t.Errorf("ID = %q, want %q", result.ID, "primary-id")
	}
}

func TestComposite_SecondariesReceiveCorrectData(t *testing.T) {
	primary := &capturingFileService{id: "p"}
	secondary := &capturingFileService{id: "s"}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	body := "file-content-here"
	_, err := svc.Upload(context.Background(), "photo.jpg", "image/jpeg", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	// Verify primary received the correct data.
	if primary.gotFilename != "photo.jpg" {
		t.Errorf("primary filename = %q, want %q", primary.gotFilename, "photo.jpg")
	}
	if primary.gotCType != "image/jpeg" {
		t.Errorf("primary contentType = %q, want %q", primary.gotCType, "image/jpeg")
	}
	if string(primary.gotBody) != body {
		t.Errorf("primary body = %q, want %q", primary.gotBody, body)
	}

	// Verify secondary received identical data.
	if secondary.gotFilename != "photo.jpg" {
		t.Errorf("secondary filename = %q, want %q", secondary.gotFilename, "photo.jpg")
	}
	if secondary.gotCType != "image/jpeg" {
		t.Errorf("secondary contentType = %q, want %q", secondary.gotCType, "image/jpeg")
	}
	if string(secondary.gotBody) != body {
		t.Errorf("secondary body = %q, want %q", secondary.gotBody, body)
	}
}

func TestComposite_PrimaryFailure(t *testing.T) {
	primary := &capturingFileService{err: errors.New("primary failure")}

	svc := storage.NewComposite(primary, nil, nopLogger{})
	_, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from primary failure")
	}
}

func TestComposite_SecondaryFailureNonFatal(t *testing.T) {
	primary := &capturingFileService{id: "ok"}
	failing := &capturingFileService{err: errors.New("secondary down")}

	svc := storage.NewComposite(primary, []services.FileService{failing}, nopLogger{})
	result, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() should not fail on secondary error: %v", err)
	}
	if result.ID != "ok" {
		t.Errorf("ID = %q, want %q", result.ID, "ok")
	}
}

type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestComposite_BufferingFailure(t *testing.T) {
	primary := &capturingFileService{id: "p"}
	svc := storage.NewComposite(primary, nil, nopLogger{})

	_, err := svc.Upload(context.Background(), "test.txt", "text/plain", errReader{errors.New("stream broken")})
	if err == nil {
		t.Fatal("expected error from broken reader")
	}
	if !strings.Contains(err.Error(), "buffer upload data") {
		t.Errorf("error = %q, want to contain 'buffer upload data'", err.Error())
	}
}
