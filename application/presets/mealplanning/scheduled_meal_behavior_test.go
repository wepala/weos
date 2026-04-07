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

package mealplanning

import (
	"testing"
	"time"
)

// fixed time "now" used by all tests that rely on onlyFuture filtering.
var testNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestExpandSchedule_SingleNonRecurring(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate": "2026-04-15",
	}
	dates, capped, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capped {
		t.Fatal("single occurrence should not hit cap")
	}
	if len(dates) != 1 {
		t.Fatalf("expected 1 date, got %d", len(dates))
	}
	if dates[0].Format("2006-01-02") != "2026-04-15" {
		t.Fatalf("unexpected date: %v", dates[0])
	}
}

func TestExpandSchedule_WeeklyForOneMonth(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-04-06",
		"endDate":         "2026-04-30",
		"repeatFrequency": "P1W",
	}
	dates, _, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Apr 6, 13, 20, 27 = 4 Mondays in range.
	if len(dates) != 4 {
		t.Fatalf("expected 4 dates, got %d: %v", len(dates), dates)
	}
}

func TestExpandSchedule_RepeatCount(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-04-06",
		"repeatFrequency": "P1D",
		"repeatCount":     float64(5),
	}
	dates, _, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dates) != 5 {
		t.Fatalf("expected 5 dates, got %d", len(dates))
	}
}

func TestExpandSchedule_CapHit(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-01-01",
		"repeatFrequency": "P1D",
		"endDate":         "2027-12-31", // would produce ~700 days
	}
	dates, capped, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capped {
		t.Fatal("expected cap to be hit")
	}
	if len(dates) != MaxOccurrences {
		t.Fatalf("expected %d dates, got %d", MaxOccurrences, len(dates))
	}
}

func TestExpandSchedule_ByDay(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-04-06", // Monday
		"endDate":         "2026-04-19",
		"repeatFrequency": "P1D",
		"byDay":           []any{"Monday", "Wednesday", "Friday"},
	}
	dates, _, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mon Apr 6, Wed 8, Fri 10, Mon 13, Wed 15, Fri 17 = 6 dates (Sun 19 excluded)
	if len(dates) != 6 {
		t.Fatalf("expected 6 dates, got %d: %v", len(dates), dates)
	}
	for _, d := range dates {
		wd := d.Weekday().String()
		if wd != "Monday" && wd != "Wednesday" && wd != "Friday" {
			t.Fatalf("unexpected weekday %s for date %v", wd, d)
		}
	}
}

func TestExpandSchedule_ExceptDate(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-04-06",
		"repeatFrequency": "P1D",
		"repeatCount":     float64(5),
		"exceptDate":      []any{"2026-04-08", "2026-04-10"},
	}
	dates, _, err := expandSchedule(sm, testNow, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Walk 5 days starting Apr 6, skipping 8 and 10 → Apr 6, 7, 9 remain (repeatCount caps walk at 5 iterations).
	// Actually: walk counts by repeatCount of emitted + skipped? The impl counts
	// emitted occurrences, so we get Apr 6, 7, 9, 11, 12 (5 emits, skipping 8, 10).
	// But we iterate max 10000 times. repeatCount applies to len(dates), so we'll
	// get 5 dates total.
	if len(dates) != 5 {
		t.Fatalf("expected 5 dates, got %d: %v", len(dates), dates)
	}
	for _, d := range dates {
		s := d.Format("2006-01-02")
		if s == "2026-04-08" || s == "2026-04-10" {
			t.Fatalf("date %s should have been excepted", s)
		}
	}
}

func TestExpandSchedule_OnlyFuture(t *testing.T) {
	t.Parallel()
	// now = 2026-01-01, schedule starts 2025-12-25 repeating daily.
	sm := map[string]any{
		"startDate":       "2025-12-25",
		"repeatFrequency": "P1D",
		"repeatCount":     float64(14),
	}
	dates, _, err := expandSchedule(sm, testNow, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, d := range dates {
		if d.Before(testNow.Truncate(24 * time.Hour)) {
			t.Fatalf("got past date %v in onlyFuture mode", d)
		}
	}
}

func TestExpandSchedule_MissingStartDate(t *testing.T) {
	t.Parallel()
	sm := map[string]any{}
	_, _, err := expandSchedule(sm, testNow, false)
	if err == nil {
		t.Fatal("expected error for missing startDate")
	}
}

func TestExpandSchedule_InvalidDuration(t *testing.T) {
	t.Parallel()
	sm := map[string]any{
		"startDate":       "2026-04-06",
		"repeatFrequency": "BAD",
	}
	_, _, err := expandSchedule(sm, testNow, false)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParseISODuration_Units(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input    string
		startStr string
		wantStr  string
	}{
		{"P1D", "2026-04-06", "2026-04-07"},
		{"P2D", "2026-04-06", "2026-04-08"},
		{"P1W", "2026-04-06", "2026-04-13"},
		{"P1M", "2026-04-06", "2026-05-06"},
		{"P1Y", "2026-04-06", "2027-04-06"},
	}
	for _, c := range cases {
		step, err := parseISODuration(c.input)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.input, err)
		}
		start, _ := time.Parse("2006-01-02", c.startStr)
		got := step(start).Format("2006-01-02")
		if got != c.wantStr {
			t.Fatalf("%s: got %s, want %s", c.input, got, c.wantStr)
		}
	}
}

func TestMatchesFilters_ByMonthDay(t *testing.T) {
	t.Parallel()
	d := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	if !matchesFilters(d, nil, nil, []int{15}) {
		t.Fatal("expected day 15 to match")
	}
	if matchesFilters(d, nil, nil, []int{1, 16}) {
		t.Fatal("expected day 15 to NOT match")
	}
}
