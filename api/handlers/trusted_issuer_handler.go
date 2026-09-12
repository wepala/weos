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
	"strings"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	"github.com/labstack/echo/v4"
)

// AssertionVerifier checks a login assertion. *trustedissuer.Verifier is the
// implementation; every error it returns is a *trustedissuer.Refusal. A refusal
// made because the request ended first wraps the request's context error.
type AssertionVerifier interface {
	Verify(ctx context.Context, assertion string) (trustedissuer.Identity, error)
}

// AssertedSignInService decides whom an accepted assertion signs in.
// *application.AssertedSignIn is the implementation.
type AssertedSignInService interface {
	SignIn(ctx context.Context, id application.AssertedIdentity) (application.AssertedSignInResult, error)
}

// CodeAmbiguousOwner is the code of the conflict answered when an accepted
// assertion's email is held by more than one person on an allowlisted
// instance, so it cannot say whose identity it is.
const CodeAmbiguousOwner = application.ReasonAmbiguousOwner

// TrustedIssuerHandlerConfig wires the login-assertion endpoint.
type TrustedIssuerHandlerConfig struct {
	Verifier AssertionVerifier
	SignIn   AssertedSignInService
	// Sessions completes an accepted sign-in exactly as password login
	// completes one — the same session, cookie and answer — so the two are
	// indistinguishable to everything downstream.
	Sessions *PasswordAuthHandler
	Logger   entities.Logger
}

// TrustedIssuerHandler answers POST /auth/assert: a person the fleet's front
// door has already verified signs in with the door's signed statement.
type TrustedIssuerHandler struct {
	cfg TrustedIssuerHandlerConfig
}

// NewTrustedIssuerHandler builds the handler.
func NewTrustedIssuerHandler(cfg TrustedIssuerHandlerConfig) *TrustedIssuerHandler {
	return &TrustedIssuerHandler{cfg: cfg}
}

// TrustedIssuerAssertionDeps is what the assertion route takes from the
// running application; its settings come from config.TrustedIssuerConfig.
type TrustedIssuerAssertionDeps struct {
	SignIn AssertedSignInService
	// Sessions is the password-auth handler serve.go already mounts, so an
	// asserted sign-in completes exactly as a password sign-in does.
	Sessions *PasswordAuthHandler
	Logger   entities.Logger
	// Now is the verifier's clock. Optional; time.Now. The acceptance tests
	// set it so a scenario can move time without waiting for it.
	Now func() time.Time
}

// NewTrustedIssuerAssertionHandler builds the handler POST /auth/assert serves:
// a verifier for the configured issuer, key list and audience that accepts
// core's OAuth registry keys as providers and enforces allowedEmails — the
// instance's OAUTH_ALLOWED_EMAILS, empty for none — wired to deps.
//
// The allowlist is a parameter rather than a field of deps so that no caller
// can build the route and forget it: a forgotten allowlist would admit
// everyone the door vouches for.
//
// serve.go and the acceptance tests both build the route through here, so the
// wiring the tests exercise is the wiring that ships.
func NewTrustedIssuerAssertionHandler(
	settings config.TrustedIssuerConfig,
	allowedEmails []string,
	deps TrustedIssuerAssertionDeps,
) *TrustedIssuerHandler {
	return NewTrustedIssuerHandler(TrustedIssuerHandlerConfig{
		Verifier: trustedissuer.NewVerifier(trustedissuer.Config{
			Issuer:        settings.Issuer,
			JWKSURL:       settings.JWKSURL,
			Audience:      settings.Audience,
			Providers:     application.OAuthProviderKeys(),
			AllowedEmails: allowedEmails,
			Now:           deps.Now,
			Logger:        deps.Logger,
		}),
		SignIn:   deps.SignIn,
		Sessions: deps.Sessions,
		Logger:   deps.Logger,
	})
}

// Verifier is the verifier the handler checks assertions with.
func (h *TrustedIssuerHandler) Verifier() AssertionVerifier { return h.cfg.Verifier }

