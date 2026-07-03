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

package mcp

import (
	"context"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// withCleanConfigurers isolates a test from the package-global configurer
// registry, restoring whatever was there before.
func withCleanConfigurers(t *testing.T) {
	t.Helper()
	saved := customConfigurers
	customConfigurers = nil
	t.Cleanup(func() { customConfigurers = saved })
}

// applyConfigurers runs every registered configurer once, passing the
// dependency bundle through; nil registrations are ignored.
func TestApplyConfigurersRunsRegisteredAndIgnoresNil(t *testing.T) {
	withCleanConfigurers(t)

	calls := 0
	var gotDeps ConfigurerDeps
	RegisterMCPConfigurer(func(_ *mcp.Server, deps ConfigurerDeps) {
		calls++
		gotDeps = deps
	})
	RegisterMCPConfigurer(nil) // must be ignored, not panic

	srv := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	deps := ConfigurerDeps{
		ResourceService:     &stubResourceService{},
		ResourceTypeService: &stubResourceTypeService{},
		Logger:              slog.Default(),
	}
	applyConfigurers(srv, deps)

	if calls != 1 {
		t.Fatalf("configurer ran %d times, want 1 (nil ignored)", calls)
	}
	if gotDeps.ResourceService == nil || gotDeps.ResourceTypeService == nil || gotDeps.Logger == nil {
		t.Errorf("deps not threaded through: %+v", gotDeps)
	}
}

// The HTTP transport applies registered configurers when the handler is
// constructed — the same applyConfigurers call the stdio Run path uses,
// so a downstream tool is registered on either transport.
func TestNewHTTPHandlerAppliesConfigurers(t *testing.T) {
	withCleanConfigurers(t)

	called := 0
	RegisterMCPConfigurer(func(_ *mcp.Server, _ ConfigurerDeps) { called++ })

	if _, err := NewHTTPHandler(&stubResourceTypeService{}, &stubResourceService{}, nil, nil, nil, slog.Default()); err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	if called != 1 {
		t.Errorf("HTTP transport did not apply the configurer (called=%d, want 1)", called)
	}
}

// A configurer can actually register a tool on the server it's handed —
// the end the hook exists for.
func TestConfigurerCanRegisterTool(t *testing.T) {
	withCleanConfigurers(t)

	type emptyIn struct{}
	type emptyOut struct{}
	RegisterMCPConfigurer(func(s *mcp.Server, _ ConfigurerDeps) {
		mcp.AddTool(s, &mcp.Tool{Name: "downstream_tool", Description: "test"},
			func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, emptyOut, error) {
				return nil, emptyOut{}, nil
			})
	})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	// Should not panic and should leave the configurer free to add tools.
	applyConfigurers(srv, ConfigurerDeps{Logger: slog.Default()})
}
