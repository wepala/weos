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
	"context"
	"database/sql"
	"fmt"

	gormlib "gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
)

// openReadOnlyStore opens the database dsn names for a command that changes
// nothing, and returns one read-only transaction over it. done rolls the
// transaction back and closes the connection.
//
// It opens the database on its own, not through the application module:
// starting the module runs migrations, installs presets and starts background
// writers, and a command that changes nothing must do none of that. It is not
// the server's dialector either: the server's SQLite pragmas switch the file to
// WAL journal mode, which writes to it. A SQLite file is opened read-only with
// no pragma, and a SQLite path with no file behind it is refused, because
// opening it would create an empty database.
//
// Every read runs in the one read-only transaction: Postgres refuses a write
// inside it, and the command reads one consistent view of the tables.
func openReadOnlyStore(ctx context.Context, dsn string) (tx *gormlib.DB, done func(), err error) {
	if path, missing := sqliteFileMissing(dsn); missing {
		return nil, nil, fmt.Errorf("no database at %s: point DATABASE_DSN or --database-dsn at the server's database", path)
	}
	openErr := func(err error) error {
		return fmt.Errorf("failed to open the database (check DATABASE_DSN): %s", redactDSN(rootCause(err).Error(), dsn))
	}
	db, err := gormlib.Open(gormdb.ReadOnlyDialectorForDSN(dsn), &gormlib.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, nil, openErr(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, openErr(err)
	}
	tx = db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if tx.Error != nil {
		// Nothing was read; a failed close loses nothing.
		_ = sqlDB.Close()
		return nil, nil, openErr(tx.Error)
	}
	return tx, func() {
		// The transaction only read, so rolling it back discards nothing, and
		// a failed close loses nothing: the process is about to exit.
		_ = tx.Rollback()
		_ = sqlDB.Close()
	}, nil
}
