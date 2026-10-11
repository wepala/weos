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

	"gorm.io/gorm"
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
//
// A family's cap (family_expires_at) can live on its deleted rows alone: a
// renewal a binary from before the cap served wrote a successor with no cap and
// a 30-day life, which outlives the capped row by more than the horizon. So in
// the same transaction, before the delete, every row that stays and carries no
// cap is given the earliest cap its family's deleted rows carry. Without that a
// renewal of the successor would find no cap and outlive the session's end.
func (r *gormRefreshTokenRepo) PurgeExpired(ctx context.Context, clientID string, before time.Time) (int64, error) {
	if clientID == "" {
		return 0, nil
	}
	var purged int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		backfill := tx.Exec(purgeCapBackfillSQL, clientID, before, clientID, before)
		if backfill.Error != nil {
			return backfill.Error
		}
		result := tx.Where("client_id = ? AND expires_at < ?", clientID, before).Delete(&OAuthRefreshToken{})
		if result.Error != nil {
			return result.Error
		}
		purged = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	return purged, nil
}

// purgeCapBackfillSQL gives every row of clientID that the purge before
// before leaves, and that carries no cap, the earliest cap of its family — when
// a row the purge deletes carries one. Its arguments are clientID, before,
// clientID, before. The table is oauth_refresh_tokens (OAuthRefreshToken).
const purgeCapBackfillSQL = `UPDATE oauth_refresh_tokens SET family_expires_at = (
	SELECT MIN(c.family_expires_at) FROM oauth_refresh_tokens c
	WHERE c.family_id = oauth_refresh_tokens.family_id AND c.family_expires_at IS NOT NULL
) WHERE client_id = ? AND family_expires_at IS NULL AND expires_at >= ? AND family_id IN (
	SELECT p.family_id FROM oauth_refresh_tokens p
	WHERE p.client_id = ? AND p.family_expires_at IS NOT NULL AND p.expires_at < ?
)`

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
