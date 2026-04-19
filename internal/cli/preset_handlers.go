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
	"context"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/labstack/echo/v4"
)

// mountPresetHandlers attaches each MountedHandler to the appropriate Echo
// group: Protected handlers go on `protected` (full auth chain), public
// handlers go on `api` (Messages middleware only).
func mountPresetHandlers(api, protected *echo.Group, mounted application.PresetHTTPHandlers, logger entities.Logger) {
	for _, mh := range mounted {
		group := api
		if mh.Protected {
			group = protected
		}
		group.Add(mh.Method, mh.Path, echo.WrapHandler(mh.Handler))
		logger.Info(context.Background(), "mounted preset handler",
			"method", mh.Method, "path", mh.Path,
			"protected", mh.Protected, "preset", mh.Source)
	}
}
