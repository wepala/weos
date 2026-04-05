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
	"net/http"

	"weos/application"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler returns an http.Handler that serves the MCP protocol over
// Streamable HTTP with all tool groups enabled.
func NewHTTPHandler(
	resourceTypeService application.ResourceTypeService,
	resourceService application.ResourceService,
) http.Handler {
	server := NewMCPServer(resourceTypeService, resourceService, nil)
	return gomcp.NewStreamableHTTPHandler(func(_ *http.Request) *gomcp.Server {
		return server
	}, nil)
}
