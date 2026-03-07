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

package config

import "errors"

var (
	// ErrMissingDatabaseDSN is returned when DatabaseDSN is not set.
	ErrMissingDatabaseDSN = errors.New("database DSN is required")

	// ErrInvalidLogLevel is returned when LogLevel has an invalid value.
	ErrInvalidLogLevel = errors.New("invalid log level, must be one of: debug, info, warn, error")
)
