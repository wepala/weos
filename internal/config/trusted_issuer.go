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

package config

import "strings"

// The environment variables that configure a trusted issuer.
const (
	EnvTrustedIssuer         = "TRUSTED_ISSUER"
	EnvTrustedIssuerJWKSURL  = "TRUSTED_ISSUER_JWKS_URL"
	EnvTrustedIssuerAudience = "TRUSTED_ISSUER_AUDIENCE"
)

// TrustedIssuerConfig names the one service — a fleet's front door — whose
// signed login assertions this instance accepts at POST /api/auth/assert.
//
// All three settings are needed. With all three the route is mounted; with
// none the instance is exactly what it was before the route existed; with one
// or two, boot warns and mounts nothing. See
// docs/decisions/trusted-issuer-login-assertion.md.
type TrustedIssuerConfig struct {
	// Issuer is the exact iss value an assertion must carry (TRUSTED_ISSUER).
	Issuer string
	// JWKSURL is where the issuer publishes its signing keys
	// (TRUSTED_ISSUER_JWKS_URL). It must be https, or http to a loopback host.
	JWKSURL string
	// Audience is this instance's fleet id, the exact aud value an assertion
	// must carry (TRUSTED_ISSUER_AUDIENCE).
	Audience string
}

// MissingKeys names the settings that are not set, in a fixed order. A value
// that is only whitespace is not set. Nil when all three are present.
func (c TrustedIssuerConfig) MissingKeys() []string {
	var missing []string
	if strings.TrimSpace(c.Issuer) == "" {
		missing = append(missing, EnvTrustedIssuer)
	}
	if strings.TrimSpace(c.JWKSURL) == "" {
		missing = append(missing, EnvTrustedIssuerJWKSURL)
	}
	if strings.TrimSpace(c.Audience) == "" {
		missing = append(missing, EnvTrustedIssuerAudience)
	}
	return missing
}

// Configured reports whether all three settings are present.
func (c TrustedIssuerConfig) Configured() bool { return len(c.MissingKeys()) == 0 }

// Unset reports whether none of the three settings is present — an instance
// outside any fleet.
func (c TrustedIssuerConfig) Unset() bool { return len(c.MissingKeys()) == 3 }
