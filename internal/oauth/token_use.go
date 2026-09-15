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

import authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"

// The token endpoint and a native sign-in sign their tokens with the same key,
// and a valid signature is all ValidateToken checks. So every access token the
// token endpoint issues carries TokenUseClaim set to TokenUseConnector, and a
// route that must not answer a third-party connector reads it back with
// IssuedToConnector (wm-8i8ln). A token without the claim is a native sign-in's
// token, including every token issued before the claim existed.
const (
	TokenUseClaim     = "token_use"
	TokenUseConnector = "oauth"
)

// connectorClaims are the extra claims on an access token the token endpoint
// issues. A fresh map each call: IssueToken snapshots its extras, but nothing
// shares this one either way.
func connectorClaims() map[string]any {
	return map[string]any{TokenUseClaim: TokenUseConnector}
}

// IssuedToConnector reports whether claims are those of an access token the
// token endpoint issued to a connector.
func IssuedToConnector(claims *authapp.PericarpClaims) bool {
	if claims == nil {
		return false
	}
	use, _ := claims.Extras[TokenUseClaim].(string)
	return use == TokenUseConnector
}
