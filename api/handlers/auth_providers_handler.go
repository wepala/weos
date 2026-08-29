package handlers

import (
	"net/http"
	"sort"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// AuthProvidersHandler reports which OAuth providers this instance can
// actually sign someone in with, so a sign-in screen renders exactly the
// buttons that will work. It reads the same registry /api/auth/login
// resolves its `provider` query param against — deriving the list from the
// registry rather than re-checking config fields is what keeps the two in
// agreement by construction.
type AuthProvidersHandler struct {
	registry authapp.OAuthProviderRegistry
}

// NewAuthProvidersHandler creates the discovery handler over the given
// provider registry.
func NewAuthProvidersHandler(registry authapp.OAuthProviderRegistry) *AuthProvidersHandler {
	return &AuthProvidersHandler{registry: registry}
}

// oauthProviderInfo is the whitelisted public view of one configured
// provider. It is a dedicated response type on purpose: the config and
// provider structs carry client IDs, secrets, and key material, and
// marshaling any of them — or tagging fields on them — is one added field
// away from leaking a credential. Nothing rides along on a type that only
// has a name.
type oauthProviderInfo struct {
	// Name is the registry key, which is also the value /api/auth/login
	// accepts as its `provider` query param (e.g. "google", "apple",
	// "netsuite").
	Name string `json:"name"`
}

// oauthProvidersResponse is an object rather than a bare array so the shape
// can grow fields later without breaking a client that decodes it today. A
// client maps the names it recognizes to buttons and ignores the rest.
type oauthProvidersResponse struct {
	Providers []oauthProviderInfo `json:"providers"`
}

// List answers GET /api/auth/providers. Anonymous by design: the sign-in
// screen calls it before any session exists. An empty registry answers 200
// with an empty list — distinct from the 401 an older build without this
// route returns on an auth-required boot.
func (h *AuthProvidersHandler) List(c echo.Context) error {
	names := make([]string, 0, len(h.registry))
	for name := range h.registry {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]oauthProviderInfo, 0, len(names))
	for _, name := range names {
		providers = append(providers, oauthProviderInfo{Name: name})
	}
	return respond(c, http.StatusOK, oauthProvidersResponse{Providers: providers})
}

// MountAuthProviders registers the discovery route on the anonymous /api
// group, beside /auth/login and /auth/callback — never inside the protected
// group, where RequireAuth would 401 the very caller the route exists for.
// serve.go and the handler tests both mount through here so there is one
// copy of that placement decision rather than two that can drift apart.
func MountAuthProviders(g *echo.Group, h *AuthProvidersHandler) {
	g.GET("/auth/providers", h.List)
}
