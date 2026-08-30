package cli

import (
	"net/http"
	"net/url"
	"strings"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// AuthLoginRefusalCode is what /api/auth/login puts on the sign-in screen's
// address when it cannot begin the sign-in that was asked for. It is a fixed
// code, never the value that was asked for: the address is reachable by
// anyone with a link, so anything echoed back off it is reflected content on
// a page this instance serves.
//
// Exported so a frontend's own tests can name the same string the server
// sends, rather than a copy of it.
const AuthLoginRefusalCode = "provider_unavailable"

// authLoginRefusalParam is the query key that carries the code above.
const authLoginRefusalParam = "auth_error"

// authLoginConfig is what the wrapper below needs to decide whether a sign-in
// can begin at all.
type authLoginConfig struct {
	// Registry is the same provider registry /api/auth/providers reports and
	// pericarp's Login resolves its `provider` query param against. Deriving
	// the refusal from the registry rather than re-reading config fields is
	// what keeps the three in agreement by construction.
	Registry authapp.OAuthProviderRegistry
	// DefaultProvider is what an absent `provider` resolves to — normally
	// config.DefaultOAuthProvider(), which honors OAUTH_DEFAULT_PROVIDER and
	// otherwise auto-picks the first provider this instance has. It is NOT
	// trusted: whatever it resolves to still has to be in the Registry, so an
	// auto-pick on an instance with nothing configured is refused like any
	// other unavailable provider. See the comment on authLoginHandler.
	DefaultProvider string
	// FrontendURL is prepended to the refusal's destination when the SPA is
	// served from another origin, mirroring what pericarp's Callback does with
	// the same value on the success path.
	FrontendURL string
}

// authLoginHandler wraps pericarp's OAuth login so that a sign-in this
// instance cannot begin returns the browser to the sign-in screen with a
// reason on the address, instead of replacing the page with a raw JSON body.
//
// The bare pericarp handler asks the auth service to initiate a flow for
// whatever provider it was handed, and an unregistered provider surfaces
// there as an error it answers with 500 and
// {"error":"failed to initiate auth flow"}. That is a reasonable answer to an
// API client and the wrong one entirely for the caller this route actually
// has: /api/auth/login is only ever reached by a top-level browser
// navigation, so the JSON body IS the page the person is left looking at —
// from a bookmark, a browser history entry, or a link they were sent, none of
// which the SPA gets a chance to intercept.
//
// A provider is refused when the registry does not hold it. That covers three
// arrivals with one rule: a provider this instance was never configured for, a
// provider it was configured for and no longer is, and a value that names no
// provider at all.
//
// An absent `provider` keeps resolving the way it always has —
// config.DefaultOAuthProvider(), which honors OAUTH_DEFAULT_PROVIDER and
// otherwise auto-picks the first provider this instance has. That resolution
// is deliberately unchanged: /api/auth/login with no provider is a shipped
// calling convention, used by core's own admin sign-in and invite pages
// (web/admin/pages/login.vue, invite.vue), by weos-finance's admin, and by
// crossdeck-crm. Refusing it outright would have broken every one of them on
// any deployment that had never needed to set OAUTH_DEFAULT_PROVIDER.
//
// What changes is that the resolved provider is no longer trusted. It must be
// in the registry, so the auto-pick's own final fallback to "google" on an
// instance with nothing configured is refused like any other provider this
// instance cannot begin — which is the case that produced the raw JSON page.
// An operator naming a provider explicitly is honored in full — including being
// refused itself when it names a
// provider the registry does not hold, which is a misconfiguration worth
// seeing rather than falling back over.
func authLoginHandler(inner http.HandlerFunc, cfg authLoginConfig) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		query := r.URL.Query()

		provider := strings.TrimSpace(query.Get("provider"))
		if provider == "" {
			provider = strings.TrimSpace(cfg.DefaultProvider)
		}
		if _, ok := cfg.Registry[provider]; provider == "" || !ok {
			return c.Redirect(http.StatusFound,
				authLoginRefusalLocation(cfg.FrontendURL, query.Get("redirect")))
		}

		inner(c.Response(), r)
		return nil
	}
}

// authLoginRefusalLocation builds where a refused sign-in sends the browser:
// the sign-in screen, carrying the refusal code.
//
// The `redirect` the caller handed in is honored under exactly the rule the
// SUCCESS path applies to it (pericarp's isValidRedirectPath: a path, not a
// protocol-relative one, and no scheme), so a refusal can never reach an
// address a completed sign-in could not. That matters more here than on the
// success path: this address is the one a hostile link points at, so a
// refusal that honored more than the success path would be an open redirect
// wearing an error message. Anything else falls back to "/", which is the
// sign-in screen.
func authLoginRefusalLocation(frontendURL, redirect string) string {
	destination := "/"
	if isRefusalRedirectPath(redirect) {
		destination = redirect
	}
	if frontendURL != "" {
		destination = strings.TrimRight(frontendURL, "/") + destination
	}

	parsed, err := url.Parse(destination)
	if err != nil {
		// Unparseable only if the configured FrontendURL is malformed, since
		// the path half was validated above. The sign-in screen at the root
		// of this origin is always somewhere to stand.
		return "/?" + authLoginRefusalParam + "=" + AuthLoginRefusalCode
	}
	query := parsed.Query()
	query.Set(authLoginRefusalParam, AuthLoginRefusalCode)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// isRefusalRedirectPath mirrors pericarp's isValidRedirectPath — a same-origin
// path and nothing else — with one addition: a leading "/\" is rejected too,
// because several browsers normalize a backslash to a forward slash and would
// read "/\evil.example" as the protocol-relative "//evil.example" that the
// second clause exists to keep out.
func isRefusalRedirectPath(path string) bool {
	return strings.HasPrefix(path, "/") &&
		!strings.HasPrefix(path, "//") &&
		!strings.HasPrefix(path, `/\`) &&
		!strings.Contains(path, "://")
}
