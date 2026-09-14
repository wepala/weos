package handlers

import (
	"net/http"
	"sort"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// TrustedIssuerProviderName is the name GET /api/auth/providers gives the
// trusted issuer — a fleet's front door — when the instance takes its login
// assertions.
const TrustedIssuerProviderName = "issuer"

// trustedIssuerLoginPath is the door's start page, on the issuer's own host.
// Whether the page is really there is the door's contract; the instance only
// publishes the address.
const trustedIssuerLoginPath = "/door/start"

// AuthProvidersHandler reports which OAuth providers this instance can
// actually sign someone in with, so a sign-in screen renders exactly the
// buttons that will work. It reads the same registry /api/auth/login
// resolves its `provider` query param against — deriving the list from the
// registry rather than re-checking config fields is what keeps the two in
// agreement by construction.
//
// On an instance that takes a trusted issuer's login assertions it also
// offers the issuer, with the address of the door's start page, so a person
// whose session expired is sent back to the door rather than shown nothing.
type AuthProvidersHandler struct {
	registry authapp.OAuthProviderRegistry
	// issuerLoginURL is the door's start page; "" when no issuer is offered.
	issuerLoginURL string
	// issuerProviderKeys are the provider keys an assertion may name, sorted;
	// nil when no issuer is offered.
	issuerProviderKeys []string
}

// AuthProvidersOption adds to what GET /api/auth/providers offers.
type AuthProvidersOption func(*AuthProvidersHandler)

// WithTrustedIssuer offers the trusted issuer as the provider "issuer" with
// the login_url <TRUSTED_ISSUER>/door/start exactly when
// MountTrustedIssuerAssertion mounts POST /api/auth/assert for cfg: all three
// trusted-issuer settings present, an issuer address the sign-in address can
// be built from, a key-list address the verifier may read, and a session
// secret of the instance's own. An instance with no assertion path has no door
// to offer.
//
// The address is built from config.TrustedIssuerConfig.IssuerID, the same
// value, trailing slashes trimmed, that the verifier compares iss with.
//
// The entry also lists, as accepted_provider_keys, the provider keys the
// verifier accepts in an assertion: application.OAuthProviderKeys, sorted. An
// older core refuses a key it does not know as claims, the same reason as a
// missing subject, so an issuer reads this list, not a refusal, to learn
// whether the instance accepts the key it would send.
//
// The entry also carries joins_door_and_google_apple_by_email, always true:
// this core joins a door identity and a google or apple identity with the same
// email into one person. An issuer offers a second sign-in method to an
// instance only when the field is present.
func WithTrustedIssuer(cfg config.Config) AuthProvidersOption {
	return func(h *AuthProvidersHandler) {
		if !trustedIssuerMountable(cfg) {
			return
		}
		h.issuerLoginURL = cfg.TrustedIssuer.IssuerID() + trustedIssuerLoginPath
		keys := application.OAuthProviderKeys()
		sort.Strings(keys)
		h.issuerProviderKeys = keys
	}
}

// NewAuthProvidersHandler creates the discovery handler over the given
// provider registry.
func NewAuthProvidersHandler(registry authapp.OAuthProviderRegistry, opts ...AuthProvidersOption) *AuthProvidersHandler {
	h := &AuthProvidersHandler{registry: registry}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// oauthProviderInfo is the whitelisted public view of one configured
// provider. It is a dedicated response type on purpose: the config and
// provider structs carry client IDs, secrets, and key material, and
// marshaling any of them — or tagging fields on them — is one added field
// away from leaking a credential. Nothing rides along on a type that only
// has a name and, for the trusted issuer alone, a public sign-in address and
// the fixed names of the providers its assertions may name.
type oauthProviderInfo struct {
	// Name is the registry key, which is also the value /api/auth/login
	// accepts as its `provider` query param (e.g. "google", "apple",
	// "netsuite"), or TrustedIssuerProviderName.
	Name string `json:"name"`
	// LoginURL is where a person signs in with this provider when that is not
	// /api/auth/login: set only for the trusted issuer, whose sign-in starts
	// at the door. Omitted for every registry provider.
	LoginURL string `json:"login_url,omitempty"`
	// AcceptedProviderKeys lists, sorted, the provider keys a trusted issuer's
	// assertion may name on this instance (application.OAuthProviderKeys):
	// core's registry keys and "door". Set only for the trusted issuer, so it
	// can tell before it sends a person whether this instance accepts the key
	// it would send. Omitted for every registry provider. The keys are fixed
	// names in core, never configuration.
	AcceptedProviderKeys []string `json:"accepted_provider_keys,omitempty"`
	// JoinsDoorAndGoogleAppleByEmail says that this core joins a door identity
	// and a google or apple identity with the same email into one person, in
	// either order, with or without an allowlist (decision wm-vvi6t). An older
	// core makes a second, empty person instead, and the two stay apart after
	// the instance is upgraded, so an issuer offers a person a second sign-in
	// method to an instance only when this is true. Set only for the trusted
	// issuer, and always true there; omitted for every registry provider and by
	// every older core.
	JoinsDoorAndGoogleAppleByEmail bool `json:"joins_door_and_google_apple_by_email,omitempty"`
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
// route returns on an auth-required boot. Providers are listed by name.
func (h *AuthProvidersHandler) List(c echo.Context) error {
	providers := make([]oauthProviderInfo, 0, len(h.registry)+1)
	for name := range h.registry {
		providers = append(providers, oauthProviderInfo{Name: name})
	}
	if h.issuerLoginURL != "" {
		providers = append(providers, oauthProviderInfo{
			Name:                           TrustedIssuerProviderName,
			LoginURL:                       h.issuerLoginURL,
			AcceptedProviderKeys:           h.issuerProviderKeys,
			JoinsDoorAndGoogleAppleByEmail: true,
		})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
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
