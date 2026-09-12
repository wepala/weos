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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// AssertionVerifier checks a login assertion. *trustedissuer.Verifier is the
// implementation; every error it returns is a *trustedissuer.Refusal.
type AssertionVerifier interface {
	Verify(ctx context.Context, assertion string) (trustedissuer.Identity, error)
}

// TrustedIssuerHandlerConfig wires the login-assertion endpoint.
type TrustedIssuerHandlerConfig struct {
	Verifier    AssertionVerifier
	AuthService authapp.AuthenticationService
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

type assertRequest struct {
	Assertion string `json:"assertion"`
}

// Assert verifies the assertion and signs its person in. Every assertion it
// does not accept is answered 401 with the refusal's reason as the answer's
// code, and logged by that reason. The assertion itself is never logged: for
// the minute it is valid it is a bearer credential.
func (h *TrustedIssuerHandler) Assert(c echo.Context) error {
	ctx := c.Request().Context()
	var req assertRequest
	if err := c.Bind(&req); err != nil {
		// A body that does not bind carries no assertion. It is refused like
		// any assertion that cannot be trusted, with a reason, rather than
		// answered as a malformed request.
		req.Assertion = ""
	}

	identity, err := h.cfg.Verifier.Verify(ctx, req.Assertion)
	if err != nil {
		return h.refuse(c, err)
	}

	agent, credential, account, err := h.cfg.AuthService.FindOrCreateAgent(ctx, authapp.UserInfo{
		ProviderUserID: identity.Subject,
		Email:          identity.Email,
		DisplayName:    DefaultDisplayName(identity.Email, identity.Name),
		Provider:       identity.Provider,
	})
	if err != nil {
		h.cfg.Logger.Error(ctx, "trusted issuer sign-in: could not find or create the agent",
			"provider", identity.Provider, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to sign in")
	}
	return h.cfg.Sessions.completeAuth(c, agent, credential, account, identity.Email)
}

func (h *TrustedIssuerHandler) refuse(c echo.Context, err error) error {
	reason, detail := trustedissuer.ReasonSignature, "the assertion could not be verified"
	var refusal *trustedissuer.Refusal
	if errors.As(err, &refusal) {
		reason, detail = refusal.Reason, refusal.Detail
	}
	h.cfg.Logger.Warn(c.Request().Context(), "trusted issuer login assertion refused",
		"reason", string(reason), "detail", detail)
	return respondErrorCode(c, http.StatusUnauthorized, "login assertion refused: "+string(reason), string(reason))
}

// MountTrustedIssuerAssertion registers POST /auth/assert when, and only when,
// all three trusted-issuer settings are present and the key-list address is
// one the verifier may read. It reports whether it mounted the route.
//
// An unmounted route is never registered, so the path answers exactly like one
// the server has never had — the MountPasswordAuth precedent, and for the same
// reason: middleware ordering cannot turn an absent route into a 401.
//
// With none of the settings, nothing is logged: the instance is outside any
// fleet. With one or two, or with an address the verifier refuses to read,
// boot logs one warning saying what is wrong, because the operator asked for
// something they are not getting. build runs only when the route is mounted.
//
// serve.go and the acceptance tests both mount through here so there is one
// copy of this decision rather than two that can drift apart.
func MountTrustedIssuerAssertion(
	ctx context.Context,
	g *echo.Group,
	settings config.TrustedIssuerConfig,
	logger entities.Logger,
	build func() *TrustedIssuerHandler,
) bool {
	if settings.Unset() {
		return false
	}
	if missing := settings.MissingKeys(); len(missing) > 0 {
		logger.Warn(ctx, "trusted-issuer login is partly configured; POST /api/auth/assert is not mounted",
			"missing", strings.Join(missing, ", "),
			"remedy", "set TRUSTED_ISSUER, TRUSTED_ISSUER_JWKS_URL and TRUSTED_ISSUER_AUDIENCE together, or none of them")
		return false
	}
	if err := trustedissuer.CheckJWKSURL(settings.JWKSURL); err != nil {
		logger.Warn(ctx, "TRUSTED_ISSUER_JWKS_URL cannot be used; POST /api/auth/assert is not mounted",
			"error", err.Error())
		return false
	}
	g.POST("/auth/assert", build().Assert)
	return true
}
