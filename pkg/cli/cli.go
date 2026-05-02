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

import (
	internalcli "github.com/wepala/weos/v3/internal/cli"
	"go.uber.org/fx"
)

// Execute runs the weos CLI root command.
//
// Downstream services embedding weos typically call this from main() after
// loading environment variables and calling presets.Register() for any
// custom presets they want to plug into the default registry.
func Execute() error {
	return internalcli.Execute()
}

// RegisterFxOptions appends fx options to be merged into the serve command's
// fx graph. Use this from a downstream binary's main() to plug in app-specific
// providers, invokes, or modules without forking serve.go. Must be called
// before Execute().
func RegisterFxOptions(opts ...fx.Option) {
	internalcli.RegisterFxOptions(opts...)
}

// EchoConfigurer is a downstream-binary hook for adding routes or middleware
// to the HTTP server. Configurers receive the *echo.Echo instance after
// standard middleware and routes are wired but before the server starts
// listening. Use for service-specific endpoints that aren't generic enough
// to belong upstream — e.g. an AT Protocol /client-metadata.json handler
// that derives URLs from request Host instead of a baked-in domain.
//
// Note: the SPA static middleware short-circuits before routing for any URL
// it has a file for. To override a path that exists in the static FS,
// either remove the file from the FS source or attach the handler via
// e.Pre(...) so it runs ahead of static.
type EchoConfigurer = internalcli.EchoConfigurer

// RegisterEchoConfigurer appends a configurer to be invoked against the
// serve command's *echo.Echo instance after standard route wiring. Must be
// called before Execute().
func RegisterEchoConfigurer(c EchoConfigurer) {
	internalcli.RegisterEchoConfigurer(c)
}
