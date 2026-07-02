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

// MemoryRecallInput is a structured recall query over semantic memory.
type MemoryRecallInput struct {
	About   string `json:"about,omitempty" jsonschema:"only facts about this entity URN"`
	Keyword string `json:"keyword,omitempty" jsonschema:"case-insensitive substring of the fact statement"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max facts to return (default 20, max 100)"`
	//nolint:lll // jsonschema description
	IncludeProvenance bool `json:"includeProvenance,omitempty" jsonschema:"include the prov:wasDerivedFrom source event IDs behind each fact"`
}

// MemoryRecallOutput lists the recalled facts.
type MemoryRecallOutput struct {
	Facts []application.RecalledFact `json:"facts"`
}

// registerMemoryTools registers the agent-memory tool group.
func registerMemoryTools(
	server *mcp.Server, playbooks application.PlaybookService, recall application.MemoryRecall,
) {
	registerPlaybookTools(server, playbooks)
	mcp.AddTool(server, &mcp.Tool{
		Name: "memory_recall",
		Description: "Recall consolidated facts from semantic memory. Superseded facts are always " +
			"excluded, and facts written this turn are included even before the knowledge graph " +
			"catches up. Use kg_sparql_query for free-form graph queries instead.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, in MemoryRecallInput,
	) (*mcp.CallToolResult, MemoryRecallOutput, error) {
		facts, err := recall.Recall(ctx, application.RecallQuery{
			About:             in.About,
			Keyword:           in.Keyword,
			Limit:             in.Limit,
			IncludeProvenance: in.IncludeProvenance,
		})
		if err != nil {
			return nil, MemoryRecallOutput{}, err
		}
		return nil, MemoryRecallOutput{Facts: facts}, nil
	})
}

// registerPlaybookTools registers the playbook outcome tool.
func registerPlaybookTools(server *mcp.Server, playbooks application.PlaybookService) {
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
