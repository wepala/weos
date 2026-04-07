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
	// Regenerate: preserve past/cooked/skipped, recreate future planned.
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
// resources for every concrete date. If preserveFuture is true, dates in
// the past or >= today with non-planned status are skipped (used by regenerate).
func (b *scheduledMealBehavior) generateOccurrences(
	ctx context.Context, resource *entities.Resource, onlyFuture bool,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return
	}

	sm, err := extractFlatData(resource)
	if err != nil {
		if log != nil {
			log.Error(ctx, "scheduled-meal behavior: invalid data",
				"id", resource.GetID(), "error", err)
		}
		return
	}

	dates, capped, err := expandSchedule(sm, time.Now(), onlyFuture)
	if err != nil {
		if log != nil {
			log.Error(ctx, "scheduled-meal behavior: schedule expansion failed",
				"id", resource.GetID(), "error", err)
		}
		return
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

	mealType, _ := sm["mealType"].(string)
	servings := sm["servings"]

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
			continue
		}
		if _, cErr := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: "meal-occurrence", Data: data,
		}); cErr != nil && log != nil {
			log.Error(ctx, "scheduled-meal behavior: failed to create occurrence",
				"date", d.Format("2006-01-02"), "error", cErr)
		}
	}
}

// regenerateOccurrences deletes future planned occurrences for the given
// scheduled meal and recreates them from the updated schedule. Past and
// cooked/skipped occurrences are preserved as historical records.
func (b *scheduledMealBehavior) regenerateOccurrences(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return
	}

	// Load existing occurrences linked to this ScheduledMeal.
	existing := b.listOccurrences(ctx, resource.GetID())
	today := time.Now().Truncate(24 * time.Hour)

	for _, occ := range existing {
		date, _ := occ["date"].(string)
		status, _ := occ["status"].(string)
		id, _ := occ["id"].(string)
		if id == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		// Preserve past or non-planned occurrences.
		if d.Before(today) || status != "planned" {
			continue
		}
		if dErr := svc.Delete(ctx, application.DeleteResourceCommand{ID: id}); dErr != nil && log != nil {
			log.Error(ctx, "scheduled-meal behavior: failed to delete future occurrence",
				"id", id, "error", dErr)
		}
	}

	// Re-walk the schedule and generate only future occurrences.
	b.generateOccurrences(ctx, resource, true)
}

// cascadeDelete removes future planned occurrences when the scheduled meal
// is deleted. Past, cooked, and skipped occurrences are preserved as history.
func (b *scheduledMealBehavior) cascadeDelete(
	ctx context.Context, resource *entities.Resource,
) {
	svc := b.svc()
	log := b.log()
	if svc == nil {
		return
	}

	existing := b.listOccurrences(ctx, resource.GetID())
	today := time.Now().Truncate(24 * time.Hour)

	for _, occ := range existing {
		date, _ := occ["date"].(string)
		status, _ := occ["status"].(string)
		id, _ := occ["id"].(string)
		if id == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
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

// listOccurrences returns all MealOccurrence resources linked to a scheduled meal.
func (b *scheduledMealBehavior) listOccurrences(
	ctx context.Context, scheduledMealID string,
) []map[string]any {
	svc := b.svc()
	if svc == nil {
		return nil
	}
	filters := []repositories.FilterCondition{
		{Field: "scheduledMeal", Operator: "eq", Value: scheduledMealID},
	}
	resp, err := svc.ListFlatWithFilters(
		ctx, "meal-occurrence", filters, "", 500, repositories.SortOptions{},
	)
	if err != nil {
		return nil
	}
	return resp.Data
}

// -- recurrence walker -------------------------------------------------------

// expandSchedule walks the recurrence rule and returns all concrete dates.
// The boolean return indicates whether the MaxOccurrences cap was hit.
func expandSchedule(
	sm map[string]any, now time.Time, onlyFuture bool,
) ([]time.Time, bool, error) {
	startStr, _ := sm["startDate"].(string)
	if startStr == "" {
		return nil, false, fmt.Errorf("startDate is required")
	}
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil, false, fmt.Errorf("invalid startDate: %w", err)
	}

	// Parse optional constraints.
	endStr, _ := sm["endDate"].(string)
	var end *time.Time
	if endStr != "" {
		e, pErr := time.Parse("2006-01-02", endStr)
		if pErr == nil {
			end = &e
		}
	}

	repeatFrequency, _ := sm["repeatFrequency"].(string)
	byDay := parseStringArray(sm["byDay"])
	byMonth := parseIntArray(sm["byMonth"])
	byMonthDay := parseIntArray(sm["byMonthDay"])
	exceptDates := parseDateArray(sm["exceptDate"])
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

	var dates []time.Time
	current := start
	capped := false
	maxIterations := 10000 // safety against infinite loops

	for i := 0; i < maxIterations; i++ {
		if end != nil && current.After(*end) {
			break
		}
		if repeatCount > 0 && len(dates) >= repeatCount {
			break
		}
		if len(dates) >= MaxOccurrences {
			capped = true
			break
		}

		if matchesFilters(current, byDay, byMonth, byMonthDay) &&
			!isExcepted(current, exceptDates) {
			if !onlyFuture || !current.Before(today) {
				dates = append(dates, current)
			}
		}

		next := step(current)
		if !next.After(current) {
			// Safety: step must advance time.
			break
		}
		current = next
	}

	return dates, capped, nil
}

// parseISODuration parses an ISO 8601 duration of the form P<N>D, P<N>W,
// P<N>M, or P<N>Y and returns a function that advances a date by that amount.
// Composite durations (e.g. P1Y6M) are not supported in this minimal parser.
func parseISODuration(s string) (func(time.Time) time.Time, error) {
	if !strings.HasPrefix(s, "P") || len(s) < 3 {
		return nil, fmt.Errorf("invalid ISO 8601 duration: %q", s)
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
		return nil, fmt.Errorf("unsupported duration unit %q in %q", unit, s)
	}
}

// matchesFilters returns true if the date satisfies all byDay/byMonth/byMonthDay
// constraints. Empty filters are treated as "no constraint".
func matchesFilters(d time.Time, byDay []string, byMonth, byMonthDay []int) bool {
	if len(byDay) > 0 {
		if !containsString(byDay, d.Weekday().String()) {
			return false
		}
	}
	if len(byMonth) > 0 {
		if !containsInt(byMonth, int(d.Month())) {
			return false
		}
	}
	if len(byMonthDay) > 0 {
		if !containsInt(byMonthDay, d.Day()) {
			return false
		}
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

func parseDateArray(v any) []time.Time {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]time.Time, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			if t, err := time.Parse("2006-01-02", s); err == nil {
				out = append(out, t)
			}
		}
	}
	return out
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
