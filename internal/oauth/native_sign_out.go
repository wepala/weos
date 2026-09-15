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
	"crypto/rsa"
	"errors"
	"fmt"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authjwt "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/jwt"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// instanceJWTService is the token service ProvideJWTService builds: pericarp's
// RSA service, which keeps its key to itself, and the public half of that key,
// so a sign-out can check the signature of a token whose hour has passed
// (wm-ehtnq). Every method of the RSA service is its own.
type instanceJWTService struct {
	*authjwt.RSAJWTService
	publicKey *rsa.PublicKey
	// successorKey derives native refresh token successors (see
	// NativeRefreshSuccessorKey). It comes from the signing key, so every
	// process that signs with that key derives the same successors.
	successorKey []byte
}

// errNoSignatureCheck is answered for a token service that holds no key this
// package can check an expired token's signature with.
var errNoSignatureCheck = errors.New("oauth: the token service cannot check an expired token's signature")

// SignedClaimsIgnoringExpiry reads the claims of an access token this instance
// signed, whether or not it has expired. Only a native sign-out uses it, to
// learn which session an app whose token has expired is ending; it must never
// admit a request. It answers an error for a token whose signature does not
// verify with the instance's key, and for a service ProvideJWTService did not
// build.
func SignedClaimsIgnoringExpiry(svc authapp.JWTService, token string) (*authapp.PericarpClaims, error) {
	instance, ok := svc.(*instanceJWTService)
	if !ok || instance == nil || instance.publicKey == nil {
		return nil, errNoSignatureCheck
	}
	claims := &authapp.PericarpClaims{}
	if _, err := gojwt.ParseWithClaims(token, claims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return instance.publicKey, nil
	}, gojwt.WithoutClaimsValidation()); err != nil {
		return nil, fmt.Errorf("oauth: read a signed token: %w", err)
	}
	return claims, nil
}
