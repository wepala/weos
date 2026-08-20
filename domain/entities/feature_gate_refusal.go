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
	"fmt"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// How a gated capability refuses, in one place for every surface that gates
// (#484 tools, #485 agent skills, #486 the admin UI).
//
// It lives here rather than in each surface because the wording is a decision,
// not a string: it tells the reader whose answer they are looking at, and a
// surface that worded it differently would send that reader somewhere else.

// HasCallerIdentity reports whether ctx carries an authenticated caller.
//
// It is the exact condition under which resolution reaches the account layer
// and the caller's grants at all: with no identity, a resolved set is built
// from the instance layer alone. Every gate that words a refusal branches on
// this, so the wording cannot drift from the answer.
func HasCallerIdentity(ctx context.Context) bool {
	return auth.AgentFromCtx(ctx) != nil
}

// RefusalScope says whose answer a refusal is.
//
// With a caller the answer came from their account and their grants, so "for
// you" is exact and an admin can change it for them. With none — the local
// stdio transport, or an anonymous request — resolution stopped at the
// instance layer, and no account override or personal grant could have
// reached it. Saying "for you" there would send somebody looking for a grant
// that cannot apply on the transport they are using.
func RefusalScope(ctx context.Context) string {
	if HasCallerIdentity(ctx) {
		return "for you"
	}
	return "on this server"
}

// GateRefusal is what a caller is told when a gated capability is not theirs.
// subject is the thing they asked for, named so they can tell this apart from
// asking for something that does not exist at all.
func GateRefusal(ctx context.Context, subject, featureKey string) string {
	return fmt.Sprintf("%s is not available: the %q capability is not enabled %s.",
		subject, featureKey, RefusalScope(ctx))
}
