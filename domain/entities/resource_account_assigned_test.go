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

package entities

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func ownerlessResource(t *testing.T, id string) *Resource {
	t.Helper()
	r, err := new(Resource).With(id, "statement", json.RawMessage(`{"name":"May"}`), "urn:agent:owner", "")
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	return r
}

func TestResource_AssignAccount_SetsTheOwnerOfAnOwnerlessResource(t *testing.T) {
	t.Parallel()
	r := ownerlessResource(t, "urn:statement:a")

	if err := r.AssignAccount("acct-1"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if r.AccountID() != "acct-1" {
		t.Fatalf("account = %q, want acct-1", r.AccountID())
	}
	events := r.GetUncommittedEvents()
	last := events[len(events)-1]
	if last.EventType != "Resource.AccountAssigned" {
		t.Fatalf("last event = %q, want Resource.AccountAssigned", last.EventType)
	}
	payload, ok := last.Payload.(ResourceAccountAssigned)
	if !ok {
		t.Fatalf("payload type = %T, want ResourceAccountAssigned", last.Payload)
	}
	if payload.AccountID != "acct-1" {
		t.Fatalf("payload account = %q, want acct-1", payload.AccountID)
	}
}

func TestResource_AssignAccount_RefusesAResourceThatAlreadyHasAnOwner(t *testing.T) {
	t.Parallel()
	r, err := new(Resource).With("urn:statement:b", "statement", json.RawMessage(`{"name":"May"}`),
		"urn:agent:owner", "acct-1")
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	before := len(r.GetUncommittedEvents())

	err = r.AssignAccount("acct-2")
	if !errors.Is(err, ErrResourceAlreadyOwned) {
		t.Fatalf("err = %v, want ErrResourceAlreadyOwned", err)
	}
	if r.AccountID() != "acct-1" {
		t.Fatalf("account = %q, want the original acct-1", r.AccountID())
	}
	if got := len(r.GetUncommittedEvents()); got != before {
		t.Fatalf("a refused assignment recorded %d event(s)", got-before)
	}

	// The same account again is refused too: an owner is set, so there is
	// nothing to assign.
	if err := r.AssignAccount("acct-1"); !errors.Is(err, ErrResourceAlreadyOwned) {
		t.Fatalf("re-assigning the same account: err = %v, want ErrResourceAlreadyOwned", err)
	}
}

func TestResource_AssignAccount_RefusesAnEmptyAccount(t *testing.T) {
	t.Parallel()
	r := ownerlessResource(t, "urn:statement:c")
	if err := r.AssignAccount(""); err == nil {
		t.Fatal("assigning an empty account succeeded")
	}
	if r.AccountID() != "" {
		t.Fatalf("account = %q, want empty", r.AccountID())
	}
}

func TestResource_ApplyEvent_AccountAssignedSetsTheOwnerOnReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := &Resource{}
	if err := r.Restore("urn:statement:d", "statement", "active",
		json.RawMessage(`{"name":"May"}`), "urn:agent:owner", "", time.Now(), 3); err != nil {
		t.Fatalf("restore: %v", err)
	}
	ev := newTestEnvelope(ResourceAccountAssigned{AccountID: "acct-1", Timestamp: time.Now()},
		"urn:statement:d", "Resource.AccountAssigned", 4)
	if err := r.ApplyEvent(ctx, ev); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if r.AccountID() != "acct-1" {
		t.Fatalf("account = %q, want acct-1", r.AccountID())
	}
}

func TestResource_ApplyEvent_AccountAssignedNeverReplacesAnOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := &Resource{}
	if err := r.Restore("urn:statement:e", "statement", "active",
		json.RawMessage(`{"name":"May"}`), "urn:agent:owner", "acct-1", time.Now(), 3); err != nil {
		t.Fatalf("restore: %v", err)
	}
	ev := newTestEnvelope(ResourceAccountAssigned{AccountID: "acct-2", Timestamp: time.Now()},
		"urn:statement:e", "Resource.AccountAssigned", 4)
	if err := r.ApplyEvent(ctx, ev); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if r.AccountID() != "acct-1" {
		t.Fatalf("account = %q, want the first owner acct-1", r.AccountID())
	}
}
