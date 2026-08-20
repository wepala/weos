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
	"fmt"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
)

// Which surface made a feature change. Recorded for the audit trail only —
// never consulted when deciding whether a caller is allowed to make the
// change. Permission keys off the request context, so a caller who could pass
// the wrong constant could not thereby grant themselves anything.
const (
	FeatureChangeSourceCLI  = "cli"
	FeatureChangeSourceAPI  = "api"
	FeatureChangeSourceMCP  = "mcp"
	FeatureChangeSourceBoot = "boot"
)

// What happened to the setting.
const (
	FeatureChangeStateOn      = "on"
	FeatureChangeStateOff     = "off"
	FeatureChangeStateReset   = "reset"
	FeatureChangeStateGranted = "granted"
	FeatureChangeStateRevoked = "revoked"
)

// FeatureChanged records one change to feature state.
//
// ActorEmail is stored alongside ActorID because an audit record has to be
// readable months later by someone who was not there. The identity on the
// request context carries only an agent id — a KSUID — and a log of KSUIDs
// answers "who" in a way nobody can use without a second lookup they will not
// bother to do. Over the command line both actor fields are empty and Source
// carries "cli", which is the honest record: the operating system authorized
// that change, not WeOS.
type FeatureChanged struct {
	Key        string
	Scope      string
	ScopeID    string
	State      string
	Source     string
	ActorID    string
	ActorEmail string
	Timestamp  time.Time

	// Subject fields are set for grants and revocations, and empty for
	// instance and account overrides. Added after the event already existed;
	// events are immutable, so this is additive only — an older stored event
	// deserializes with these empty, which is what it meant.
	SubjectType  string
	SubjectID    string
	SubjectEmail string
}

func (e FeatureChanged) EventType() string { return "Feature.Changed" }

// FeatureChange is the aggregate that carries one FeatureChanged event.
//
// Each change is its own aggregate, deliberately. Pericarp's events table has
// a unique index on (aggregate_id, sequence_no), and an aggregate built from a
// fresh BaseEntity always records at sequence 1 — so a stable
// "urn:feature:<key>" aggregate would write fine the first time a key was
// flipped and collide every time after, which is exactly when an audit trail
// stops being decorative.
type FeatureChange struct {
	*ddd.BaseEntity
}

// With builds the change and records its event. id comes from
// identity.NewFeatureChange().
func (c *FeatureChange) With(id string, event FeatureChanged) (*FeatureChange, error) {
	if id == "" {
		return nil, fmt.Errorf("feature change id cannot be empty")
	}
	if event.Key == "" {
		return nil, fmt.Errorf("feature change must name a feature")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	c.BaseEntity = ddd.NewBaseEntity(id)
	if err := c.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record Feature.Changed event: %w", err)
	}
	return c, nil
}
