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

import "context"

// EmailSender is the domain interface for sending emails.
// Implementations live in the infrastructure layer.
type EmailSender interface {
	// Send delivers an email to the given address.
	Send(ctx context.Context, to, subject, body string) error

	// Configured reports whether the sender has valid configuration
	// to actually deliver mail. When false, Send is expected to no-op.
	Configured() bool
}
