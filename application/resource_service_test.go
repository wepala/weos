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
	"strings"
	"testing"
)

// TestEnterResourceCall_IncrementsBelowLimit verifies the happy path: depths
// 0..maxBehaviorRecursionDepth-1 succeed and each returned context carries
// depth+1.
func TestEnterResourceCall_IncrementsBelowLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for i := range maxBehaviorRecursionDepth {
		next, err := enterResourceCall(ctx)
		if err != nil {
			t.Fatalf("depth %d: unexpected error: %v", i, err)
		}
		got, _ := next.Value(behaviorDepthKey{}).(int)
		if got != i+1 {
			t.Errorf("depth %d: next ctx depth = %d, want %d", i, got, i+1)
		}
		ctx = next
	}
}

// TestEnterResourceCall_RejectsAtLimit verifies that the guard refuses once
// the depth is already at maxBehaviorRecursionDepth — i.e. the call that
// would push it to depth+1 beyond the max is the one that fails.
func TestEnterResourceCall_RejectsAtLimit(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth)
	gotCtx, err := enterResourceCall(ctx)
	if err == nil {
		t.Fatal("expected error at limit, got nil")
	}
	if !strings.Contains(err.Error(), "recursion depth") {
		t.Errorf("error = %q, want contains 'recursion depth'", err.Error())
	}
	// On error the original ctx must be returned (not nil) so callers that
	// forget to guard the error don't null-deref on subsequent ctx use.
	if gotCtx != ctx {
		t.Errorf("error path should return the original ctx, got a different value")
	}
}

// TestEnterResourceCall_BoundaryOffByOne pins the exact edge: depth 7
// (maxBehaviorRecursionDepth-1) must succeed, depth 8 must fail. This is the
// most likely place for an off-by-one regression.
func TestEnterResourceCall_BoundaryOffByOne(t *testing.T) {
	t.Parallel()
	justUnder := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth-1)
	if _, err := enterResourceCall(justUnder); err != nil {
		t.Errorf("depth %d should succeed (last legal level), got %v", maxBehaviorRecursionDepth-1, err)
	}
	atLimit := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth)
	if _, err := enterResourceCall(atLimit); err == nil {
		t.Errorf("depth %d should fail (exceeds max), got nil", maxBehaviorRecursionDepth)
	}
}

// TestEnterResourceCall_SiblingsDoNotInflateCounter proves the sibling-write
// accounting. Two calls derived from the same parent ctx each produce a
// child at depth parent+1 — not parent+1 and parent+2. This works because
// context values are immutable: the increment lives only on the returned
// child ctx. A regression that switched to a mutable counter would falsely
// trip the guard on legitimate fan-out (e.g. an enrollment behavior creating
// N attendance records).
func TestEnterResourceCall_SiblingsDoNotInflateCounter(t *testing.T) {
	t.Parallel()
	parent := context.WithValue(context.Background(), behaviorDepthKey{}, 3)

	sibling1, err := enterResourceCall(parent)
	if err != nil {
		t.Fatalf("first sibling errored: %v", err)
	}
	sibling2, err := enterResourceCall(parent)
	if err != nil {
		t.Fatalf("second sibling errored: %v", err)
	}

	got1, _ := sibling1.Value(behaviorDepthKey{}).(int)
	got2, _ := sibling2.Value(behaviorDepthKey{}).(int)
	if got1 != 4 || got2 != 4 {
		t.Errorf("sibling depths = (%d, %d), want (4, 4); parent ctx leaked a mutation", got1, got2)
	}

	// Parent itself must be unchanged after its children were derived.
	parentDepth, _ := parent.Value(behaviorDepthKey{}).(int)
	if parentDepth != 3 {
		t.Errorf("parent depth = %d, want 3 (context value should be immutable)", parentDepth)
	}
}

// TestEnterResourceCall_MissingKeyStartsAtZero verifies the zero-value path:
// a ctx with no depth key must be treated as depth 0 and produce a child at
// depth 1, not panic on the type assertion.
func TestEnterResourceCall_MissingKeyStartsAtZero(t *testing.T) {
	t.Parallel()
	next, err := enterResourceCall(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on fresh ctx: %v", err)
	}
	got, _ := next.Value(behaviorDepthKey{}).(int)
	if got != 1 {
		t.Errorf("fresh ctx child depth = %d, want 1", got)
	}
}

// TestResourceService_CreateEnforcesDepthGuard asserts that Create calls
// enterResourceCall before any repository work — a ctx seeded at the limit
// must be rejected without touching the stub repos (which would panic on
// method invocation since they only embed the interface).
func TestResourceService_CreateEnforcesDepthGuard(t *testing.T) {
	t.Parallel()
	svc := &resourceService{
		repo:     &stubResourceRepo{},
		typeRepo: &stubTypeRepo{},
		logger:   noopLogger{},
	}
	ctx := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth)
	_, err := svc.Create(ctx, CreateResourceCommand{TypeSlug: "anything"})
	if err == nil {
		t.Fatal("expected depth-limit error from Create, got nil")
	}
	if !strings.Contains(err.Error(), "recursion depth") {
		t.Errorf("Create error = %q, want contains 'recursion depth' (guard did not fire)", err.Error())
	}
}

// TestResourceService_UpdateEnforcesDepthGuard — symmetric check for Update.
func TestResourceService_UpdateEnforcesDepthGuard(t *testing.T) {
	t.Parallel()
	svc := &resourceService{
		repo:     &stubResourceRepo{},
		typeRepo: &stubTypeRepo{},
		logger:   noopLogger{},
	}
	ctx := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth)
	_, err := svc.Update(ctx, UpdateResourceCommand{ID: "anything"})
	if err == nil {
		t.Fatal("expected depth-limit error from Update, got nil")
	}
	if !strings.Contains(err.Error(), "recursion depth") {
		t.Errorf("Update error = %q, want contains 'recursion depth' (guard did not fire)", err.Error())
	}
}

// TestResourceService_DeleteEnforcesDepthGuard — symmetric check for Delete.
func TestResourceService_DeleteEnforcesDepthGuard(t *testing.T) {
	t.Parallel()
	svc := &resourceService{
		repo:     &stubResourceRepo{},
		typeRepo: &stubTypeRepo{},
		logger:   noopLogger{},
	}
	ctx := context.WithValue(context.Background(), behaviorDepthKey{}, maxBehaviorRecursionDepth)
	err := svc.Delete(ctx, DeleteResourceCommand{ID: "anything"})
	if err == nil {
		t.Fatal("expected depth-limit error from Delete, got nil")
	}
	if !strings.Contains(err.Error(), "recursion depth") {
		t.Errorf("Delete error = %q, want contains 'recursion depth' (guard did not fire)", err.Error())
	}
}
