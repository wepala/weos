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
	"errors"
	"testing"
)

// A planner is what a dry run builds over a read-only connection. It has no
// event store, so it must refuse to apply a plan rather than reach one.
func TestOwnerBackfillPlanner_RefusesToApply(t *testing.T) {
	planner := NewOwnerBackfillPlanner(nil, nil)
	plan := AssignOwnerReport{
		AccountID: "urn:account:a", CreatedBy: "urn:agent:b",
		Resources: []AssignOwnerResource{{ID: "urn:task:c", TypeSlug: "task"}},
		Types:     map[string]AssignOwnerTypeCount{"task": {Ownerless: 1}},
	}
	report, err := planner.ApplyAssignOwner(context.Background(), plan)
	if !errors.Is(err, ErrOwnerBackfillReadOnly) {
		t.Fatalf("ApplyAssignOwner on a planner: err = %v, want ErrOwnerBackfillReadOnly", err)
	}
	if report.Assigned() != 0 {
		t.Errorf("a planner reports %d assigned", report.Assigned())
	}
}
