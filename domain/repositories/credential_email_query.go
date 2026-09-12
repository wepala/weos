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

package repositories

import "context"

// CredentialEmailQuery reads pericarp's credentials projection for the one
// question owner binding needs and pericarp's own CredentialRepository cannot
// answer: which credentials hold this email, compared without regard to
// capitals or surrounding spaces?
//
// pericarp's FindByEmail matches the stored text exactly. Password credentials
// store a lower-case email, but an OAuth or trusted-issuer credential stores
// the email as its provider wrote it, so an exact match would miss the owner
// the moment a provider capitalizes differently. It is a read-only port over a
// table pericarp owns; nothing here writes.
type CredentialEmailQuery interface {
	// CredentialsByEmail returns every credential — of any kind, active or
	// not — whose email equals email after both are trimmed and lower-cased.
	// Which of them may say who owns the email is the caller's decision, so
	// none is left out here. An email nobody holds is an empty result, not an
	// error; so is an empty email.
	CredentialsByEmail(ctx context.Context, email string) ([]CredentialEmailMatch, error)
}

// CredentialEmailMatch is one credential CredentialsByEmail found.
type CredentialEmailMatch struct {
	// AgentID is the person the credential belongs to.
	AgentID string
	// Provider is how the credential signs in: "password", or a provider's
	// registry key.
	Provider string
	// Active is false once the credential has been turned off.
	Active bool
}
