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

package application

import (
	"errors"
	"fmt"

	"github.com/wepala/weos/domain/entities"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
)

// ODRLActions maps short action names used in the API to ODRL IRIs.
var ODRLActions = map[string]string{
	"read":   authentities.ActionRead,
	"modify": authentities.ActionModify,
	"delete": authentities.ActionDelete,
}

// SyncAccessMapToCasbin clears stale role policies and re-adds from the new access map.
// oldAccessMap may be nil (on startup). It preserves admin and owner wildcard policies.
func SyncAccessMapToCasbin(
	checker *authcasbin.CasbinAuthorizationChecker,
	newMap, oldMap entities.AccessMap,
) error {
	var errs []error

	// Collect all roles to clear: union of old and new maps (excluding admin/owner).
	rolesToClear := make(map[string]bool)
	for role := range oldMap {
		if role != authentities.RoleAdmin && role != authentities.RoleOwner {
			rolesToClear[role] = true
		}
	}
	for role := range newMap {
		if role != authentities.RoleAdmin && role != authentities.RoleOwner {
			rolesToClear[role] = true
		}
	}

	// Remove all per-slug policies for roles being updated/removed.
	for role := range rolesToClear {
		if oldResources, ok := oldMap[role]; ok {
			for slug := range oldResources {
				for _, odrl := range ODRLActions {
					if err := checker.RemovePermission(role, odrl, slug); err != nil {
						errs = append(errs, fmt.Errorf("remove %s/%s/%s: %w", role, odrl, slug, err))
					}
				}
			}
		}
		// Also remove wildcard in case it was set.
		for _, odrl := range ODRLActions {
			if err := checker.RemovePermission(role, odrl, "*"); err != nil {
				errs = append(errs, fmt.Errorf("remove %s/%s/*: %w", role, odrl, err))
			}
		}
	}

	// Add policies from the new map.
	for role, resources := range newMap {
		if role == authentities.RoleAdmin || role == authentities.RoleOwner {
			continue
		}
		for slug, actions := range resources {
			for _, action := range actions {
				if odrl, ok := ODRLActions[action]; ok {
					if err := checker.AddPermission(role, odrl, slug); err != nil {
						errs = append(errs, fmt.Errorf("add %s/%s/%s: %w", role, odrl, slug, err))
					}
				}
			}
		}
	}

	if err := SeedAdminPolicies(checker); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// SeedAdminPolicies ensures admin and owner roles have wildcard access.
// Idempotent — safe to call on every startup.
func SeedAdminPolicies(checker *authcasbin.CasbinAuthorizationChecker) error {
	var errs []error
	for _, role := range []string{authentities.RoleAdmin, authentities.RoleOwner} {
		for _, odrl := range ODRLActions {
			if err := checker.AddPermission(role, odrl, "*"); err != nil {
				errs = append(errs, fmt.Errorf("seed %s/%s/*: %w", role, odrl, err))
			}
		}
	}
	return errors.Join(errs...)
}
