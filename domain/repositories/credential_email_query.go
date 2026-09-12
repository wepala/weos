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
// answer: which people hold a credential for this email, compared without
// regard to capitals or surrounding spaces?
//
// pericarp's FindByEmail matches the stored text exactly. Password credentials
// store a lower-case email, but an OAuth or trusted-issuer credential stores
// the email as its provider wrote it, so an exact match would miss the owner
// the moment a provider capitalizes differently. It is a read-only port over a
// table pericarp owns; nothing here writes.
type CredentialEmailQuery interface {
	// AgentIDsByEmail returns, once each, the ids of the agents holding at
	// least one credential — of any kind, active or not — whose email equals
	// email after both are trimmed and lower-cased. An email nobody holds is an
	// empty result, not an error; so is an empty email.
	AgentIDsByEmail(ctx context.Context, email string) ([]string, error)
}
