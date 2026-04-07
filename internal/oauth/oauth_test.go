package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Use a single shared connection for the in-memory database so all
	// queries in a single test see the same schema. ":memory:" with the
	// default GORM pool gives each connection its own DB which would
	// produce missing-table errors under parallel test execution.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&OAuthClient{}, &OAuthAuthorizationCode{}, &OAuthRefreshToken{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type noopLogger struct{}

func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}

func tokenForm(values map[string]string) string {
	v := url.Values{}
	for k, val := range values {
		v.Set(k, val)
	}
	return v.Encode()
}

func newTokenRequest(form string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestValidateRedirectURI_HTTPS(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://example.com/callback"); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidateRedirectURI_LocalhostHTTP(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{
		"http://localhost:3000/callback",
		"http://127.0.0.1:8080/cb",
		"http://[::1]:9999/cb",
	} {
		if err := validateRedirectURI(uri); err != nil {
			t.Errorf("expected valid for %q, got %v", uri, err)
		}
	}
}

func TestValidateRedirectURI_RejectsPlainHTTP(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("http://evil.com/steal"); err == nil {
		t.Error("expected error for non-localhost HTTP")
	}
}

func TestValidateRedirectURI_RejectsJavascript(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("javascript:alert(1)"); err == nil {
		t.Error("expected error for javascript: scheme")
	}
}

func TestValidateRedirectURI_RejectsFragment(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://example.com/cb#fragment"); err == nil {
		t.Error("expected error for fragment")
	}
}

func TestValidateRedirectURI_RejectsNoHost(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https:/path"); err == nil {
		t.Error("expected error for missing host")
	}
}

func TestValidateRedirectURI_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://user:pass@example.com/cb"); err == nil {
		t.Error("expected error for userinfo")
	}
}

func TestIsRedirectURIAllowed(t *testing.T) {
	t.Parallel()
	ok, err := isRedirectURIAllowed(`["https://a.com/cb","https://b.com/cb"]`, "https://a.com/cb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected allowed")
	}
}

func TestIsRedirectURIAllowed_NotInList(t *testing.T) {
	t.Parallel()
	ok, err := isRedirectURIAllowed(`["https://a.com/cb"]`, "https://evil.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected not allowed")
	}
}

func TestIsRedirectURIAllowed_CorruptJSON(t *testing.T) {
	t.Parallel()
	_, err := isRedirectURIAllowed(`not json`, "https://a.com")
	if err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	t.Parallel()
	h1 := HashToken("test-token")
	h2 := HashToken("test-token")
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("hash should not be empty")
	}
}

func TestHashToken_DifferentInput(t *testing.T) {
	t.Parallel()
	if HashToken("a") == HashToken("b") {
		t.Error("different inputs should produce different hashes")
	}
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	t.Parallel()
	t1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if t1 == t2 {
		t.Error("tokens should be unique")
	}
}

func TestPKCEVerification(t *testing.T) {
	t.Parallel()
	verifier, err := authapp.GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	challenge := authapp.GenerateCodeChallenge(verifier)

	// Correct verifier should match.
	if authapp.GenerateCodeChallenge(verifier) != challenge {
		t.Error("same verifier should produce same challenge")
	}

	// Wrong verifier should not match.
	if authapp.GenerateCodeChallenge("wrong-verifier") == challenge {
		t.Error("wrong verifier should not match challenge")
	}
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	if StatusPending == StatusIssued || StatusIssued == StatusExchanged {
		t.Error("status constants must be distinct")
	}
}

func TestValidateScope_Empty(t *testing.T) {
	t.Parallel()
	if err := validateScope(""); err != nil {
		t.Errorf("empty scope should be allowed: %v", err)
	}
}

func TestValidateScope_KnownScopes(t *testing.T) {
	t.Parallel()
	if err := validateScope("mcp:read mcp:write"); err != nil {
		t.Errorf("known scopes should pass: %v", err)
	}
}

func TestValidateScope_UnknownScope(t *testing.T) {
	t.Parallel()
	if err := validateScope("mcp:read evil:scope"); err == nil {
		t.Error("expected error for unknown scope")
	}
}

// --- Repository state transition tests ---

func TestAuthCodeRepo_CreateAndFind(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	ctx := context.Background()

	code := &OAuthAuthorizationCode{
		ClientID:            "client-1",
		RedirectURI:         "https://example.com/cb",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		Status:              StatusPending,
	}
	if err := repo.Create(ctx, code); err != nil {
		t.Fatalf("create: %v", err)
	}
	if code.Code == "" {
		t.Error("Code should be auto-generated")
	}
	if code.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be auto-set")
	}

	found, err := repo.FindByCode(ctx, code.Code)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ClientID != "client-1" {
		t.Errorf("ClientID = %q, want client-1", found.ClientID)
	}
}

