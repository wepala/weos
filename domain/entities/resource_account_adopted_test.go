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

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// These tests pin wm-c7xehr: an account with no members can never be used
// again, so the operator moves each of its resources to a named account with
// one Resource.AccountAdopted. The move is compare-and-set on the account the
// resource is in, so a replay or a second run changes nothing.

func ownedResource(t *testing.T, id, account string) *Resource {
	t.Helper()
	r, err := new(Resource).With(id, "task", json.RawMessage(`{"name":"Reconcile May"}`), "urn:agent:writer", account)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	return r
}

func TestResource_AdoptAccount_MovesTheResourceAndRecordsTheEvent(t *testing.T) {
	t.Parallel()
	r := ownedResource(t, "urn:task:a", "acct-orphan")

	if err := r.AdoptAccount("acct-orphan", "acct-target"); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if r.AccountID() != "acct-target" {
		t.Fatalf("account = %q, want acct-target", r.AccountID())
	}
	events := r.GetUncommittedEvents()
	last := events[len(events)-1]
	if last.EventType != "Resource.AccountAdopted" {
		t.Fatalf("last event = %q, want Resource.AccountAdopted", last.EventType)
	}
	payload, ok := last.Payload.(ResourceAccountAdopted)
	if !ok {
		t.Fatalf("payload type = %T, want ResourceAccountAdopted", last.Payload)
	}
	if payload.FromAccountID != "acct-orphan" || payload.AccountID != "acct-target" {
		t.Fatalf("payload = %+v, want from acct-orphan to acct-target", payload)
	}
}

func TestResource_AdoptAccount_RefusesAResourceOfAnotherAccount(t *testing.T) {
	t.Parallel()
	for name, account := range map[string]string{
		"a third account": "acct-third", "the target already": "acct-target", "no account": "",
	} {
		r := ownedResource(t, "urn:task:b", account)
		before := len(r.GetUncommittedEvents())
		err := r.AdoptAccount("acct-orphan", "acct-target")
		if !errors.Is(err, ErrResourceNotInAccount) {
			t.Errorf("%s: err = %v, want ErrResourceNotInAccount", name, err)
		}
		if r.AccountID() != account {
			t.Errorf("%s: account = %q, want it unchanged (%q)", name, r.AccountID(), account)
		}
		if got := len(r.GetUncommittedEvents()); got != before {
			t.Errorf("%s: a refused move recorded %d event(s)", name, got-before)
		}
	}
}

func TestResource_AdoptAccount_RefusesAnEmptyOrTheSameAccount(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ from, to string }{
		{"", "acct-target"}, {"acct-orphan", ""}, {"acct-orphan", "acct-orphan"},
	} {
		r := ownedResource(t, "urn:task:c", "acct-orphan")
		if err := r.AdoptAccount(c.from, c.to); err == nil {
			t.Errorf("adopt from %q to %q succeeded", c.from, c.to)
		}
		if r.AccountID() != "acct-orphan" {
			t.Errorf("from %q to %q: account = %q, want acct-orphan", c.from, c.to, r.AccountID())
		}
	}
}

func restoredIn(t *testing.T, id, account string) *Resource {
	t.Helper()
	r := &Resource{}
	if err := r.Restore(id, "task", "active", json.RawMessage(`{"name":"May"}`), "urn:agent:writer",
		account, time.Now(), 3); err != nil {
		t.Fatalf("restore: %v", err)
	}
	return r
}

func TestResource_ApplyEvent_AccountAdoptedMovesOnlyFromTheNamedAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name, current, want string
	}{
		{"in the source account", "acct-orphan", "acct-target"},
		{"already moved (a replay)", "acct-target", "acct-target"},
		{"in a third account", "acct-third", "acct-third"},
		{"with no owner", "", ""},
	} {
		r := restoredIn(t, "urn:task:d", c.current)
		ev := newTestEnvelope(ResourceAccountAdopted{FromAccountID: "acct-orphan", AccountID: "acct-target",
			Timestamp: time.Now()}, "urn:task:d", "Resource.AccountAdopted", 4)
		if err := r.ApplyEvent(ctx, ev); err != nil {
			t.Fatalf("%s: apply: %v", c.name, err)
		}
		if r.AccountID() != c.want {
			t.Errorf("%s: account = %q, want %q", c.name, r.AccountID(), c.want)
		}
	}
}

// The whole history replayed: created in the source account, moved, then
// updated. The update carries the new owner, and the owner stays moved.
func TestResource_ApplyEvent_AHistoryWithAnAdoptionEndsInTheTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := &Resource{BaseEntity: ddd.NewBaseEntity("urn:task:e")}
	created := newTestEnvelope(ResourceCreated{TypeSlug: "task", Data: json.RawMessage(`{"name":"May"}`),
		CreatedBy: "urn:agent:writer", AccountID: "acct-orphan", Timestamp: time.Now()},
		"urn:task:e", "Resource.Created", 1)
	adopted := newTestEnvelope(ResourceAccountAdopted{FromAccountID: "acct-orphan", AccountID: "acct-target",
		Timestamp: time.Now()}, "urn:task:e", "Resource.AccountAdopted", 2)
	updated := newTestEnvelope(ResourceUpdated{Data: json.RawMessage(`{"name":"June"}`), AccountID: "acct-target",
		Timestamp: time.Now()}, "urn:task:e", "Resource.Updated", 3)
	for _, ev := range []domain.EventEnvelope[any]{created, adopted, updated} {
		if err := r.ApplyEvent(ctx, ev); err != nil {
			t.Fatalf("apply %s: %v", ev.EventType, err)
		}
	}
	if r.AccountID() != "acct-target" {
		t.Fatalf("account = %q, want acct-target", r.AccountID())
	}
	if r.CreatedBy() != "urn:agent:writer" {
		t.Fatalf("created by = %q, want the creator kept", r.CreatedBy())
	}
}
