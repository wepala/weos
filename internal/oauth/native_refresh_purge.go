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

package oauth

import (
	"context"
	"sync"
	"time"
)

// NativeRefreshTokenPurgeHorizon is how long past its expiry a native refresh
// token row is kept (wm-sa7wv). A spent or revoked row stays until then, so
// presenting it is still caught as reuse, and ends its family, for the token's
// whole 30-day life and a week after. Past that the token could only ever be
// refused, which is what an unknown token gets too, so only the reuse signal
// is lost. A row lives at most NativeRefreshTokenTTL plus the horizon: 37 days.
const NativeRefreshTokenPurgeHorizon = 7 * 24 * time.Hour

// NativeRefreshTokenPurgeInterval is the least time between two purges in one
// process.
const NativeRefreshTokenPurgeInterval = time.Hour

// PurgeExpired deletes the refresh tokens held for clientID that expired before
// before, revoked or not, and answers how many it deleted.
func (r *gormRefreshTokenRepo) PurgeExpired(ctx context.Context, clientID string, before time.Time) (int64, error) {
	if clientID == "" {
		return 0, nil
	}
	result := r.db.WithContext(ctx).
		Where("client_id = ? AND expires_at < ?", clientID, before).
		Delete(&OAuthRefreshToken{})
	return result.RowsAffected, result.Error
}

// NativeRefreshTokenPurger purges native refresh token rows past the horizon
// from where native refresh tokens are written — a native sign-in and a
// renewal — at most once every NativeRefreshTokenPurgeInterval, so the purge
// needs no timer of its own.
type NativeRefreshTokenPurger struct {
	repo RefreshTokenRepository
	now  func() time.Time
	mu   sync.Mutex
	last time.Time
}

// NewNativeRefreshTokenPurger makes a purger over repo. now is the clock; nil
// is time.Now.
func NewNativeRefreshTokenPurger(repo RefreshTokenRepository, now func() time.Time) *NativeRefreshTokenPurger {
	if now == nil {
		now = time.Now
	}
	return &NativeRefreshTokenPurger{repo: repo, now: now}
}

// MaybePurge purges when the interval has passed since this process last
// purged, and reports whether it ran and how many rows it deleted. A purge that
// fails still counts as the last one, so a failing store is not asked again on
// every request.
func (p *NativeRefreshTokenPurger) MaybePurge(ctx context.Context) (ran bool, purged int64, err error) {
	if p == nil || p.repo == nil {
		return false, 0, nil
	}
	now := p.now()
	p.mu.Lock()
	if !p.last.IsZero() && now.Sub(p.last) < NativeRefreshTokenPurgeInterval {
		p.mu.Unlock()
		return false, 0, nil
	}
	p.last = now
	p.mu.Unlock()
	purged, err = p.repo.PurgeExpired(ctx, NativeClientID, now.Add(-NativeRefreshTokenPurgeHorizon))
	return true, purged, err
}
