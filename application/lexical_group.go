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
	"fmt"
	"sort"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/identity"

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
// the index is active (SQLite FTS5 or PostgreSQL), and nothing otherwise. The
// index is a rebuildable derived view: `worker checkpoint reset lexical-index
// --truncate` clears it and replays history through this handler. A group
// that has never run has no checkpoint row, so its first run starts at 0 and
// replays all history — which is how a deployment whose index did not exist
// before (PostgreSQL before wm-irqzji) fills it on first start.
func ProvideLexicalGroup(p LexicalGroupParams) []SubscriberGroup {
	if !p.Index.Active() {
		p.Logger.Debug(context.Background(),
			"lexical index inactive, lexical-index subscriber not registered")
		return nil
	}
	return []SubscriberGroup{{
		Name:     repositories.LexicalIndexSubscriber,
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
			return indexResource(ctx, index, logger, event.AggregateID, typeSlug, state.Data,
				func() (repositories.LexicalOwner, error) {
					return lexicalOwner(ctx, eventStore, index, event, state)
				})
		case "Resource.AccountAssigned":
			return reindexAssignedOwner(ctx, eventStore, index, logger, event)
		default:
			return nil
		}
	}
}

// indexResource writes one resource's text to the index, or removes its row
// when there is no text to search (or it is a superseded fact). owner is
// called only when a row is written.
func indexResource(
	ctx context.Context, index repositories.LexicalIndex, logger entities.Logger,
	aggregateID, typeSlug string, data json.RawMessage,
	owner func() (repositories.LexicalOwner, error),
) error {
	var node map[string]any
	if err := json.Unmarshal(ExtractEntityNode(data), &node); err != nil {
		logger.Error(ctx, "lexical index: unparseable entity node, skipping",
			"resource", aggregateID, "error", err)
		return nil
	}
	if typeSlug == "fact" {
		if invalidated, _ := node["invalidatedAtTime"].(string); invalidated != "" {
			return index.Remove(ctx, aggregateID)
		}
	}
	content := lexicalContent(node)
	if content == "" {
		return index.Remove(ctx, aggregateID)
	}
	o, err := owner()
	if err != nil {
		return err
	}
	return index.Index(ctx, aggregateID, typeSlug, content, o)
}

// reindexAssignedOwner re-indexes a resource under the account a
// Resource.AccountAssigned gave it. The event carries only the account, so
// the resource's text and type are rebuilt from its history, the same state a
// Resource.Published reads. The first owner stands: a row already indexed
// under an account keeps it. A resource deleted before the assignment, or one
// with no text, is left as the delete or the publish left it.
func reindexAssignedOwner(
	ctx context.Context, eventStore domain.EventStore, index repositories.LexicalIndex,
	logger entities.Logger, event domain.EventEnvelope[any],
) error {
	history, err := eventStore.GetEvents(ctx, event.AggregateID)
	if err != nil {
		return fmt.Errorf("lexical index: history of %s: %w", event.AggregateID, err)
	}
	state := buildStateFromTransaction(ctx, history, event.AggregateID, event.SequenceNo, logger)
	if state.IsDelete || state.Data == nil {
		return nil
	}
	typeSlug := state.TypeSlug
	if typeSlug == "" {
		typeSlug = identity.ExtractResourceTypeSlug(event.AggregateID)
	}
	return indexResource(ctx, index, logger, event.AggregateID, typeSlug, state.Data,
		func() (repositories.LexicalOwner, error) {
			owner := repositories.LexicalOwner{CreatedBy: state.CreatedBy, AccountID: state.AccountID}
			if indexed, found, err := index.Owner(ctx, event.AggregateID); err != nil {
				return owner, fmt.Errorf("lexical index: owner of %s: %w", event.AggregateID, err)
			} else if found {
				owner = indexed
			}
			if owner.AccountID == "" {
				owner.AccountID = accountFromPayload(event.Payload)
			}
			return owner, nil
		})
}

// lexicalOwner reads who owns the published resource from its events — never
// from ctx, which during a rebuild belongs to no caller at all. A create
// carries the owner in its own transaction. An update does not, and an owner
// never changes, so it is read from the resource's existing index row; only a
// resource with no row yet (a refill that starts mid-history, a row removed
// when its text emptied) costs a load of the aggregate's history, to read its
// Resource.Created. A rebuild replays the create before any update, so the
// row is always there for a replayed update. A failed lookup is returned so
// the subscriber retries the event rather than indexing the resource as
// ownerless, which would show it to every caller.
func lexicalOwner(
	ctx context.Context, eventStore domain.EventStore, index repositories.LexicalIndex,
	event domain.EventEnvelope[any], state txResourceState,
) (repositories.LexicalOwner, error) {
	owner := repositories.LexicalOwner{CreatedBy: state.CreatedBy, AccountID: state.AccountID}
	if !state.IsCreate {
		indexed, found, err := index.Owner(ctx, event.AggregateID)
		if err != nil {
			return owner, fmt.Errorf("lexical index: owner of %s: %w", event.AggregateID, err)
		}
		if found {
			return indexed, nil
		}
		account, createdBy, err := resourceOwnerFromHistory(ctx, eventStore, event.AggregateID)
		if err != nil {
			return owner, fmt.Errorf("lexical index: owner of %s: %w", event.AggregateID, err)
		}
		owner = repositories.LexicalOwner{CreatedBy: createdBy, AccountID: account}
	}
	if owner.AccountID == "" {
		owner.AccountID = accountFromPayload(event.Payload)
	}
	return owner, nil
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
