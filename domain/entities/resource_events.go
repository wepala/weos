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
	"encoding/json"
	"time"
)

type ResourceCreated struct {
	TypeSlug  string
	Data      json.RawMessage
	CreatedBy string
	AccountID string
	Timestamp time.Time
}

func (e *ResourceCreated) With(typeSlug string, data json.RawMessage, createdBy, accountID string) ResourceCreated {
	return ResourceCreated{
		TypeSlug:  typeSlug,
		Data:      data,
		CreatedBy: createdBy,
		AccountID: accountID,
		Timestamp: time.Now(),
	}
}

func (e ResourceCreated) EventType() string {
	return "Resource.Created"
}

// AccountID is carried on every resource event (not just ResourceCreated) so a
// background projector can route the event to the owning account's knowledge
// graph without a separate history lookup. The resource entity always knows its
// account (set from ResourceCreated on load), so recording it is free; events
// persisted before this field existed carry an empty value and the projector
// falls back to the aggregate's ResourceCreated to recover it.
type ResourceUpdated struct {
	Data      json.RawMessage
	AccountID string
	Timestamp time.Time
}

func (e ResourceUpdated) With(data json.RawMessage, accountID string) ResourceUpdated {
	return ResourceUpdated{
		Data:      data,
		AccountID: accountID,
		Timestamp: time.Now(),
	}
}

func (e ResourceUpdated) EventType() string {
	return "Resource.Updated"
}

type ResourceDeleted struct {
	AccountID string
	Timestamp time.Time
}

func (e ResourceDeleted) With(accountID string) ResourceDeleted {
	return ResourceDeleted{AccountID: accountID, Timestamp: time.Now()}
}

func (e ResourceDeleted) EventType() string {
	return "Resource.Deleted"
}

type ResourcePublished struct {
	TypeSlug  string
	AccountID string
	Timestamp time.Time
}

func (e ResourcePublished) With(typeSlug, accountID string) ResourcePublished {
	return ResourcePublished{
		TypeSlug:  typeSlug,
		AccountID: accountID,
		Timestamp: time.Now(),
	}
}

func (e ResourcePublished) EventType() string {
	return "Resource.Published"
}

// ResourceAccountAssigned gives an ownerless resource its owning account.
//
// Resource.Created is the only other event that sets the owner, and a
// resource written by a session whose active account was empty stored an
// empty one there. Nothing else could change it afterwards, so a projection
// row fixed by hand lost its owner again on the next rebuild. This event puts
// the owner into the stream, where every rebuild reads it.
//
// It fills an empty owner only. It is never a transfer between accounts:
// Resource.AssignAccount refuses a resource that already has an owner, and
// every consumer keeps the first owner it saw. Once written, this event type
// stays readable for as long as the stream does.
type ResourceAccountAssigned struct {
	AccountID string
	Timestamp time.Time
}

func (e ResourceAccountAssigned) With(accountID string) ResourceAccountAssigned {
	return ResourceAccountAssigned{AccountID: accountID, Timestamp: time.Now()}
}

func (e ResourceAccountAssigned) EventType() string {
	return "Resource.AccountAssigned"
}

// ResourceAccountAdopted moves a resource out of an account that nobody can
// use any more — an account with no members — into a named account
// (wm-c7xehr).
//
// It is the one event that moves a set owner. Resource.AccountAssigned only
// fills an empty one, and that rule stands. A move is compare-and-set on
// FromAccountID: the aggregate, the canonical record, every projection row,
// the knowledge graph and the lexical index each apply it only while the
// resource is still in FromAccountID, so a replay or a second run of the
// operator command changes nothing. AccountID is the account the resource
// moves to; it carries the same name as on every other resource event so a
// background subscriber routes the event to the new owner.
//
// Once written, this event type stays readable for as long as the stream
// does.
type ResourceAccountAdopted struct {
	FromAccountID string
	AccountID     string
	Timestamp     time.Time
}

func (e ResourceAccountAdopted) With(fromAccountID, accountID string) ResourceAccountAdopted {
	return ResourceAccountAdopted{FromAccountID: fromAccountID, AccountID: accountID, Timestamp: time.Now()}
}

func (e ResourceAccountAdopted) EventType() string {
	return "Resource.AccountAdopted"
}

const ResourceEventPattern = "Resource.%"
