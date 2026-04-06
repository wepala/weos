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

// BehaviorSettingsRepository persists per-account behavior configuration
// for resource types. A nil slice from GetByAccountAndType signals that
// no account-level override exists (use preset defaults).
type BehaviorSettingsRepository interface {
	// GetByAccountAndType returns the enabled behavior slugs for a resource
	// type within an account. Returns (nil, nil) when no override exists.
	GetByAccountAndType(ctx context.Context, accountID, typeSlug string) ([]string, error)

	// SaveByAccountAndType upserts the enabled behavior slugs for a resource
	// type within an account.
	SaveByAccountAndType(ctx context.Context, accountID, typeSlug string, enabledSlugs []string) error
}
