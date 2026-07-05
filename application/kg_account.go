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

package application

import "context"

// LocalAccountID is the reserved account whose graph serves LOCAL stdio callers
// that have no resolvable account in per-account mode — the stdio exception to
// the otherwise fail-closed rule (a remote request with no account is refused).
// It is not a KSUID (real account ids are base62 KSUIDs), so it can never
// collide with a real account. The same sentinel is the routing target for the
// write side: resources created without an account (a system/stdio writer) are
// projected into this graph, so a local caller reads its own writes.
const LocalAccountID = "local"

type localTransportKey struct{}

// WithLocalTransport marks ctx as originating from the LOCAL stdio MCP transport
// (`weos mcp`). The stdio server roots its whole session in this ctx, so every
// tool call it serves inherits the marker. Per-account store resolution reads it
// to apply the stdio exception — serve the local graph instead of failing closed
// when no account resolves. HTTP requests never carry it, so a remote unresolved
// caller stays fail-closed.
func WithLocalTransport(ctx context.Context) context.Context {
	return context.WithValue(ctx, localTransportKey{}, true)
}

// isLocalTransport reports whether ctx was marked by WithLocalTransport.
func isLocalTransport(ctx context.Context) bool {
	v, _ := ctx.Value(localTransportKey{}).(bool)
	return v
}