func TestAuthCodeRepo_FindByCode_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	_, err := repo.FindByCode(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAuthCodeRepo_StatusTransition_PendingToIssued(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	ctx := context.Background()

	code := &OAuthAuthorizationCode{
		ClientID: "c", RedirectURI: "https://e.com/cb",
		CodeChallenge: "ch", CodeChallengeMethod: "S256", Status: StatusPending,
	}
	_ = repo.Create(ctx, code)

	if err := repo.UpdateIdentity(ctx, code.Code, "agent-1", "acct-1"); err != nil {
		t.Fatalf("update identity: %v", err)
	}

	found, _ := repo.FindByCode(ctx, code.Code)
	if found.Status != StatusIssued {
		t.Errorf("status = %q, want %q", found.Status, StatusIssued)
	}
	if found.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", found.AgentID)
	}
}

func TestAuthCodeRepo_UpdateIdentity_RejectsNonPending(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	ctx := context.Background()

	code := &OAuthAuthorizationCode{
		ClientID: "c", RedirectURI: "https://e.com/cb",
		CodeChallenge: "ch", CodeChallengeMethod: "S256", Status: StatusPending,
	}
	_ = repo.Create(ctx, code)
	_ = repo.UpdateIdentity(ctx, code.Code, "agent-1", "acct-1")

	// Second UpdateIdentity should fail because status is now "issued".
	err := repo.UpdateIdentity(ctx, code.Code, "agent-2", "acct-2")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound on re-update, got %v", err)
	}
}

func TestAuthCodeRepo_MarkExchanged_SingleUse(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	ctx := context.Background()

	code := &OAuthAuthorizationCode{
		ClientID: "c", RedirectURI: "https://e.com/cb",
		CodeChallenge: "ch", CodeChallengeMethod: "S256", Status: StatusPending,
	}
	_ = repo.Create(ctx, code)
	_ = repo.UpdateIdentity(ctx, code.Code, "agent-1", "acct-1")

	// First exchange should succeed.
	if err := repo.MarkExchanged(ctx, code.Code); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	// Second exchange must fail (single-use).
	if err := repo.MarkExchanged(ctx, code.Code); err != ErrNotFound {
		t.Errorf("second exchange should fail, got %v", err)
	}
}

func TestAuthCodeRepo_MarkExchanged_RejectsExpired(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewAuthCodeRepository(db)
	ctx := context.Background()

	code := &OAuthAuthorizationCode{
		ClientID: "c", RedirectURI: "https://e.com/cb",
		CodeChallenge: "ch", CodeChallengeMethod: "S256",
		Status:    StatusPending,
		ExpiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}
	_ = repo.Create(ctx, code)
	_ = repo.UpdateIdentity(ctx, code.Code, "a", "acc")

	if err := repo.MarkExchanged(ctx, code.Code); err != ErrNotFound {
		t.Errorf("expired code should not be exchangeable, got %v", err)
	}
}

func TestRefreshTokenRepo_CreateAndFind(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	raw := "raw-token-value"
	token := &OAuthRefreshToken{
		AgentID: "a", AccountID: "acc", ClientID: "c", Scope: "mcp:read",
	}
	if err := repo.Create(ctx, token, raw); err != nil {
		t.Fatalf("create: %v", err)
	}
	if token.ID == "" || token.TokenHash == "" {
		t.Error("ID and TokenHash should be auto-set")
	}

	found, err := repo.FindByTokenHash(ctx, HashToken(raw))
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.AgentID != "a" {
		t.Errorf("AgentID = %q, want a", found.AgentID)
	}
}

func TestRefreshTokenRepo_Revoke(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	token := &OAuthRefreshToken{AgentID: "a", ClientID: "c"}
	_ = repo.Create(ctx, token, "raw")

	if err := repo.Revoke(ctx, token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	found, _ := repo.FindByTokenHash(ctx, HashToken("raw"))
	if !found.Revoked {
		t.Error("expected token to be revoked")
	}

	// Revoke nonexistent token returns ErrNotFound.
	if err := repo.Revoke(ctx, "nonexistent"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- Token handler tests (validation paths only) ---

func TestTokenHandler_UnsupportedGrantType(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	handler := Token(
		nil, NewAuthCodeRepository(db), NewRefreshTokenRepository(db),
		nil, nil, noopLogger{},
	)

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{"grant_type": "password"}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Errorf("body should contain unsupported_grant_type: %s", rec.Body.String())
	}
}

func TestTokenHandler_AuthCodeMissingFields(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	handler := Token(
		nil, NewAuthCodeRepository(db), NewRefreshTokenRepository(db),
		nil, nil, noopLogger{},
	)

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type": "authorization_code",
		"code":       "x",
		// missing code_verifier, client_id, redirect_uri
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Errorf("body should contain invalid_request: %s", rec.Body.String())
	}
}

func TestTokenHandler_RefreshMissingToken(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	handler := Token(
		nil, NewAuthCodeRepository(db), NewRefreshTokenRepository(db),
		nil, nil, noopLogger{},
	)

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type": "refresh_token",
		// missing refresh_token
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Errorf("body should contain invalid_request: %s", rec.Body.String())
	}
}

