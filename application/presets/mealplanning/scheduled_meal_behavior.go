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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"
)

// MaxOccurrences is the hard cap on how many MealOccurrence resources a
// single ScheduledMeal can generate. Exceeding this emits a warning message.
const MaxOccurrences = 52

// scheduledMealBehavior handles generate, regenerate, and cascade-delete
// for ScheduledMeal resources.
type scheduledMealBehavior struct {
	baseBehavior
}

// NewScheduledMealBehavior returns a stateless behavior instance.
func NewScheduledMealBehavior() *scheduledMealBehavior {
	return &scheduledMealBehavior{}
}

func (b *scheduledMealBehavior) AfterCreate(
	ctx context.Context, resource *entities.Resource,
) error {
	b.generateOccurrences(ctx, resource, false)
	return nil
}

func (b *scheduledMealBehavior) AfterUpdate(
	ctx context.Context, resource *entities.Resource,
) error {
	b.regenerateOccurrences(ctx, resource)
	return nil
}

func (b *scheduledMealBehavior) AfterDelete(
	ctx context.Context, resource *entities.Resource,
) error {
	b.cascadeDelete(ctx, resource)
	return nil
}

// generateOccurrences expands the schedule and creates MealOccurrence
// resources for every concrete date. If onlyFuture is true, only dates
// on or after today (UTC) are emitted. Errors during expansion or
// individual creates surface as user-facing warning messages.
func (b *scheduledMealBehavior) generateOccurrences(
	ctx context.Context, resource *entities.Resource, onlyFuture bool,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		addNilSvcWarning(ctx, "scheduled-meal generation")
		return
	}

	sm, err := extractFlatDataByID(resource, resource.GetID())
	if err != nil {
		if log != nil {
			log.Error(ctx, "scheduled-meal behavior: invalid data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	dates, capped, warnings, err := expandScheduleWithWarnings(sm, time.Now().UTC(), onlyFuture)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"scheduled-meal behavior: schedule expansion failed",
			fmt.Sprintf("Schedule expansion failed: %v — no occurrences generated", err),
			"scheduled_meal_expansion_error",
			"id", resource.GetID(), "error", err)
		return
	}

	emitScheduleWarnings(ctx, warnings, capped)

	mealType, _ := sm["mealType"].(string)
	servings := sm["servings"]

	failures := 0
	for _, d := range dates {
		occurrence := map[string]any{
			"date":          d.Format("2006-01-02"),
			"mealType":      mealType,
			"status":        "planned",
			"scheduledMeal": resource.GetID(),
		}
		if servings != nil {
			occurrence["servings"] = servings
		}
		data, mErr := json.Marshal(occurrence)
		if mErr != nil {
			if log != nil {
				log.Error(ctx, "scheduled-meal behavior: failed to marshal occurrence",
					"date", d.Format("2006-01-02"), "error", mErr)
			}
			failures++
			continue
		}
		if _, cErr := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: "meal-occurrence", Data: data,
		}); cErr != nil {
			if log != nil {
				log.Error(ctx, "scheduled-meal behavior: failed to create occurrence",
					"date", d.Format("2006-01-02"), "error", cErr)
			}
			failures++
		}
	}
	if failures > 0 {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"%d of %d occurrences failed to create; check the schedule",
				failures, len(dates)),
			Code: "scheduled_meal_occurrence_create_partial",
		})
	}
}

