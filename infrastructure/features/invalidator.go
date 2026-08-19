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

// Package features holds the deployment-specific half of feature-cache
// invalidation. The behaviour in front of the interface never varies; only the
// machinery behind it does, and it lives here so swapping the transport later
// touches this package and nothing else.
package features

import (
	"context"
	"fmt"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// NotifyInvalidator broadcasts invalidations to other replicas over Postgres
// LISTEN/NOTIFY, wrapping the local in-process invalidator rather than
// replacing it: the writing process still evicts precisely and immediately,
// and the broadcast is what reaches everybody else.
//
// It is deliberately NOT a checkpointed subscriber group. Those lock their
// checkpoint row FOR UPDATE SKIP LOCKED on Postgres, which makes N replicas
// competing consumers — exactly one would process each event and every other
// replica would serve a stale answer indefinitely. Invalidation needs a
// broadcast, and LISTEN/NOTIFY is the one this stack already has.
//
// The notification carries no payload. pericarp's listener hands subscribers a
// bare wake signal (Subscribe returns <-chan struct{}), so a replica receiving
// one cannot tell what changed and drops its whole cache. That is safe by
// construction — over-invalidation costs a re-resolve and can never produce a
// stale answer — and precision is preserved where it is cheap, in the process
// that made the change.
//
// NOTIFY is not durable: a replica whose LISTEN connection is down misses
// those messages outright. The resolver's maximum cache age is what bounds
// that, and on a replica deployment it is load-bearing rather than a nicety.
type NotifyInvalidator struct {
	local   repositories.FeatureCacheInvalidator
	db      *gorm.DB
	channel string
	logger  entities.Logger
}

// NewNotifyInvalidator wraps a local invalidator with a Postgres broadcast.
//
// The constructor lives here and the SELECTION of which invalidator to use
// lives in application, because choosing requires the concrete resolver and
// infrastructure must not import application.
func NewNotifyInvalidator(
	local repositories.FeatureCacheInvalidator,
	db *gorm.DB,
	channel string,
	logger entities.Logger,
) *NotifyInvalidator {
	if channel == "" {
		channel = DefaultNotifyChannel
	}
	return &NotifyInvalidator{local: local, db: db, channel: channel, logger: logger}
}

// DefaultNotifyChannel is used when configuration names none.
const DefaultNotifyChannel = "weos_feature_cache"

// Listen drops the local cache whenever another replica broadcasts, and blocks
// until ctx is cancelled. Callers run it in a goroutine started from an fx
// OnStart hook and cancel it on OnStop.
func (n *NotifyInvalidator) Listen(ctx context.Context, dsn string) error {
	return n.listen(ctx, dsn)
}

// InvalidateAll evicts locally, then tells every other replica to do the same.
func (n *NotifyInvalidator) InvalidateAll(ctx context.Context) {
	n.local.InvalidateAll(ctx)
	n.broadcast(ctx)
}

// InvalidateAccount evicts this account's entries locally. Other replicas drop
// their whole cache, because the wake signal carries no account to narrow by.
func (n *NotifyInvalidator) InvalidateAccount(ctx context.Context, accountID string) {
	n.local.InvalidateAccount(ctx, accountID)
	n.broadcast(ctx)
}

// InvalidateAgents evicts these people's entries locally, and asks other
// replicas to drop everything.
func (n *NotifyInvalidator) InvalidateAgents(ctx context.Context, accountID string, agentIDs ...string) {
	n.local.InvalidateAgents(ctx, accountID, agentIDs...)
	n.broadcast(ctx)
}

// broadcast sends the wake signal. A failure is logged and swallowed: the
// local eviction already happened and the caller's write already landed, so
// failing here would report an error for work that succeeded. The other
// replicas converge on the maximum cache age instead.
func (n *NotifyInvalidator) broadcast(ctx context.Context) {
	if err := n.db.WithContext(ctx).
		Exec("SELECT pg_notify(?, '')", n.channel).Error; err != nil {
		n.log(ctx, "could not broadcast a feature-cache invalidation; other replicas "+
			"will converge when their cached sets reach the maximum age",
			"channel", n.channel, "error", err)
	}
}

func (n *NotifyInvalidator) log(ctx context.Context, msg string, kv ...any) {
	if n.logger == nil {
		return
	}
	n.logger.Warn(ctx, msg, kv...)
}

// listen drops the local cache whenever another replica broadcasts. Runs until
// ctx is cancelled; the fx OnStop hook cancels it.
func (n *NotifyInvalidator) listen(ctx context.Context, dsn string) error {
	listener, err := subscriptions.NewPostgresListener(dsn,
		subscriptions.WithListenerChannel(n.channel))
	if err != nil {
		return fmt.Errorf("failed to start the feature-cache listener: %w", err)
	}
	wakes := listener.Subscribe()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wakes:
				// No payload to narrow by, so drop everything. Safe: an extra
				// resolve costs one read, a stale answer costs correctness.
				n.local.InvalidateAll(ctx)
			}
		}
	}()
	return listener.Run(ctx)
}
