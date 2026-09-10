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

//go:build !oxigraph_embedded

package e2e

import "github.com/cucumber/godog"

// embeddedGraphBuilt reports whether this binary carries the embedded graph
// store. Without it the @requires-embedded-graph scenarios have no steps and
// TestAccountDeletion leaves them out.
const embeddedGraphBuilt = false

func (w *deletionWorld) registerGraphSteps(*godog.ScenarioContext) {}
