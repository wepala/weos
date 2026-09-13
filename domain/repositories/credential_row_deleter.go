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

// CredentialRowDeleter removes one row from pericarp's credentials projection.
//
// Owner binding uses it for one thing: taking back a linked credential whose
// Credential.Created event could not be committed after its row was saved. A
// row with no event works until a projection rebuild silently drops it, so the
// row is deleted and the sign-in fails instead. pericarp's CredentialRepository
// has no delete, and its UnitOfWork carries events only, so the row and its
// event cannot be written in one transaction from core.
type CredentialRowDeleter interface {
	// DeleteCredentialRow deletes the credential row with id. A row that does
	// not exist is not an error.
	DeleteCredentialRow(ctx context.Context, id string) error
}
