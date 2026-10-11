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

package oauth

import (
	"context"
	"fmt"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// IssueToken is pericarp's RSA IssueToken, except that a token issued with a
// ctx from WithAccessTokenNotAfter expires at that time when it comes before
// the token's hour (wm-vdax2). Pericarp has no option for a shorter token, so
// the token it signs is read back, given the earlier exp and signed again with
// the same key; every other claim is kept as it was.
func (s *instanceJWTService) IssueToken(
	ctx context.Context, agent *entities.Agent, accounts []*entities.Account, activeAccountID string,
	subscription *auth.SubscriptionClaim, extras map[string]any,
) (string, error) {
	token, err := s.RSAJWTService.IssueToken(ctx, agent, accounts, activeAccountID, subscription, extras)
	if err != nil {
		return "", err
	}
	notAfter := AccessTokenNotAfter(ctx)
	if notAfter.IsZero() {
		return token, nil
	}
	return s.endTokenBy(token, notAfter)
}

// endTokenBy answers token with its exp moved back to notAfter, or token itself
// when it already expires by then.
func (s *instanceJWTService) endTokenBy(token string, notAfter time.Time) (string, error) {
	if s.signingKey == nil || s.publicKey == nil {
		return "", fmt.Errorf("%w: the token service holds no key to end the token at its session's cap",
			authapp.ErrSigningFailed)
	}
	claims := &authapp.PericarpClaims{}
	if _, err := gojwt.ParseWithClaims(token, claims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.publicKey, nil
	}, gojwt.WithoutClaimsValidation()); err != nil {
		return "", fmt.Errorf("%w: read the token back to end it at its session's cap: %v", authapp.ErrSigningFailed, err)
	}
	if claims.ExpiresAt != nil && !notAfter.Before(claims.ExpiresAt.Time) {
		return token, nil
	}
	// NewNumericDate truncates to the second, so the exp is at or before
	// notAfter, never after it.
	claims.ExpiresAt = gojwt.NewNumericDate(notAfter)
	signed, err := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims).SignedString(s.signingKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", authapp.ErrSigningFailed, err)
	}
	return signed, nil
}