type assertRequest struct {
	Assertion string `json:"assertion"`
}

// AssertBodyLimit is the largest request body POST /auth/assert reads, 16 KiB.
// A login assertion is about a kilobyte; the route is public, so a larger body
// is answered 413 before any of it is parsed.
const AssertBodyLimit = 16 << 10

// Assert verifies the assertion and signs its person in. Every assertion it
// does not accept is answered 401 with the refusal's reason as the answer's
// code, and logged by that reason. The assertion itself is never logged: for
// the minute it is valid it is a bearer credential.
//
// A body larger than AssertBodyLimit is not an assertion refusal: it is
// answered 413, with no reason, and the verifier never sees it.
func (h *TrustedIssuerHandler) Assert(c echo.Context) error {
	ctx := c.Request().Context()
	httpReq := c.Request()
	if httpReq.ContentLength > AssertBodyLimit {
		return h.tooLarge(c)
	}
	httpReq.Body = http.MaxBytesReader(c.Response(), httpReq.Body, AssertBodyLimit)
	var req assertRequest
	if err := c.Bind(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return h.tooLarge(c)
		}
		// A body that does not bind carries no assertion. It is refused like
		// any assertion that cannot be trusted, with a reason, rather than
		// answered as a malformed request.
		req.Assertion = ""
	}

	identity, err := h.cfg.Verifier.Verify(ctx, req.Assertion)
	if err != nil {
		return h.refuse(c, err)
	}

	result, err := h.cfg.SignIn.SignIn(ctx, application.AssertedIdentity{
		Provider: identity.Provider,
		Subject:  identity.Subject,
		Email:    identity.Email,
		// The password registration fallback, so a person the door gives no
		// name is named the same way whichever door they came in by.
		Name: DefaultDisplayName(identity.Email, identity.Name),
	})
	if err != nil {
		if errors.Is(err, application.ErrAmbiguousOwner) {
			// The sign-in service has logged the refusal with every person
			// holding the email, which is what an operator acts on. A second
			// line here would count one refusal twice.
			return respondErrorCode(c, http.StatusConflict,
				"more than one account holds this email, so the sign-in cannot tell whose it is", CodeAmbiguousOwner)
		}
		h.cfg.Logger.Error(ctx, "trusted issuer sign-in: could not find or create the agent",
			"provider", identity.Provider, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to sign in")
	}
	return h.cfg.Sessions.completeAuthAs(c, result.Agent, result.Credential, result.Account, identity.Email,
		func(answer authSuccessResponse) any {
			return assertSuccessResponse{authSuccessResponse: answer, NewAccount: result.NewAccount}
		})
}

// assertSuccessResponse is password sign-in's answer plus new_account. The
// OAuth callback reports the same fact as ?new_account=1 on its redirect; a
// JSON answer has no redirect to carry it. It is always present, false
// included, so a reader never has to tell "false" from "not said".
type assertSuccessResponse struct {
	authSuccessResponse
	NewAccount bool `json:"new_account"`
}

// tooLarge answers a body over AssertBodyLimit. The log line carries the limit
// and nothing of the body.
func (h *TrustedIssuerHandler) tooLarge(c echo.Context) error {
	h.cfg.Logger.Warn(c.Request().Context(), "trusted issuer login assertion request body is too large; it was not read",
		"limit_bytes", AssertBodyLimit)
	return respondError(c, http.StatusRequestEntityTooLarge, "the request body is larger than a login assertion can be")
}

func (h *TrustedIssuerHandler) refuse(c echo.Context, err error) error {
	ctx := c.Request().Context()
	reason, detail := trustedissuer.ReasonSignature, "the assertion could not be verified"
	var refusal *trustedissuer.Refusal
	if errors.As(err, &refusal) {
		reason, detail = refusal.Reason, refusal.Detail
	}
	if ended := ctx.Err(); ended != nil && errors.Is(err, ended) {
		// The request ended while it waited for the issuer's key list. The
		// client went away; the key list did not fail. A warning under the
		// refusal's reason would send an operator after a network fault that
		// never happened, so this is a debug line that names no reason.
		h.cfg.Logger.Debug(ctx, "trusted issuer login assertion abandoned: the request ended before the assertion could be checked")
	} else {
		h.cfg.Logger.Warn(ctx, "trusted issuer login assertion refused",
			"reason", string(reason), "detail", detail)
	}
	return respondErrorCode(c, http.StatusUnauthorized, "login assertion refused: "+string(reason), string(reason))
}

