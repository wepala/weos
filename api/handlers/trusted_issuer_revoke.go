// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	"github.com/labstack/echo/v4"
)

// TokenRevocationService ends the token access of the person an accepted
// assertion names. *application.AssertedTokenRevocation is the implementation.
type TokenRevocationService interface {
	Revoke(ctx context.Context, id application.AssertedIdentity) (application.TokenRevocationResult, error)
}

// RevokeTokensBodyLimit is the largest request body POST /auth/revoke-tokens
// reads, 16 KiB — the assertion route's limit, for the same reason: the route
// is public, and the assertion is about a kilobyte.
const RevokeTokensBodyLimit = 16 << 10

// revocationRetryAfter is the Retry-After, in seconds, on a 503 for a store
// that could not be written.
const revocationRetryAfter = "5"

// TokenRevocationHandlerConfig wires the token-revocation endpoint.
type TokenRevocationHandlerConfig struct {
	Verifier AssertionVerifier
	Revoke   TokenRevocationService
	Logger   entities.Logger
	// AllowedOrigins are the addresses whose origins a browser may post a
	// revocation from, as for the assertion route. The door calls this one
	// server-side and carries no Origin at all, which passes.
	AllowedOrigins []string
}

// TokenRevocationHandler answers POST /auth/revoke-tokens: the fleet's front
// door, having reset a person's password, tells the instance to stop renewing
// that person's tokens.
//
// The door — not the person — makes the call, because the person may not be
// present: the whole reason to reset a password is that somebody else may hold
// the device. So the caller is authorized exactly as a sign-in is, by a
// short-lived assertion the trusted issuer signed for this one instance:
// ES256 against the issuer's published key list, the instance's own audience,
// a single-use jti, a 60-second life, this instance's identity allowlist. It
// carries one claim more than a login assertion — purpose=revoke-tokens
// (trustedissuer.PurposeRevokeTokens) — so an assertion captured on its way to
// the sign-in route cannot end a person's token access, and one captured on
// its way here cannot sign anyone in.
//
// Reusing the assertion is what makes this safe to mount at all. A shared
// secret would be a second credential per instance to distribute, store and
// rotate, and rotating the door's signing key already covers the fleet through
// the key list.
type TokenRevocationHandler struct {
	cfg     TokenRevocationHandlerConfig
	origins map[string]struct{}
}

// NewTokenRevocationHandler builds the handler.
func NewTokenRevocationHandler(cfg TokenRevocationHandlerConfig) *TokenRevocationHandler {
	return &TokenRevocationHandler{cfg: cfg, origins: originSet(cfg.AllowedOrigins)}
}

// TrustedIssuerRevocationDeps is what the revocation route takes from the
// running application; its settings come from config.TrustedIssuerConfig.
type TrustedIssuerRevocationDeps struct {
	Revoke TokenRevocationService
	Logger entities.Logger
	// PublicBaseURL is the instance's own public address, as for the assertion
	// route. Optional.
	PublicBaseURL string
	// Now is the verifier's clock. Optional; time.Now.
	Now func() time.Time
}

// NewTrustedIssuerRevocationHandler builds the handler POST
// /auth/revoke-tokens serves: a verifier for the same issuer, key list,
// audience, provider keys and allowlist as the sign-in, asking for
// PurposeRevokeTokens instead of a login.
//
// It is a verifier of its own, not the sign-in's: the two must not share a jti
// memory that lets one route spend the other's assertion, and the purpose is
// what tells them apart.
//
// The allowlist is a parameter rather than a field of deps for the reason it
// is one there: a forgotten allowlist would let the door end the token access
// of an address the instance never admitted.
func NewTrustedIssuerRevocationHandler(
	settings config.TrustedIssuerConfig,
	allowedEmails []string,
	deps TrustedIssuerRevocationDeps,
) *TokenRevocationHandler {
	return NewTokenRevocationHandler(TokenRevocationHandlerConfig{
		Verifier: trustedissuer.NewVerifier(trustedissuer.Config{
			Issuer:        settings.IssuerID(),
			JWKSURL:       settings.JWKSURL,
			Audience:      settings.Audience,
			Purpose:       trustedissuer.PurposeRevokeTokens,
			Providers:     application.OAuthProviderKeys(),
			AllowedEmails: allowedEmails,
			Now:           deps.Now,
			Logger:        deps.Logger,
		}),
		Revoke:         deps.Revoke,
		Logger:         deps.Logger,
		AllowedOrigins: []string{settings.IssuerID(), deps.PublicBaseURL},
	})
}

// Verifier is the verifier the handler checks assertions with.
func (h *TokenRevocationHandler) Verifier() AssertionVerifier { return h.cfg.Verifier }

type revokeTokensRequest struct {
	Assertion string `json:"assertion"`
}

