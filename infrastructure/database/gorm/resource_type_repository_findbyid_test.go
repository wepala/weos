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

package gorm

import (
	"context"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// TestResourceTypeRepository_FindByID_NotFound pins that a missing id maps to
// repositories.ErrNotFound (like FindBySlug). The async Oxigraph projector
// relies on this to skip — rather than permanently park — a ResourceType event
// whose type was since deleted.
func TestResourceTypeRepository_FindByID_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.ResourceType{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := &ResourceTypeRepository{db: db}

	_, err := repo.FindByID(context.Background(), "urn:type:does-not-exist")
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("FindByID(missing) error = %v, want repositories.ErrNotFound", err)
	}
}