// regenerateOccurrences deletes future planned occurrences for the given
// scheduled meal and recreates them from the updated schedule. Past and
// cooked/skipped occurrences are preserved as historical records.
//
// Strategy: validate the new schedule FIRST (dry run expandSchedule). Only
// if expansion succeeds do we delete existing future planned occurrences
// and then create the new ones. This prevents destroying user data when
// the update contains a malformed recurrence rule.
func (b *scheduledMealBehavior) regenerateOccurrences(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		addNilSvcWarning(ctx, "scheduled-meal regeneration")
		return
	}

	sm, err := extractFlatDataByID(resource, resource.GetID())
	if err != nil {
		if log != nil {
			log.Error(ctx, "scheduled-meal behavior: invalid data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	now := time.Now().UTC()
	// Dry-run: validate the new schedule before deleting anything.
	newDates, capped, warnings, err := expandScheduleWithWarnings(sm, now, true)
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"scheduled-meal behavior: regenerate expansion failed",
			fmt.Sprintf("Schedule update rejected: %v — existing occurrences preserved", err),
			"scheduled_meal_regenerate_expansion_error",
			"id", resource.GetID(), "error", err)
		return
	}
	emitScheduleWarnings(ctx, warnings, capped)

	// List existing occurrences (propagate error instead of silent no-op).
	existing, listErr := b.listOccurrences(ctx, resource.GetID())
	if listErr != nil {
		addServiceErrorMessage(ctx, log,
			"scheduled-meal behavior: failed to list existing occurrences",
			"Failed to list existing occurrences; schedule not regenerated",
			"scheduled_meal_regenerate_list_error",
			"id", resource.GetID(), "error", listErr)
		return
	}

	today := now.Truncate(24 * time.Hour)
	preservedDates := map[string]bool{}
	deleteFailures := 0

	for _, occ := range existing {
		date, _ := occ["date"].(string)
		status, _ := occ["status"].(string)
		id, _ := occ["id"].(string)
		if id == "" {
			if log != nil {
				log.Warn(ctx, "scheduled-meal behavior: occurrence missing id")
			}
			continue
		}
		d, parseErr := time.Parse("2006-01-02", date)
		if parseErr != nil {
			if log != nil {
				log.Warn(ctx, "scheduled-meal behavior: invalid occurrence date",
					"id", id, "date", date)
			}
			continue
		}
		// Preserve past or non-planned occurrences.
		if d.Before(today) || status != "planned" {
			preservedDates[date] = true
			continue
		}
		if dErr := svc.Delete(ctx, application.DeleteResourceCommand{ID: id}); dErr != nil {
			if log != nil {
				log.Error(ctx, "scheduled-meal behavior: failed to delete future occurrence",
					"id", id, "error", dErr)
			}
			deleteFailures++
		}
	}

	if deleteFailures > 0 {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"%d existing occurrences could not be deleted; schedule may be inconsistent",
				deleteFailures),
			Code: "scheduled_meal_regenerate_delete_partial",
		})
	}

	// Create new occurrences, skipping any dates already preserved.
	b.createOccurrencesForDates(ctx, resource, sm, newDates, preservedDates)
}

// createOccurrencesForDates creates MealOccurrence resources for each date,
// skipping any date already covered by a preserved occurrence.
func (b *scheduledMealBehavior) createOccurrencesForDates(
	ctx context.Context, resource *entities.Resource,
	sm map[string]any, dates []time.Time, preserved map[string]bool,
) {
	svc := b.svc()
	log := b.log()
	mealType, _ := sm["mealType"].(string)
	servings := sm["servings"]
	failures := 0

	for _, d := range dates {
		dateStr := d.Format("2006-01-02")
		if preserved[dateStr] {
			continue
		}
		occurrence := map[string]any{
			"date":          dateStr,
			"mealType":      mealType,
			"status":        "planned",
			"scheduledMeal": resource.GetID(),
		}
		if servings != nil {
			occurrence["servings"] = servings
		}
		data, mErr := json.Marshal(occurrence)
		if mErr != nil {
			if log != nil {
				log.Error(ctx, "scheduled-meal behavior: failed to marshal occurrence",
					"date", dateStr, "error", mErr)
			}
			failures++
			continue
		}
		if _, cErr := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: "meal-occurrence", Data: data,
		}); cErr != nil {
			if log != nil {
				log.Error(ctx, "scheduled-meal behavior: failed to create occurrence",
					"date", dateStr, "error", cErr)
			}
			failures++
		}
	}
	if failures > 0 {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"%d occurrences failed to create during schedule regeneration",
				failures),
			Code: "scheduled_meal_regenerate_create_partial",
		})
	}
}

