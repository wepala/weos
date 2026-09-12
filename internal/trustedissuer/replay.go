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

package trustedissuer

import (
	"sync"
	"time"
)

// replaySweepInterval bounds how often remember scans for ids it may forget.
const replaySweepInterval = time.Minute

// replayMemory remembers each spent jti for ReplayMemory. It lives in process
// memory and is reset when the instance restarts (see Verifier).
type replayMemory struct {
	mu    sync.Mutex
	until map[string]time.Time
	swept time.Time
}

func newReplayMemory() *replayMemory {
	return &replayMemory{until: map[string]time.Time{}}
}

// remember spends jti and reports whether it was unspent. Checking and
// spending happen under one lock, so two requests racing with one assertion
// cannot both be told it was unspent.
func (m *replayMemory) remember(jti string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until, seen := m.until[jti]; seen && now.Before(until) {
		return false
	}
	m.until[jti] = now.Add(ReplayMemory)
	if now.Sub(m.swept) >= replaySweepInterval {
		for id, until := range m.until {
			if !now.Before(until) {
				delete(m.until, id)
			}
		}
		m.swept = now
	}
	return true
}
