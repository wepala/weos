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

// Package auth provides infrastructure implementations for authentication
// services defined in pericarp's auth application layer.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weos/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth/application"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/segmentio/ksuid"
)

const (
	// inviteTokenIssuer is the "iss" claim value for invite tokens signed by WeOS.
	inviteTokenIssuer = "weos"
)

// HMACInviteTokenService implements pericarp's application.InviteTokenService
// using HMAC-SHA256 signed JWTs. It reuses the existing session secret so
// WeOS deployments can issue invite tokens without any new infrastructure.
type HMACInviteTokenService struct {
	secret []byte
	issuer string
}

// NewHMACInviteTokenService creates a new HMAC-SHA256 invite token service
// from the given secret. The secret must be non-empty.
func NewHMACInviteTokenService(secret string) (*HMACInviteTokenService, error) {
	if secret == "" {
		return nil, application.ErrNoSigningKey
	}
	return &HMACInviteTokenService{
		secret: []byte(secret),
		issuer: inviteTokenIssuer,
	}, nil
}

// IssueInviteToken creates an HMAC-SHA256 signed JWT for the given invite.
// The token's subject and custom invite_id claim are both set to inviteID,
// and it expires after the given duration.
func (s *HMACInviteTokenService) IssueInviteToken(
	ctx context.Context, inviteID string, expiry time.Duration,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if inviteID == "" {
		return "", fmt.Errorf("authentication: invite ID must not be empty")
	}
	if expiry <= 0 {
		return "", fmt.Errorf("authentication: invite token expiry must be positive")
	}

	now := time.Now()
	claims := application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   inviteID,
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(now.Add(expiry)),
			ID:        ksuid.New().String(),
		},
		InviteID: inviteID,
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("%w: %v", application.ErrSigningFailed, err)
	}

	return tokenString, nil
}

// ValidateInviteToken parses and validates an HMAC-SHA256 signed invite token,
// returning the claims. Expired tokens return application.ErrTokenExpired and
// all other parse/signature failures return application.ErrTokenInvalid.
func (s *HMACInviteTokenService) ValidateInviteToken(
	ctx context.Context, tokenString string,
) (*application.InviteClaims, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tokenString == "" {
		return nil, application.ErrTokenInvalid
	}

	claims := &application.InviteClaims{}
	token, err := gojwt.ParseWithClaims(tokenString, claims, func(token *gojwt.Token) (any, error) {
		// Enforce HS256 specifically — accepting any HMAC variant would
		// allow a caller-chosen hash strength that doesn't match the
		// documented algorithm.
		if token.Method != gojwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, gojwt.ErrTokenExpired) {
			return nil, application.ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", application.ErrTokenInvalid, err)
	}
	if !token.Valid {
		return nil, application.ErrTokenInvalid
	}

	// Semantic claim validation — defense in depth beyond JWT's built-in
	// signature/expiry checks. A token signed with the correct secret but
	// missing required fields is still invalid for our use case.
	if claims.Issuer != s.issuer {
		return nil, fmt.Errorf("%w: unexpected issuer %q", application.ErrTokenInvalid, claims.Issuer)
	}
	if claims.ExpiresAt == nil {
		return nil, fmt.Errorf("%w: missing expiry", application.ErrTokenInvalid)
	}
	if claims.InviteID == "" {
		return nil, fmt.Errorf("%w: missing invite_id", application.ErrTokenInvalid)
	}
	if claims.Subject != claims.InviteID {
		return nil, fmt.Errorf("%w: subject does not match invite_id", application.ErrTokenInvalid)
	}

	return claims, nil
}

// ProvideInviteTokenService is the Fx provider that wires an
// HMACInviteTokenService using the configured SESSION_SECRET.
func ProvideInviteTokenService(cfg config.Config) (application.InviteTokenService, error) {
	return NewHMACInviteTokenService(cfg.SessionSecret)
}

// Compile-time assertion that HMACInviteTokenService satisfies the pericarp
// InviteTokenService interface. Lives in the implementation file so normal
// `go build` catches interface drift.
var _ application.InviteTokenService = (*HMACInviteTokenService)(nil)