func TestTokenHandler_AuthCode_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	handler := Token(
		nil, NewAuthCodeRepository(db), NewRefreshTokenRepository(db),
		nil, nil, noopLogger{},
	)

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "authorization_code",
		"code":          "nonexistent",
		"code_verifier": "v",
		"client_id":     "c",
		"redirect_uri":  "https://x/cb",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("body should contain invalid_grant: %s", rec.Body.String())
	}
}

func TestTokenHandler_AuthCode_PKCEFailure(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	codeRepo := NewAuthCodeRepository(db)
	ctx := context.Background()

	verifier, _ := authapp.GenerateCodeVerifier()
	challenge := authapp.GenerateCodeChallenge(verifier)
	authCode := &OAuthAuthorizationCode{
		ClientID:            "client-1",
		RedirectURI:         "https://example.com/cb",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Status:              StatusPending,
	}
	_ = codeRepo.Create(ctx, authCode)
	_ = codeRepo.UpdateIdentity(ctx, authCode.Code, "agent-1", "acct-1")

	handler := Token(nil, codeRepo, NewRefreshTokenRepository(db), nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "authorization_code",
		"code":          authCode.Code,
		"code_verifier": "wrong-verifier",
		"client_id":     "client-1",
		"redirect_uri":  "https://example.com/cb",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("body should contain invalid_grant: %s", rec.Body.String())
	}
}

func TestTokenHandler_AuthCode_ClientIDMismatch(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	codeRepo := NewAuthCodeRepository(db)
	ctx := context.Background()

	verifier, _ := authapp.GenerateCodeVerifier()
	challenge := authapp.GenerateCodeChallenge(verifier)
	authCode := &OAuthAuthorizationCode{
		ClientID:            "client-1",
		RedirectURI:         "https://example.com/cb",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Status:              StatusPending,
	}
	_ = codeRepo.Create(ctx, authCode)
	_ = codeRepo.UpdateIdentity(ctx, authCode.Code, "agent-1", "acct-1")

	handler := Token(nil, codeRepo, NewRefreshTokenRepository(db), nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "authorization_code",
		"code":          authCode.Code,
		"code_verifier": verifier,
		"client_id":     "different-client",
		"redirect_uri":  "https://example.com/cb",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTokenHandler_AuthCode_RedirectURIMismatch(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	codeRepo := NewAuthCodeRepository(db)
	ctx := context.Background()

	verifier, _ := authapp.GenerateCodeVerifier()
	challenge := authapp.GenerateCodeChallenge(verifier)
	authCode := &OAuthAuthorizationCode{
		ClientID: "client-1", RedirectURI: "https://example.com/cb",
		CodeChallenge: challenge, CodeChallengeMethod: "S256", Status: StatusPending,
	}
	_ = codeRepo.Create(ctx, authCode)
	_ = codeRepo.UpdateIdentity(ctx, authCode.Code, "a", "acc")

	handler := Token(nil, codeRepo, NewRefreshTokenRepository(db), nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "authorization_code",
		"code":          authCode.Code,
		"code_verifier": verifier,
		"client_id":     "client-1",
		"redirect_uri":  "https://different.com/cb",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTokenHandler_RefreshToken_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	handler := Token(
		nil, NewAuthCodeRepository(db), NewRefreshTokenRepository(db),
		nil, nil, noopLogger{},
	)

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "nonexistent",
		"client_id":     "c",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTokenHandler_RefreshToken_Revoked(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	refreshRepo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	tok := &OAuthRefreshToken{AgentID: "a", ClientID: "c"}
	raw := "raw-refresh"
	_ = refreshRepo.Create(ctx, tok, raw)
	_ = refreshRepo.Revoke(ctx, tok.ID)

	handler := Token(nil, NewAuthCodeRepository(db), refreshRepo, nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": raw,
		"client_id":     "c",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant for revoked token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTokenHandler_RefreshToken_Expired(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	refreshRepo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	tok := &OAuthRefreshToken{
		AgentID: "a", ClientID: "c",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // already expired
	}
	raw := "raw-refresh"
	_ = refreshRepo.Create(ctx, tok, raw)

	handler := Token(nil, NewAuthCodeRepository(db), refreshRepo, nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": raw,
		"client_id":     "c",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant for expired token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTokenHandler_RefreshToken_ClientIDMismatch(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	refreshRepo := NewRefreshTokenRepository(db)
	ctx := context.Background()

	tok := &OAuthRefreshToken{AgentID: "a", ClientID: "client-1"}
	raw := "raw-refresh"
	_ = refreshRepo.Create(ctx, tok, raw)

	handler := Token(nil, NewAuthCodeRepository(db), refreshRepo, nil, nil, noopLogger{})

	e := echo.New()
	req := newTokenRequest(tokenForm(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": raw,
		"client_id":     "different-client",
	}))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = handler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expected 400 invalid_grant for client mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
}
