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

package trustedissuer_test

import (
	"testing"

	"github.com/wepala/weos/v3/internal/trustedissuer"
)

// One issuer, one key list and one audience serve both routes, so an assertion
// minted for one of them must be refused by the other. Otherwise an assertion
// captured on its way to the sign-in would end the person's token access, and
// one captured on its way to the revocation would sign its holder in.
func TestVerifyAcceptsOnlyTheAssertionThatAsksForWhatTheVerifierServes(t *testing.T) {
	cases := map[string]struct {
		serves   string // the verifier's purpose; "" leaves it unset
		asks     string // the assertion's purpose claim; "" carries none
		accepted bool
	}{
		"a login verifier takes an assertion that says nothing":       {serves: "", asks: "", accepted: true},
		"a login verifier takes one that asks to sign in":             {serves: trustedissuer.PurposeLogin, asks: trustedissuer.PurposeLogin, accepted: true},
		"a login verifier refuses one that asks to revoke tokens":     {serves: trustedissuer.PurposeLogin, asks: trustedissuer.PurposeRevokeTokens},
		"a revocation verifier takes one that asks to revoke tokens":  {serves: trustedissuer.PurposeRevokeTokens, asks: trustedissuer.PurposeRevokeTokens, accepted: true},
		"a revocation verifier refuses one that asks to sign in":      {serves: trustedissuer.PurposeRevokeTokens, asks: trustedissuer.PurposeLogin},
		"a revocation verifier refuses one that says nothing":         {serves: trustedissuer.PurposeRevokeTokens, asks: ""},
		"a revocation verifier refuses one asking for something else": {serves: trustedissuer.PurposeRevokeTokens, asks: "delete-everything"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Purpose = c.serves })
			claims := f.good(t)
			if c.asks != "" {
				claims["purpose"] = c.asks
			}

			identity, err := f.verify(f.door.sign(currentKey, claims))

			if c.accepted {
				if err != nil {
					t.Fatalf("the assertion was refused: %v", err)
				}
				if identity.Email != opsEmail {
					t.Fatalf("accepted assertion names %q, want %q", identity.Email, opsEmail)
				}
				return
			}
			requireReason(t, err, trustedissuer.ReasonClaims)
		})
	}
}

// A purpose claim that is THERE but says nothing a route serves — null, a
// number, an array, an object, or text with nothing in it — is refused, by the
// login verifier as much as by the revocation verifier. Reading such a claim
// as the absent one would let an assertion the door minted to end a person's
// access, with a purpose its JSON mangled, sign that person in instead: the
// claim exists to keep the two routes apart, so a malformed one must never
// fall back to the route with the wider reach (Copilot, PR #573).
func TestVerifyRefusesAPurposeThatIsPresentAndSaysNothing(t *testing.T) {
	shapes := map[string]any{
		"null":       nil,
		"a number":   12345,
		"an array":   []string{trustedissuer.PurposeRevokeTokens},
		"an object":  map[string]any{"name": trustedissuer.PurposeRevokeTokens},
		"a boolean":  true,
		"empty text": "",
		"whitespace": "   ",
	}
	serves := map[string]string{
		"a login verifier":      trustedissuer.PurposeLogin,
		"a revocation verifier": trustedissuer.PurposeRevokeTokens,
	}
	for route, purpose := range serves {
		for shape, value := range shapes {
			t.Run(route+" refuses "+shape, func(t *testing.T) {
				f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Purpose = purpose })
				c := f.good(t)
				c["purpose"] = value

				_, err := f.verify(f.door.sign(currentKey, c))

				requireReason(t, err, trustedissuer.ReasonClaims)
			})
		}
	}
}

// A malformed purpose is refused in the same place as every other purpose, so
// the jti it carried is still good: the door corrects the claim and presents
// the same assertion id.
func TestAnAssertionRefusedForAMalformedPurposeDoesNotSpendItsJTI(t *testing.T) {
	f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Purpose = trustedissuer.PurposeRevokeTokens })
	claims := f.good(t)
	claims["purpose"] = []string{trustedissuer.PurposeRevokeTokens}

	if _, err := f.verify(f.door.sign(currentKey, claims)); err == nil {
		t.Fatal("an assertion whose purpose is an array was accepted")
	}

	claims["purpose"] = trustedissuer.PurposeRevokeTokens
	if _, err := f.verify(f.door.sign(currentKey, claims)); err != nil {
		t.Fatalf("the corrected assertion, with the same jti, was refused: %v", err)
	}
}

// The purpose is checked before the jti is spent, like every other claim: an
// issuer that minted the wrong kind corrects it and presents the same jti.
func TestAnAssertionRefusedForItsPurposeDoesNotSpendItsJTI(t *testing.T) {
	f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Purpose = trustedissuer.PurposeRevokeTokens })
	claims := f.good(t)

	if _, err := f.verify(f.door.sign(currentKey, claims)); err == nil {
		t.Fatal("an assertion carrying no purpose was accepted by a revocation verifier")
	}

	claims["purpose"] = trustedissuer.PurposeRevokeTokens
	if _, err := f.verify(f.door.sign(currentKey, claims)); err != nil {
		t.Fatalf("the corrected assertion, with the same jti, was refused: %v", err)
	}
}
