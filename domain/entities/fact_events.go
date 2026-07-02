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

import "time"

// FactRecorded signals that a consolidated fact was committed. Like
// ResourcePublished it is a signal event recorded on the fact's aggregate in
// the same UnitOfWork commit as the underlying ResourceCreated — it carries
// provenance for subscribers and audit, never entity state.
type FactRecorded struct {
	FactID      string
	Statement   string
	DerivedFrom []string
	Timestamp   time.Time
}

func (e FactRecorded) EventType() string {
	return "Fact.Recorded"
}

// FactSuperseded signals that the fact identified by FactID has been
// superseded by SupersededBy. It rides the superseding fact's aggregate so it
// commits atomically with that fact's creation; the predecessor's
// invalidatedAtTime literal arrives via its own ResourceUpdated event.
type FactSuperseded struct {
	FactID       string
	SupersededBy string
	Timestamp    time.Time
}

func (e FactSuperseded) EventType() string {
	return "Fact.Superseded"
}
