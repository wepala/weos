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
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// TestApplicationModuleGraphValidates checks that the full dependency graph
// assembled by application.Module is resolvable — all provides, decorations,
// and invokes wire up — without starting it (no DB, no lifecycle). This guards
// against graph-construction regressions (e.g. two decorations of the same type
// in one module) that the focused unit tests, which bypass the Fx graph, miss.
func TestApplicationModuleGraphValidates(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseDSN = ":memory:"

	var manager *application.Manager
	if err := fx.ValidateApp(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&manager),
	); err != nil {
		t.Fatalf("application.Module graph failed validation: %v", err)
	}
}
