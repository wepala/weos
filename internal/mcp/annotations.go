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

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Tool annotations are the machine-readable read-only/mutating contract for
// every WeOS tool, declared once alongside the tool itself. ReadOnlyHint is
// the signal downstream consumers key off — e.g. deriving human-in-the-loop
// confirmation for mutating tools before an in-app agent may execute them.
// Every WeOS tool operates on the local instance only, so the open-world
// hint is always false.

// annReadOnly marks a tool that never modifies the instance.
func annReadOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// annAdditive marks a tool that mutates state only by adding to it (create,
// record). idempotent reports whether repeating the call with the same
// arguments has no further effect.
func annAdditive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(false),
	}
}

// annDestructive marks a tool that overwrites or removes existing state
// (update, delete, replace). Not idempotent: WeOS is event-sourced, so a
// repeated update records another domain event (the log grows, handlers
// re-fire) and a repeated delete errors on the missing entity — clients
// must not retry these calls assuming no additional effect.
func annDestructive() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		IdempotentHint:  false,
		OpenWorldHint:   boolPtr(false),
	}
}

func boolPtr(b bool) *bool { return &b }
