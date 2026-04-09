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

// Package cli provides a public wrapper around the internal weos CLI for use
// by downstream services that embed the weos binary as a library.
//
// The primary CLI implementation lives in weos/internal/cli. Go's internal
// package rules prevent that package from being imported outside the weos
// module, so this thin re-export exists to give downstream binaries a stable
// public entry point.
package cli

import internalcli "weos/internal/cli"

// Execute runs the weos CLI root command.
//
// Downstream services embedding weos typically call this from main() after
// loading environment variables and calling presets.Register() for any
// custom presets they want to plug into the default registry.
func Execute() error {
	return internalcli.Execute()
}
