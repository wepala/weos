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

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
)

// LexicalGroupParams bundles the lexical index subscriber's dependencies.
type LexicalGroupParams struct {
	fx.In
	EventStore domain.EventStore
	Index      repositories.LexicalIndex
	Logger     entities.Logger
}

// ProvideLexicalGroup contributes the "lexical-index" subscriber group when
// the index is active (SQLite with FTS5), and nothing otherwise. The index is
// a rebuildable derived view: `worker checkpoint reset lexical-index
// --truncate` clears it and replays history through this handler.
func ProvideLexicalGroup(p LexicalGroupParams) []SubscriberGroup {
	if !p.Index.Active() {
		p.Logger.Debug(context.Background(),
			"lexical index inactive, lexical-index subscriber not registered")
		return nil
	}
	return []SubscriberGroup{{
		Name:     "lexical-index",
		Handler:  lexicalHandler(p.EventStore, p.Index, p.Logger),
		Truncate: p.Index.Clear,
	}}
}

// lexicalHandler keeps the full-text index in step with resource events.
// Superseded facts are dropped from the index so lexical recall never
// surfaces them.
func lexicalHandler(
	eventStore domain.EventStore,
	index repositories.LexicalIndex,
	logger entities.Logger,
) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		switch event.EventType {
		case "Resource.Deleted":
			return index.Remove(ctx, event.AggregateID)
		case "Resource.Published":
			if event.TransactionID == "" {
				return nil
			}
			txEvents, err := eventStore.GetEventsByTransactionID(ctx, event.TransactionID)
			if err != nil {
				return err
			}
			state := buildStateFromTransaction(ctx, txEvents, event.AggregateID, event.SequenceNo, logger)
			if state.IsDelete || state.Data == nil {
				return nil
			}
			typeSlug := publishedTypeSlug(event.Payload, state.TypeSlug)
			var node map[string]any
			if err := json.Unmarshal(ExtractEntityNode(state.Data), &node); err != nil {
				logger.Error(ctx, "lexical index: unparseable entity node, skipping",
					"resource", event.AggregateID, "error", err)
				return nil
			}
			if typeSlug == "fact" {
				if invalidated, _ := node["invalidatedAtTime"].(string); invalidated != "" {
					return index.Remove(ctx, event.AggregateID)
				}
			}
			content := lexicalContent(node)
			if content == "" {
				return index.Remove(ctx, event.AggregateID)
			}
			return index.Index(ctx, event.AggregateID, typeSlug, content)
		default:
			return nil
		}
	}
}

// lexicalContent flattens a resource's entity node into one searchable string:
// every text literal (including strings inside arrays), in deterministic key
// order so replays produce identical index content.
func lexicalContent(node map[string]any) string {
	keys := make([]string, 0, len(node))
	for k := range node {
		if strings.HasPrefix(k, "@") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		switch v := node[k].(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}
