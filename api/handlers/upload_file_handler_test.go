package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/labstack/echo/v4"
)

const (
	photoOfA = "photo of account A"
	photoOfB = "photo of account B"
)

// stageUploadDir lays out what the local backend writes: one photo in each of
// two account folders, and one flat file from before account folders existed.
func stageUploadDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"accounts/acctA/uploads/idA-lasagna.jpg": photoOfA,
		"accounts/acctB/uploads/idB-lasagna.jpg": photoOfB,
		"idL-old-menu.jpg":                       "flat photo",
		"notes/idN-nested.jpg":                   "nested photo",
	}
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// serveAs requests target from the read route as a caller acting in
// accountID, or as a caller with no identity when accountID is empty.
func serveAs(t *testing.T, dir, accountID, target string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	withAccount := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if accountID != "" {
				ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
					AgentID:         "agent-" + accountID,
					AccountIDs:      []string{accountID},
					ActiveAccountID: accountID,
				})
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	}
	e.GET("/api/uploads/files/*", handlers.ServeUploadedFiles(dir, nopLogger{}), withAccount)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func assertSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Content-Disposition":     "attachment",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'",
		"Cache-Control":           "private, no-store",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func TestServeUploadedFiles_ServesOwnAccountFile(t *testing.T) {
	dir := stageUploadDir(t)
	rec := serveAs(t, dir, "acctB", "/api/uploads/files/accounts/acctB/uploads/idB-lasagna.jpg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != photoOfB {
		t.Errorf("body = %q, want %q", rec.Body.String(), photoOfB)
	}
	assertSecurityHeaders(t, rec)
}

func TestServeUploadedFiles_AnotherAccountsFileIsNotFound(t *testing.T) {
	dir := stageUploadDir(t)
	targets := map[string]string{
		"direct":             "accounts/acctA/uploads/idA-lasagna.jpg",
		"literal dot-dot":    "accounts/acctB/uploads/../../acctA/uploads/idA-lasagna.jpg",
		"encoded dot-dot":    "accounts/acctB/uploads/%2e%2e%2f%2e%2e%2facctA/uploads/idA-lasagna.jpg",
		"encoded slash":      "accounts/acctB/uploads/..%2f..%2facctA/uploads/idA-lasagna.jpg",
		"encoded dots":       "accounts/acctB/uploads/%2e%2e/%2e%2e/acctA/uploads/idA-lasagna.jpg",
		"uppercase encoding": "accounts/acctB/uploads/%2E%2E%2F%2E%2E%2FacctA/uploads/idA-lasagna.jpg",
		"double encoded":     "accounts/acctB/uploads/%252e%252e%252f%252e%252e%252facctA/uploads/idA-lasagna.jpg",
		"doubled slash":      "accounts//acctA/uploads/idA-lasagna.jpg",
		"dot segment":        "accounts/./acctA/uploads/idA-lasagna.jpg",
		"from a flat folder": "notes/../accounts/acctA/uploads/idA-lasagna.jpg",
		// On a case-insensitive filesystem these open account A's folder.
		"uppercase prefix":        "ACCOUNTS/acctA/uploads/idA-lasagna.jpg",
		"mixed case":              "Accounts/acctA/Uploads/idA-lasagna.jpg",
		"query names the account": "accounts/acctA/uploads/idA-lasagna.jpg?account_id=acctA",
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			rec := serveAs(t, dir, "acctB", "/api/uploads/files/"+target)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if strings.Contains(rec.Body.String(), photoOfA) {
				t.Errorf("body carries account A's photo: %q", rec.Body.String())
			}
		})
	}
}

func TestServeUploadedFiles_MissingFileAnsweredLikeAnotherAccountsFile(t *testing.T) {
	dir := stageUploadDir(t)
	other := serveAs(t, dir, "acctB", "/api/uploads/files/accounts/acctA/uploads/idA-lasagna.jpg")
	missing := serveAs(t, dir, "acctB", "/api/uploads/files/accounts/acctB/uploads/idX-lasagna.jpg")

	if other.Code != missing.Code {
		t.Errorf("status differs: another account's file %d, missing file %d", other.Code, missing.Code)
	}
	if other.Body.String() != missing.Body.String() {
		t.Errorf("body differs: another account's file %q, missing file %q", other.Body.String(), missing.Body.String())
	}
}

func TestServeUploadedFiles_CallerWithNoAccountReadsNoAccountFile(t *testing.T) {
	dir := stageUploadDir(t)
	rec := serveAs(t, dir, "", "/api/uploads/files/accounts/acctA/uploads/idA-lasagna.jpg")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestServeUploadedFiles_FlatFileFromBeforeAccountFoldersStillServes(t *testing.T) {
	dir := stageUploadDir(t)
	rec := serveAs(t, dir, "acctB", "/api/uploads/files/idL-old-menu.jpg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "flat photo" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "flat photo")
	}
	assertSecurityHeaders(t, rec)
}

func TestServeUploadedFiles_DirectoriesAndOtherShapesAreNotFound(t *testing.T) {
	dir := stageUploadDir(t)
	targets := []string{
		"/api/uploads/files/",
		"/api/uploads/files/accounts",
		"/api/uploads/files/accounts/",
		"/api/uploads/files/accounts/acctB",
		"/api/uploads/files/accounts/acctB/uploads",
		"/api/uploads/files/accounts/acctB/uploads/",
		"/api/uploads/files/notes",
		"/api/uploads/files/notes/idN-nested.jpg",
		"/api/uploads/files/../../etc/passwd",
		"/api/uploads/files/%2e%2e/%2e%2e/etc/passwd",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			rec := serveAs(t, dir, "acctB", target)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d (location %q)", rec.Code, http.StatusNotFound, rec.Header().Get("Location"))
			}
		})
	}
}
