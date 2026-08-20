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

import "github.com/wepala/weos/v3/domain/entities"

// CoreFeatureDeclarations are the features core itself declares. Presets and
// downstream binaries declare their own through the same value group; see
// FeatureDeclarations.
//
// Keep this list short. A feature belongs here only when core ships the call
// site that reads it — a key declared with nothing gating on it is a switch
// wired to nothing, and an operator who flips it learns that the hard way.
func CoreFeatureDeclarations() []entities.FeatureMeta {
	return []entities.FeatureMeta{
		{
			// Gates the episodic_recall MCP tool, at that tool's own
			// mcp.AddTool call site.
			//
			// Declared ON. The tool ships today and instances rely on it, so
			// an upgrade that introduced the gate with a default of off would
			// take a working capability away from every instance until
			// somebody noticed and turned it back on. An operator who wants it
			// dark turns it off, and that is a decision with a name on it.
			Key:         "episodic-recall",
			DisplayName: "Episodic recall",
			Description: "Let an assistant recall past events from the instance's event log.",
			Default:     true,
			Manageable:  true,
			Grantable:   true,
		},
		{
			// Gates the in-app agent's REST surface — the three
			// /api/agent/conversations routes — and the admin's Agent
			// sidebar entry (#486).
			//
			// Declared ON, for the same reason as episodic-recall: the chat
			// ships today and an upgrade that introduced the gate dark would
			// take a working page away from every instance.
			//
			// It composes with #485 rather than overlapping it. Off means
			// there is no assistant at all. On means there is one, whose skill
			// graph #485 filters for whoever is talking to it.
			Key:         FeatureAgentChat,
			DisplayName: "Assistant",
			Description: "Let people chat with the instance's in-app assistant.",
			Default:     true,
			Manageable:  true,
			Grantable:   true,
		},
	}
}

// FeatureAgentChat gates the in-app assistant. Named rather than spelled at
// each call site, because it is read in three places — the REST routes, the
// admin's sidebar, and the declaration above — and a typo in any of them is
// registry drift that leaves the capability open.
const FeatureAgentChat = "agent-chat"