// cascadeDelete removes future planned occurrences when the scheduled meal
// is deleted. Past, cooked, and skipped occurrences are preserved as history.
func (b *scheduledMealBehavior) cascadeDelete(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		addNilSvcWarning(ctx, "scheduled-meal cascade delete")
		return
	}

	existing, err := b.listOccurrences(ctx, resource.GetID())
	if err != nil {
		addServiceErrorMessage(ctx, log,
			"scheduled-meal behavior: cascade delete list failed",
			"Failed to list occurrences; some may be orphaned",
			"scheduled_meal_cascade_list_error",
			"id", resource.GetID(), "error", err)
		return
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)

	for _, occ := range existing {
		date, _ := occ["date"].(string)
		status, _ := occ["status"].(string)
		id, _ := occ["id"].(string)
		if id == "" {
			continue
		}
		d, parseErr := time.Parse("2006-01-02", date)
		if parseErr != nil {
			continue
		}
		if d.Before(today) || status != "planned" {
			continue
		}
		if dErr := svc.Delete(ctx, application.DeleteResourceCommand{ID: id}); dErr != nil && log != nil {
			log.Error(ctx, "scheduled-meal behavior: cascade delete failed",
				"id", id, "error", dErr)
		}
	}
}

// listOccurrences returns all MealOccurrence resources linked to a
// scheduled meal. Errors are propagated so callers can react.
func (b *scheduledMealBehavior) listOccurrences(
	ctx context.Context, scheduledMealID string,
) ([]map[string]any, error) {
	svc := b.svc()
	if svc == nil {
		return nil, errors.New("resource service not injected")
	}
	filters := []repositories.FilterCondition{
		{Field: "scheduledMeal", Operator: "eq", Value: scheduledMealID},
	}
	// Page through all occurrences. The 52-occurrence cap keeps the typical
	// count well below 100, but historical preserved occurrences can accumulate
	// across repeated regenerations.
	const pageSize = 100
	var all []map[string]any
	cursor := ""
	for {
		resp, err := svc.ListFlatWithFilters(
			ctx, "meal-occurrence", filters, cursor, pageSize,
			repositories.SortOptions{},
		)
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		if resp.Cursor == "" || len(resp.Data) < pageSize {
			break
		}
		cursor = resp.Cursor
	}
	return all, nil
}

// emitScheduleWarnings classifies each warning string and adds an
// appropriate user-facing message with a dedicated code.
func emitScheduleWarnings(
	ctx context.Context, warnings []string, capped bool,
) {
	for _, w := range warnings {
		text, code := classifyScheduleWarning(w)
		entities.AddMessage(ctx, entities.Message{
			Type: "warning", Text: text, Code: code,
		})
	}
	if capped {
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf(
				"Showing first %d occurrences — edit the schedule to extend",
				MaxOccurrences),
			Code: "scheduled_meal_occurrence_cap_reached",
		})
	}
}

// classifyScheduleWarning maps a raw warning string to a user-facing
// message text and a stable machine-readable code.
func classifyScheduleWarning(w string) (string, string) {
	lower := strings.ToLower(w)
	switch {
	case strings.Contains(lower, "max iteration") ||
		strings.Contains(lower, "truncat"):
		return "Schedule expansion was truncated: " + w,
			"scheduled_meal_expansion_truncated"
	default:
		return "Schedule contains invalid exceptDate entry: " + w,
			"scheduled_meal_invalid_except_date"
	}
}

// -- recurrence walker -------------------------------------------------------

// expandSchedule walks the recurrence rule and returns all concrete dates.
// The boolean return indicates whether the MaxOccurrences cap was hit.
//
// When onlyFuture is true, dates strictly before `today` (day granularity)
// are excluded — today IS included. regenerateOccurrences additionally
// preserves dates that have existing non-planned occurrences to avoid
// duplicates.
func expandSchedule(
	sm map[string]any, now time.Time, onlyFuture bool,
) ([]time.Time, bool, error) {
	startStr, _ := sm["startDate"].(string)
	if startStr == "" {
		return nil, false, fmt.Errorf("startDate is required")
	}
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil, false, fmt.Errorf("invalid startDate %q: %w", startStr, err)
	}

	endStr, _ := sm["endDate"].(string)
	var end *time.Time
	if endStr != "" {
		e, pErr := time.Parse("2006-01-02", endStr)
		if pErr == nil {
			end = &e
		} else {
			return nil, false, fmt.Errorf("invalid endDate %q: %w", endStr, pErr)
		}
	}

	repeatFrequency, _ := sm["repeatFrequency"].(string)
	byDay := parseStringArray(sm["byDay"])
	byMonth := parseIntArray(sm["byMonth"])
	byMonthDay := parseIntArray(sm["byMonthDay"])
	exceptDates, exceptWarnings := parseDateArrayWithWarnings(sm["exceptDate"])
	repeatCount := parseInt(sm["repeatCount"])

	today := now.Truncate(24 * time.Hour)

	// Non-recurring: single occurrence on startDate.
	if repeatFrequency == "" {
		if onlyFuture && start.Before(today) {
			return nil, false, nil
		}
		if isExcepted(start, exceptDates) {
			return nil, false, nil
		}
		return []time.Time{start}, false, nil
	}

	step, stepErr := parseISODuration(repeatFrequency)
	if stepErr != nil {
		return nil, false, stepErr
	}

	dates, capped, _ := walkSchedule(walkParams{
		start: start, end: end, today: today,
		step: step, byDay: byDay, byMonth: byMonth, byMonthDay: byMonthDay,
		exceptDates: exceptDates, repeatCount: repeatCount, onlyFuture: onlyFuture,
	})
	_ = exceptWarnings // kept for API symmetry with expandScheduleWithWarnings
	return dates, capped, nil
}

