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
	"encoding/json"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PlaybookOutcomeInput records an agent's verdict on a playbook.
type PlaybookOutcomeInput struct {
	ID      string `json:"id" jsonschema:"playbook resource URN (urn:playbook:...)"`
	Outcome string `json:"outcome" jsonschema:"confirmed when the procedure worked, rejected when it failed or misled"`
	Note    string `json:"note,omitempty" jsonschema:"optional short note on what happened"`
}

// PlaybookOutcomeOutput reports the playbook's counters after the verdict.
type PlaybookOutcomeOutput struct {
	ID           string `json:"id"`
	SuccessCount int    `json:"successCount"`
	FailureCount int    `json:"failureCount"`
}

// registerMemoryTools registers the agent-memory tool group.
func registerMemoryTools(server *mcp.Server, playbooks application.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "playbook_record_outcome",
		Description: "Record whether a playbook (learned procedure) worked after using it: " +
			"confirmed increments its success counter, rejected its failure counter. " +
			"The verdict is event-sourced (Playbook.Confirmed / Playbook.Rejected).",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, in PlaybookOutcomeInput,
	) (*mcp.CallToolResult, PlaybookOutcomeOutput, error) {
		res, err := playbooks.RecordOutcome(ctx, in.ID, entities.PlaybookOutcome(in.Outcome), in.Note)
		if err != nil {
			return nil, PlaybookOutcomeOutput{}, err
		}
		out := PlaybookOutcomeOutput{ID: in.ID}
		var node struct {
			SuccessCount int `json:"successCount"`
			FailureCount int `json:"failureCount"`
		}
		if err := json.Unmarshal(application.ExtractEntityNode(res.Data()), &node); err == nil {
			out.SuccessCount = node.SuccessCount
			out.FailureCount = node.FailureCount
		}
		return nil, out, nil
	})
}
