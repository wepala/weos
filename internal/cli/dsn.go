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

package cli

import (
	"fmt"
	"os"
)

// requireExplicitDSN refuses to guess which store a one-shot command should
// act on.
//
// config.Default() carries a "weos.db" fallback. For a server that means
// "start somewhere"; for a command that changes state it would mean writing
// into whatever directory the process happened to start in, reporting success,
// and exiting 0 — leaving an operator convinced they changed an instance they
// never touched, and a stray database file behind to prove otherwise. An
// entrypoint's DSN is never implicit, so neither is a command's.
//
// Shared by every such command rather than copied into each, so they cannot
// drift into disagreeing about when it is safe to guess.
func requireExplicitDSN(command string) error {
	if os.Getenv("DATABASE_DSN") != "" || databaseDSN != "" {
		return nil
	}
	return fmt.Errorf(
		"no database specified: set DATABASE_DSN or pass --database-dsn "+
			"(%s will not fall back to the built-in default, so it never acts on "+
			"a store nobody asked for)", command)
}