// expandScheduleWithWarnings returns the same as expandSchedule plus any
// parse warnings discovered in exceptDate entries or iteration truncation.
// Behaviors use this to surface user-facing warning messages for bad input.
func expandScheduleWithWarnings(
	sm map[string]any, now time.Time, onlyFuture bool,
) ([]time.Time, bool, []string, error) {
	// Parse upfront so we can report bad input and pass clean data to the walker.
	exceptDates, exceptWarnings := parseDateArrayWithWarnings(sm["exceptDate"])

	startStr, _ := sm["startDate"].(string)
	if startStr == "" {
		return nil, false, exceptWarnings, fmt.Errorf("startDate is required")
	}
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil, false, exceptWarnings,
			fmt.Errorf("invalid startDate %q: %w", startStr, err)
	}

	endStr, _ := sm["endDate"].(string)
	var end *time.Time
	if endStr != "" {
		e, pErr := time.Parse("2006-01-02", endStr)
		if pErr != nil {
			return nil, false, exceptWarnings,
				fmt.Errorf("invalid endDate %q: %w", endStr, pErr)
		}
		end = &e
	}

	repeatFrequency, _ := sm["repeatFrequency"].(string)
	today := now.Truncate(24 * time.Hour)

	if repeatFrequency == "" {
		if onlyFuture && start.Before(today) {
			return nil, false, exceptWarnings, nil
		}
		if isExcepted(start, exceptDates) {
			return nil, false, exceptWarnings, nil
		}
		return []time.Time{start}, false, exceptWarnings, nil
	}

	step, stepErr := parseISODuration(repeatFrequency)
	if stepErr != nil {
		return nil, false, exceptWarnings, stepErr
	}

	dates, capped, truncated := walkSchedule(walkParams{
		start: start, end: end, today: today, step: step,
		byDay:       parseStringArray(sm["byDay"]),
		byMonth:     parseIntArray(sm["byMonth"]),
		byMonthDay:  parseIntArray(sm["byMonthDay"]),
		exceptDates: exceptDates,
		repeatCount: parseInt(sm["repeatCount"]),
		onlyFuture:  onlyFuture,
	})
	if truncated {
		exceptWarnings = append(exceptWarnings,
			"schedule walk exceeded max iterations; results may be incomplete")
	}
	return dates, capped, exceptWarnings, nil
}

type walkParams struct {
	start, today time.Time
	end          *time.Time
	step         func(time.Time) time.Time
	byDay        []string
	byMonth      []int
	byMonthDay   []int
	exceptDates  []time.Time
	repeatCount  int
	onlyFuture   bool
}

// maxWalkIterations caps the recurrence walker's iteration count as a
// safety net against runaway schedules. When exceeded, walkSchedule returns
// truncated=true so the caller can surface a warning.
const maxWalkIterations = 10000

