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
	"sync/atomic"
	"testing"
	"time"
)

// replayThroughput appends n events and replays them through a checkpointed
// subscriber into the projection table, returning events per second. This is
// the projection-replay write path: batched transactions, one checkpoint
// advance per batch, WAL commits — on the pure-Go SQLite driver production
// now uses.
func replayThroughput(t testing.TB, n int) float64 {
	t.Helper()
	env := newWorkerTestEnv(t)
	env.appendEvents(t, 0, n)
	var applied int64
	group := SubscriberGroup{Name: "throughput", Handler: projectionHandler(&applied)}
	m := newTestManagerWithGroups(t, env, []SubscriberGroup{group}, nil)
	start := time.Now()
	runManagerUntil(t, m, func() bool { return atomic.LoadInt64(&applied) >= int64(n) })
	return float64(n) / time.Since(start).Seconds()
}

// TestProjectionReplayThroughputSanity guards the switch to the pure-Go
// SQLite driver (glebarez/modernc): replaying events through a checkpointed
// subscriber must stay fast enough for single-user local instances. The floor
// is deliberately far below the observed rate (thousands/sec) so it only
// trips on a real regression — e.g. a driver change that serializes every row
// into its own WAL commit — not on CI noise or the race detector.
func TestProjectionReplayThroughputSanity(t *testing.T) {
	if testing.Short() {
		t.Skip("throughput sanity skipped in -short")
	}
	const n = 500
	rate := replayThroughput(t, n)
	t.Logf("projection replay: %d events at %.0f events/sec", n, rate)
	if rate < 100 {
		t.Fatalf("projection replay throughput = %.0f events/sec, floor is 100 — "+
			"unacceptable for local single-user instances", rate)
	}
}

// BenchmarkProjectionReplay measures the same path for manual comparison
// (e.g. pure-Go vs cgo driver):
//
//	go test ./application/ -bench BenchmarkProjectionReplay -run '^$' -benchtime 2000x
func BenchmarkProjectionReplay(b *testing.B) {
	rate := replayThroughput(b, b.N)
	b.ReportMetric(rate, "events/sec")
}
