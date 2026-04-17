package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wepala/weos/api/handlers"
	"github.com/wepala/weos/domain/services"

	"github.com/labstack/echo/v4"
)

type nopLogger struct{}

func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

type mockFileService struct {
	result   *services.UploadResult
	err      error
	gotID    string
	gotFname string
	gotCType string
	gotBody  []byte
}

func (m *mockFileService) Upload(
	_ context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	m.gotID = params.ID
	m.gotFname = params.Filename
	m.gotCType = params.ContentType
	if reader != nil {
		m.gotBody, _ = io.ReadAll(reader)
	}
	return m.result, m.err
}

func newMultipartRequest(t *testing.T, fieldName, filename, body string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, body); err != nil {
		t.Fatal(err)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestUploadHandler_Success(t *testing.T) {
	svc := &mockFileService{
		result: &services.UploadResult{
			ID:          "abc123",
			URL:         "/api/uploads/files/abc123-test.txt",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        5,
		},
	}
	handler := handlers.NewUploadHandler(svc, nopLogger{}, 0)

	e := echo.New()
	req := newMultipartRequest(t, "file", "test.txt", "hello")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.Upload(c); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	// Verify response body contains the expected envelope.
	var env struct {
		Data services.UploadResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if env.Data.ID != "abc123" {
		t.Errorf("data.id = %q, want %q", env.Data.ID, "abc123")
	}
	if env.Data.URL != "/api/uploads/files/abc123-test.txt" {
		t.Errorf("data.url = %q, want %q", env.Data.URL, "/api/uploads/files/abc123-test.txt")
	}
	if env.Data.Filename != "test.txt" {
		t.Errorf("data.filename = %q, want %q", env.Data.Filename, "test.txt")
	}

	// Verify service received correct arguments.
	if svc.gotFname != "test.txt" {
		t.Errorf("service got filename = %q, want %q", svc.gotFname, "test.txt")
	}
	if string(svc.gotBody) != "hello" {
		t.Errorf("service got body = %q, want %q", svc.gotBody, "hello")
	}
}

func TestUploadHandler_MissingFile(t *testing.T) {
	handler := handlers.NewUploadHandler(&mockFileService{}, nopLogger{}, 0)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.Upload(c); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if env.Error != "missing or invalid file field" {
		t.Errorf("error = %q, want %q", env.Error, "missing or invalid file field")
	}
}

func TestUploadHandler_ServiceError(t *testing.T) {
	svc := &mockFileService{err: errors.New("storage failed")}
	handler := handlers.NewUploadHandler(svc, nopLogger{}, 0)

	e := echo.New()
	req := newMultipartRequest(t, "file", "test.txt", "data")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.Upload(c); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if env.Error != "file upload failed" {
		t.Errorf("error = %q, want %q (should not contain internal details)", env.Error, "file upload failed")
	}
}

func TestUploadHandler_RequestEntityTooLarge(t *testing.T) {
	const maxBytes int64 = 64

	handler := handlers.NewUploadHandler(&mockFileService{}, nopLogger{}, maxBytes)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < int(maxBytes)+1024; i++ {
		_, _ = part.Write([]byte("x"))
	}
	w.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.Upload(c); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
