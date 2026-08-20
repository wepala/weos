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
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/wepala/weos/v3/application"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewConfiguredServer builds the MCP server with all tool groups enabled and
// every downstream configurer applied — the complete tool surface both
// transports and the in-app agent toolset share. kgService may be nil — if
// so the knowledge-graph tools are not registered (and not advertised).
func NewConfiguredServer(
	resourceTypeService application.ResourceTypeService,
	resourceService application.ResourceService,
	kgService application.KnowledgeGraphService,
	lexicalSearch application.LexicalSearch,
	episodicRecall application.EpisodicRecall,
	featureAdmin *application.FeatureAdminService,
	logger *slog.Logger,
) (*gomcp.Server, error) {
	server, err := NewMCPServer(
		resourceTypeService, resourceService, kgService, lexicalSearch, episodicRecall, featureAdmin, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}
	// Custom tools registered by downstream binaries run on every
	// transport; apply them before anything consumes the server.
	applyConfigurers(server, ConfigurerDeps{
		ResourceService:     resourceService,
		ResourceTypeService: resourceTypeService,
		Logger:              logger,
	})
	return server, nil
}

// HandlerForServer serves an already-configured MCP server over Streamable
// HTTP.
func HandlerForServer(server *gomcp.Server, logger *slog.Logger) http.Handler {
	return gomcp.NewStreamableHTTPHandler(func(_ *http.Request) *gomcp.Server {
		return server
	}, &gomcp.StreamableHTTPOptions{
		Logger:         logger,
		SessionTimeout: 30 * time.Minute,
	})
}

// NewHTTPHandler returns an http.Handler that serves the MCP protocol over
// Streamable HTTP with all tool groups enabled.
func NewHTTPHandler(
	resourceTypeService application.ResourceTypeService,
	resourceService application.ResourceService,
	kgService application.KnowledgeGraphService,
	lexicalSearch application.LexicalSearch,
	episodicRecall application.EpisodicRecall,
	featureAdmin *application.FeatureAdminService,
	logger *slog.Logger,
) (http.Handler, error) {
	server, err := NewConfiguredServer(
		resourceTypeService, resourceService, kgService, lexicalSearch, episodicRecall, featureAdmin, logger)
	if err != nil {
		return nil, err
	}
	return HandlerForServer(server, logger), nil
}