// RevokeTokens ends the access of the person the assertion names: every
// refresh token of theirs — each connector's and each app session's, in every
// family and account — every browser session of theirs, and every
// authorization code of theirs nobody has redeemed. So nothing issued before
// the password changed renews again, and nothing left alive issues more.
//
// An accepted assertion is answered **204, always**: whether the instance
// knows the person, whether they had any token, and whether this call or an
// earlier one revoked it. The door learns only that the person now has no
// refresh token here. Anything else would make the route an oracle for whether
// an address has an account on this instance.
//
// Every assertion it does not accept is answered 401 with the refusal's reason
// as the answer's code, exactly as the sign-in answers one, and the assertion
// itself is never logged. A request from another site is refused first, 403
// cross-site, before the body is read. A body over RevokeTokensBodyLimit is
// answered 413. A store that could not be written answers 503 with
// Retry-After: the tokens may still renew, so the door must ask again — never
// a 204, which would report an eviction that did not happen.
func (h *TokenRevocationHandler) RevokeTokens(c echo.Context) error {
	ctx := c.Request().Context()
	httpReq := c.Request()
	if why := crossSiteRequest(httpReq, h.origins); why != "" {
		h.cfg.Logger.Warn(ctx, "trusted issuer token revocation refused before it was read: the request came from another site",
			"reason", CodeCrossSite, "detail", why)
		return respondErrorCode(c, http.StatusForbidden, "token revocation refused: "+CodeCrossSite, CodeCrossSite)
	}
	if httpReq.ContentLength > RevokeTokensBodyLimit {
		return h.tooLarge(c)
	}
	httpReq.Body = http.MaxBytesReader(c.Response(), httpReq.Body, RevokeTokensBodyLimit)
	var req revokeTokensRequest
	if err := c.Bind(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return h.tooLarge(c)
		}
		// A body that does not bind carries no assertion, and is refused like
		// one that cannot be trusted rather than answered as a malformed
		// request — the assertion route's rule.
		req.Assertion = ""
	}

	identity, err := h.cfg.Verifier.Verify(ctx, req.Assertion)
	if err != nil {
		return h.refuse(c, err)
	}

	if _, err := h.cfg.Revoke.Revoke(ctx, application.AssertedIdentity{
		Provider: identity.Provider,
		Subject:  identity.Subject,
		Email:    identity.Email,
	}); err != nil {
		// The service has logged which person and what was left, which is what
		// an operator acts on; this answer is what the door acts on.
		c.Response().Header().Set("Retry-After", revocationRetryAfter)
		return respondError(c, http.StatusServiceUnavailable,
			"the person's token access could not be ended; ask again")
	}
	return c.NoContent(http.StatusNoContent)
}

// tooLarge answers a body over RevokeTokensBodyLimit. The log line carries the
// limit and nothing of the body.
func (h *TokenRevocationHandler) tooLarge(c echo.Context) error {
	h.cfg.Logger.Warn(c.Request().Context(), "trusted issuer token revocation request body is too large; it was not read",
		"limit_bytes", RevokeTokensBodyLimit)
	return respondError(c, http.StatusRequestEntityTooLarge, "the request body is larger than an assertion can be")
}

// refuse answers an assertion the verifier did not accept: 401 with the
// refusal's reason, and one warning naming it. The assertion never reaches
// either.
func (h *TokenRevocationHandler) refuse(c echo.Context, err error) error {
	ctx := c.Request().Context()
	reason, detail := refusalOf(err)
	if ended := ctx.Err(); ended != nil && errors.Is(err, ended) {
		// The request ended while it waited for the issuer's key list: the
		// caller went away, and the key list did not fail. The assertion
		// route's rule, and for the same reason.
		h.cfg.Logger.Debug(ctx, "trusted issuer token revocation abandoned: the request ended before the assertion could be checked")
	} else {
		h.cfg.Logger.Warn(ctx, "trusted issuer token revocation refused",
			"reason", string(reason), "detail", detail)
	}
	return respondErrorCode(c, http.StatusUnauthorized, "token revocation refused: "+string(reason), string(reason))
}

// MountTrustedIssuerRevocation registers POST /auth/revoke-tokens on exactly
// the condition MountTrustedIssuerAssertion mounts the sign-in on
// (trustedIssuerMountable), and reports whether it mounted. The two belong
// together: the revocation authorizes its caller with the same issuer, key
// list and audience as the sign-in, so an instance that cannot take an
// assertion cannot take a revocation either, and an instance that takes
// sign-ins must offer the door a way to end what those sign-ins handed out.
//
// It logs nothing when it does not mount: MountTrustedIssuerAssertion has
// already said, in one line, what is wrong with the same settings. It is
// called after that one so the lines stay in that order. build runs only when
// the route is mounted.
func MountTrustedIssuerRevocation(
	ctx context.Context,
	g *echo.Group,
	cfg config.Config,
	logger entities.Logger,
	build func() *TokenRevocationHandler,
) bool {
	if !trustedIssuerMountable(cfg) {
		return false
	}
	g.POST("/auth/revoke-tokens", build().RevokeTokens)
	logger.Info(ctx, "trusted-issuer token revocation is on; POST /api/auth/revoke-tokens is mounted",
		"issuer", cfg.TrustedIssuer.IssuerID(),
		"consequence", "the trusted issuer can end one person's access after it resets that person's password: every refresh token of theirs — every connector and every app session — every browser session of theirs, and every authorization code of theirs nobody has redeemed. It reaches that one person and nobody else, so ending a session no longer needs a SESSION_SECRET rotation, which signs everybody out at once. An access token already issued still lives out its hour")
	return true
}