// walkSchedule is the inner recurrence iteration loop, kept separate from
// expandSchedule to satisfy function length limits. Returns the emitted
// dates plus two boolean flags: capped (MaxOccurrences reached) and
// truncated (maxWalkIterations exhausted before the schedule naturally ended).
func walkSchedule(p walkParams) ([]time.Time, bool, bool) {
	var dates []time.Time
	current := p.start
	capped := false
	truncated := true // assume truncation until we exit via a natural break

	// Fast-forward: when onlyFuture is true and start is well before today,
	// skip the step()-by-step walk through the past. We advance by step()
	// without recording dates until current is within one step of today.
	// This preserves byDay/byMonth/byMonthDay filters (they're checked at
	// emission time below) while avoiding wasted iterations.
	if p.onlyFuture {
		for i := 0; i < maxWalkIterations; i++ {
			next := p.step(current)
			if !next.After(current) {
				break
			}
			if !next.Before(p.today) {
				break
			}
			current = next
		}
	}

	for i := 0; i < maxWalkIterations; i++ {
		if p.end != nil && current.After(*p.end) {
			truncated = false
			break
		}
		if p.repeatCount > 0 && len(dates) >= p.repeatCount {
			truncated = false
			break
		}
		if len(dates) >= MaxOccurrences {
			capped = true
			truncated = false
			break
		}

		if matchesFilters(current, p.byDay, p.byMonth, p.byMonthDay) &&
			!isExcepted(current, p.exceptDates) {
			if !p.onlyFuture || !current.Before(p.today) {
				dates = append(dates, current)
			}
		}

		next := p.step(current)
		if !next.After(current) {
			truncated = false
			break
		}
		current = next
	}
	return dates, capped, truncated
}

// parseISODuration parses an ISO 8601 duration of the form P<N>D, P<N>W,
// P<N>M, or P<N>Y and returns a function that advances a date by that amount.
// Composite durations (e.g. P1Y6M) are not supported in this minimal parser.
func parseISODuration(s string) (func(time.Time) time.Time, error) {
	if !strings.HasPrefix(s, "P") || len(s) < 3 {
		return nil, fmt.Errorf("invalid ISO 8601 duration %q (expected P<N>D/W/M/Y)", s)
	}
	body := s[1:]
	unit := body[len(body)-1:]
	numStr := body[:len(body)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("invalid duration value in %q", s)
	}
	switch unit {
	case "D":
		return func(t time.Time) time.Time { return t.AddDate(0, 0, n) }, nil
	case "W":
		return func(t time.Time) time.Time { return t.AddDate(0, 0, 7*n) }, nil
	case "M":
		return func(t time.Time) time.Time { return t.AddDate(0, n, 0) }, nil
	case "Y":
		return func(t time.Time) time.Time { return t.AddDate(n, 0, 0) }, nil
	default:
		return nil, fmt.Errorf("unsupported duration unit %q in %q (use D/W/M/Y)", unit, s)
	}
}

// matchesFilters returns true if the date satisfies all byDay/byMonth/byMonthDay
// constraints. Empty filters are treated as "no constraint".
func matchesFilters(d time.Time, byDay []string, byMonth, byMonthDay []int) bool {
	if len(byDay) > 0 && !containsString(byDay, d.Weekday().String()) {
		return false
	}
	if len(byMonth) > 0 && !containsInt(byMonth, int(d.Month())) {
		return false
	}
	if len(byMonthDay) > 0 && !containsInt(byMonthDay, d.Day()) {
		return false
	}
	return true
}

func isExcepted(d time.Time, exceptions []time.Time) bool {
	for _, e := range exceptions {
		if e.Year() == d.Year() && e.Month() == d.Month() && e.Day() == d.Day() {
			return true
		}
	}
	return false
}

// -- small helpers -----------------------------------------------------------

func parseStringArray(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseIntArray(v any) []int {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(arr))
	for _, x := range arr {
		if n := parseInt(x); n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// parseDateArrayWithWarnings parses an array of date strings and returns
// both the successfully parsed dates and any parse warnings encountered.
// The warnings should be surfaced to the caller via entities.AddMessage.
func parseDateArrayWithWarnings(v any) ([]time.Time, []string) {
	arr, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]time.Time, 0, len(arr))
	var warnings []string
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%v is not a string", x))
			continue
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			warnings = append(warnings,
				fmt.Sprintf("%q is not a valid date (YYYY-MM-DD)", s))
			continue
		}
		out = append(out, t)
	}
	return out, warnings
}

func parseInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

func containsString(arr []string, s string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}

func containsInt(arr []int, n int) bool {
	for _, a := range arr {
		if a == n {
			return true
		}
	}
	return false
}
