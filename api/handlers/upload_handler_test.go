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

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/domain/services"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/labstack/echo/v4"
)

type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

type mockFileService struct {
	result     *services.UploadResult
	err        error
	called     bool
	gotID      string
	gotAccount string
	gotFname   string
	gotCType   string
	gotBody    []byte
}

func (m *mockFileService) Upload(
	_ context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	m.called = true
	m.gotID = params.ID
	m.gotAccount = params.AccountID
	m.gotFname = params.Filename
	m.gotCType = params.ContentType
	if reader != nil {
		m.gotBody, _ = io.ReadAll(reader)
	}
	return m.result, m.err
}

func (m *mockFileService) DeleteAccountFolder(context.Context, string) error { return nil }

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

// asAccount attaches the identity the session or bearer middleware resolves
// for a signed-in caller acting in accountID.
func asAccount(req *http.Request, accountID string) *http.Request {
	return req.WithContext(auth.ContextWithAgent(req.Context(), &auth.Identity{
		AgentID:         "agent-1",
		AccountIDs:      []string{accountID},
		ActiveAccountID: accountID,
	}))
}

func TestUploadHandler_Success(t *testing.T) {
	svc := &mockFileService{
		result: &services.UploadResult{
			ID:          "abc123",
			URL:         "/api/uploads/files/accounts/acct-1/uploads/abc123-test.txt",
			Filename:    "test.txt",
			ContentType: "text/plain",
			Size:        5,
		},
	}
	handler := handlers.NewUploadHandler(svc, nopLogger{}, 0)

	e := echo.New()
	req := asAccount(newMultipartRequest(t, "file", "test.txt", "hello"), "acct-1")
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
	if env.Data.URL != "/api/uploads/files/accounts/acct-1/uploads/abc123-test.txt" {
		t.Errorf("data.url = %q, want %q", env.Data.URL, "/api/uploads/files/accounts/acct-1/uploads/abc123-test.txt")
	}
	if env.Data.Filename != "test.txt" {
		t.Errorf("data.filename = %q, want %q", env.Data.Filename, "test.txt")
	}

	// Verify service received correct arguments.
	if svc.gotFname != "test.txt" {
		t.Errorf("service got filename = %q, want %q", svc.gotFname, "test.txt")
	}
	if svc.gotAccount != "acct-1" {
		t.Errorf("service got account = %q, want %q", svc.gotAccount, "acct-1")
	}
	if string(svc.gotBody) != "hello" {
		t.Errorf("service got body = %q, want %q", svc.gotBody, "hello")
	}
}

func TestUploadHandler_AccountComesFromSignedInIdentity(t *testing.T) {
	svc := &mockFileService{result: &services.UploadResult{ID: "abc123"}}
	handler := handlers.NewUploadHandler(svc, nopLogger{}, 0)

	// Every place a caller could name an account names a different one than
	// the account the caller is signed in to.
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	for _, field := range []string{"account_id", "accountId", "account"} {
		if err := form.WriteField(field, "acct-claimed"); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "data"); err != nil {
		t.Fatal(err)
	}
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads?account_id=acct-claimed&accountId=acct-claimed", &buf)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Account-ID", "acct-claimed")
	req.Header.Set("X-Active-Account", "acct-claimed")
	req = asAccount(req, "acct-signed-in")

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := handler.Upload(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if svc.gotAccount != "acct-signed-in" {
		t.Errorf("service got account = %q, want the signed-in account %q", svc.gotAccount, "acct-signed-in")
	}
}

func TestUploadHandler_RefusesUploadWithNoAccount(t *testing.T) {
	tests := []struct {
		name       string
		identity   *auth.Identity
		wantStatus int
		wantError  string
	}{
		{"no identity", nil, http.StatusUnauthorized, "authentication required"},
		{"identity with no active account", &auth.Identity{AgentID: "agent-1"}, http.StatusForbidden, "no active account"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockFileService{result: &services.UploadResult{ID: "abc123"}}
			handler := handlers.NewUploadHandler(svc, nopLogger{}, 0)

			req := newMultipartRequest(t, "file", "photo.jpg", "data")
			req.URL.RawQuery = "account_id=acct-claimed"
			req.Header.Set("X-Account-ID", "acct-claimed")
			if tt.identity != nil {
				req = req.WithContext(auth.ContextWithAgent(req.Context(), tt.identity))
			}

			e := echo.New()
			rec := httptest.NewRecorder()
			if err := handler.Upload(e.NewContext(req, rec)); err != nil {
				t.Fatalf("Upload() error: %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var env map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
			}
			if len(env) != 1 || env["error"] != tt.wantError {
				t.Errorf("body = %s, want the error envelope {\"error\":%q}", rec.Body.String(), tt.wantError)
			}
			if svc.called {
				t.Error("the file service was called for an upload with no account")
			}
		})
	}
}

func TestUploadHandler_MissingFile(t *testing.T) {
	handler := handlers.NewUploadHandler(&mockFileService{}, nopLogger{}, 0)

	e := echo.New()
	req := asAccount(httptest.NewRequest(http.MethodPost, "/api/uploads", nil), "acct-1")
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
	req := asAccount(newMultipartRequest(t, "file", "test.txt", "data"), "acct-1")
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
	req := asAccount(httptest.NewRequest(http.MethodPost, "/api/uploads", &body), "acct-1")
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
