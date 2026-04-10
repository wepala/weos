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

package mealplanning

import (
	"weos/application"
	"weos/domain/entities"
)

// baseBehavior holds the BehaviorServices injected via BehaviorFactory
// at startup. All meal-planning behaviors embed this so they share the
// same dependency wiring pattern.
type baseBehavior struct {
	entities.DefaultBehavior
	writer application.ResourceWriter
	logger entities.Logger
	svc    application.BehaviorServices
}

func newBase(svc application.BehaviorServices) baseBehavior {
	return baseBehavior{
		writer: svc.Writer,
		logger: svc.Logger,
		svc:    svc,
	}
}
