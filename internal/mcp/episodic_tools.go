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

	"github.com/wepala/weos/v3/application"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EpisodicRecallInput is a scoped query over the event log. All filters combine.
type EpisodicRecallInput struct {
	//nolint:lll // jsonschema description
	From string `json:"from,omitempty" jsonschema:"start of the time window — RFC3339 or relative (last 7 days, 3 days ago)"`
	//nolint:lll // jsonschema description
	Until string `json:"until,omitempty" jsonschema:"end of the time window — RFC3339 or relative; open when omitted"`
	//nolint:lll // jsonschema description
	About []string `json:"about,omitempty" jsonschema:"only events involving these resource URNs — as the event's aggregate or referenced in its payload"`
	//nolint:lll // jsonschema description
	EventType    string `json:"eventType,omitempty" jsonschema:"only events of this stored type (e.g. Resource.Created)"`
	ResourceType string `json:"resourceType,omitempty" jsonschema:"only events on resources of this type slug"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max events to return (default 20, max 100)"`
	//nolint:lll // jsonschema description
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call — re-send the same filters with it"`
}

// EpisodicRecallOutput is one time-ordered page of compact events.
type EpisodicRecallOutput struct {
	Events  []application.RecalledEvent `json:"events"`
	Cursor  string                      `json:"cursor,omitempty"`
	HasMore bool                        `json:"has_more"`
}

// registerEpisodicTools registers the episodic-memory tool group.
func registerEpisodicTools(server *mcp.Server, episodic application.EpisodicRecall) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "episodic_recall",
		Description: "Recall what happened from the event log: a time-ordered, paginated slice of " +
			"events, filterable by time window (absolute or relative), anchor resource URNs " +
			"(one or more — matched as the event's aggregate or referenced in its payload), " +
			"event type, and resource type. Results are compact summaries — deterministic " +
			"retrieval, no ranking. Use memory_recall for consolidated facts.",
		Annotations: annReadOnly(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, in EpisodicRecallInput,
	) (*mcp.CallToolResult, EpisodicRecallOutput, error) {
		res, err := episodic.Recall(ctx, application.EpisodicQuery{
			From:         in.From,
			Until:        in.Until,
			Anchors:      in.About,
			EventType:    in.EventType,
			ResourceType: in.ResourceType,
			Limit:        in.Limit,
			Cursor:       in.Cursor,
		})
		if err != nil {
			return nil, EpisodicRecallOutput{}, err
		}
		return nil, EpisodicRecallOutput{
			Events:  res.Events,
			Cursor:  res.Cursor,
			HasMore: res.HasMore,
		}, nil
	})
}
