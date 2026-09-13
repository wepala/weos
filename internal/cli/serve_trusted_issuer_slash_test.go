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

package cli

import (
	"net/http"
	"testing"

	"github.com/wepala/weos/v3/internal/config"
)

// An operator who writes TRUSTED_ISSUER with a trailing slash, for a door that
// mints iss without one, gets a door link and a door that works: one value
// serves both. Before, the link looked right and every assertion was refused
// as iss, which on a door-only instance locked everyone out.
func TestServe_TrustedIssuerWrittenWithATrailingSlashTakesTheDoorsAssertion(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	cfg.TrustedIssuer.Issuer = bootDoorIssuer + "/"
	srv := bootServe(t, cfg)

	if !offersTheDoor(t, srv) {
		t.Fatalf("the sign-in screen does not offer the door")
	}
	signIn := serveCall(t, srv, http.MethodPost, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail), nil)
	if signIn.status != http.StatusOK {
		t.Fatalf("POST /api/auth/assert answered %d %s, want 200", signIn.status, signIn.body)
	}
}
