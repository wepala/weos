package entities

import (
	"testing"
	"time"
)

func at(base time.Time, d time.Duration) *time.Time {
	t := base.Add(d)
	return &t
}

func grantRow(key string, from, through *time.Time) FeatureGrantRecord {
	return FeatureGrantRecord{
		SubjectType: "agent", SubjectID: "agent-ops", AccountID: "acct",
		FeatureKey: key, ValidFrom: from, ValidThrough: through,
	}
}

// TestGrantWindowIsHalfOpen pins the two instants that decide whether a window
// is off by one: a grant is on the moment it starts, and off the moment it
// ends.
func TestGrantWindowIsHalfOpen(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	start, end := now, now.Add(time.Hour)
	g := grantRow("ledger-export", &start, &end)

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"a moment before it starts", start.Add(-time.Nanosecond), false},
		{"the instant it starts", start, true},
		{"midway", start.Add(30 * time.Minute), true},
		{"a moment before it ends", end.Add(-time.Nanosecond), true},
		{"the instant it ends", end, false},
		{"after it ends", end.Add(time.Nanosecond), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.Active(tc.when); got != tc.want {
				t.Fatalf("Active(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

func TestGrantWindowStates(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  FeatureGrantRecord
		want string
	}{
		{"no window", grantRow("k", nil, nil), GrantActive},
		{"starts later", grantRow("k", at(now, time.Hour), nil), GrantPending},
		{"already ended", grantRow("k", nil, at(now, -time.Hour)), GrantExpired},
		{"open now", grantRow("k", at(now, -time.Hour), at(now, time.Hour)), GrantActive},
		{"ends exactly now", grantRow("k", nil, &now), GrantExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.WindowState(now); got != tc.want {
				t.Fatalf("WindowState = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFoldGrantsReportsTheEarliestBoundary is the crux of the story. The
// boundary is what lets a grant expire to the second on a cache measured in
// minutes.
func TestFoldGrantsReportsTheEarliestBoundary(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	t.Run("no windows means no boundary", func(t *testing.T) {
		active, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", nil, nil), grantRow("b", nil, nil),
		}, now)
		if len(active) != 2 {
			t.Fatalf("active = %v, want both", active)
		}
		if !next.IsZero() {
			t.Fatalf("next = %v, want zero — nothing changes on its own", next)
		}
	})

	t.Run("a live grant's end is a boundary", func(t *testing.T) {
		active, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", nil, at(now, 2*time.Second)),
		}, now)
		if !active["a"] {
			t.Fatal("a live grant was folded away")
		}
		if !next.Equal(now.Add(2 * time.Second)) {
			t.Fatalf("next = %v, want the moment it ends", next)
		}
	})

	t.Run("a pending grant's start is a boundary", func(t *testing.T) {
		active, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", at(now, time.Minute), nil),
		}, now)
		if active["a"] {
			t.Fatal("a grant that has not started was counted")
		}
		if !next.Equal(now.Add(time.Minute)) {
			t.Fatalf("next = %v, want the moment it starts", next)
		}
	})

	t.Run("the earliest of many wins", func(t *testing.T) {
		_, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", nil, at(now, time.Hour)),
			grantRow("b", at(now, 5*time.Second), nil),
			grantRow("c", nil, at(now, time.Minute)),
		}, now)
		if !next.Equal(now.Add(5 * time.Second)) {
			t.Fatalf("next = %v, want the earliest boundary", next)
		}
	})

	t.Run("an expired grant contributes nothing", func(t *testing.T) {
		active, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", nil, at(now, -time.Hour)),
		}, now)
		if active["a"] {
			t.Fatal("an expired grant was counted as held")
		}
		if !next.IsZero() {
			t.Fatalf("next = %v, want zero — an expired grant will not change again", next)
		}
	})

	// A boundary at or before now would make the entry stale the instant it
	// was stored, and every evaluation would re-resolve forever.
	t.Run("a boundary is never at or before now", func(t *testing.T) {
		_, next := FoldGrants([]FeatureGrantRecord{
			grantRow("a", nil, &now),
			grantRow("b", &now, nil),
		}, now)
		if !next.IsZero() {
			t.Fatalf("next = %v, want zero — a set must never be born stale", next)
		}
	})

	// Two grants on one key: an expired personal grant must not cancel a live
	// role grant, and the dead one must not contribute a boundary.
	t.Run("one live grant survives an expired one on the same key", func(t *testing.T) {
		active, next := FoldGrants([]FeatureGrantRecord{
			{SubjectType: "agent", FeatureKey: "ledger-export", ValidThrough: at(now, -time.Hour)},
			{SubjectType: "role", FeatureKey: "ledger-export"},
		}, now)
		if !active["ledger-export"] {
			t.Fatal("an expired personal grant canceled a live role grant")
		}
		if !next.IsZero() {
			t.Fatalf("next = %v, want zero", next)
		}
	})
}