// trustedIssuerMountable reports whether MountTrustedIssuerAssertion mounts
// the route for cfg: all three trusted-issuer settings present, a key-list
// address the verifier may read, and a session secret of the instance's own.
// WithTrustedIssuer offers the door on exactly this, so the providers list
// never offers a door the instance cannot take an assertion from.
func trustedIssuerMountable(cfg config.Config) bool {
	return cfg.TrustedIssuer.Configured() &&
		trustedissuer.CheckJWKSURL(cfg.TrustedIssuer.JWKSURL) == nil &&
		!cfg.UsesPublicSessionSecret()
}

// unmountedIssuerConsequence is what every boot line about an unmounted
// assertion route adds: any trusted-issuer setting makes the API require a
// sign-in (config.Config.AuthEnabled), so an operator must not read "not
// mounted" as "open".
const unmountedIssuerConsequence = "the API still requires a sign-in; with no OAuth provider or password sign-in configured, nobody can sign in"

// MountTrustedIssuerAssertion registers POST /auth/assert when, and only when,
// all three trusted-issuer settings are present, the key-list address is one
// the verifier may read, and SESSION_SECRET is the instance's own. It reports
// whether it mounted the route.
//
// An unmounted route is never registered, so the path answers exactly like one
// the server has never had — the MountPasswordAuth precedent, and for the same
// reason: middleware ordering cannot turn an absent route into a 401.
//
// With none of the settings, nothing is logged: the instance is outside any
// fleet. With one or two, or with an address the verifier refuses to read,
// boot logs one warning saying what is wrong, because the operator asked for
// something they are not getting. With all three and SESSION_SECRET at core's
// public default, or empty, boot logs one error naming SESSION_SECRET: a
// session signed with a key anyone knows can be forged, so the route fails
// closed. build runs only when the route is mounted.
//
// serve.go and the acceptance tests both mount through here so there is one
// copy of this decision rather than two that can drift apart.
func MountTrustedIssuerAssertion(
	ctx context.Context,
	g *echo.Group,
	cfg config.Config,
	logger entities.Logger,
	build func() *TrustedIssuerHandler,
) bool {
	settings := cfg.TrustedIssuer
	if settings.Unset() {
		return false
	}
	if missing := settings.MissingKeys(); len(missing) > 0 {
		logger.Warn(ctx, "trusted-issuer login is partly configured; POST /api/auth/assert is not mounted",
			"missing", strings.Join(missing, ", "),
			"remedy", "set TRUSTED_ISSUER, TRUSTED_ISSUER_JWKS_URL and TRUSTED_ISSUER_AUDIENCE together, or none of them",
			"consequence", unmountedIssuerConsequence)
		return false
	}
	if err := trustedissuer.CheckJWKSURL(settings.JWKSURL); err != nil {
		logger.Warn(ctx, "TRUSTED_ISSUER_JWKS_URL cannot be used; POST /api/auth/assert is not mounted",
			"error", err.Error(),
			"consequence", unmountedIssuerConsequence)
		return false
	}
	if cfg.UsesPublicSessionSecret() {
		// An error, not a warning: every other setting says this instance takes
		// the door's sign-ins, and mounting would hand out sessions anyone can
		// forge and cookies without Secure.
		logger.Error(ctx, "SESSION_SECRET is core's public default or empty, so a session this instance issued could be forged; POST /api/auth/assert is not mounted",
			"remedy", "set SESSION_SECRET to a long random value that belongs to this instance alone",
			"consequence", unmountedIssuerConsequence)
		return false
	}
	g.POST("/auth/assert", build().Assert)
	return true
}
