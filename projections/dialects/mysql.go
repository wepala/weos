package dialects

import (
	"database/sql"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type MySQL struct {
	*mysql.Dialector
}

func NewMySQL(config mysql.Config) gorm.Dialector {
	return &MySQL{&mysql.Dialector{Config: &config}}
}

type MySQLColumn struct {
	name              string
	nullable          sql.NullString
	datatype          string
	maxLen            sql.NullInt64
	precision         sql.NullInt64
	scale             sql.NullInt64
	datetimePrecision sql.NullInt64
}

func (c MySQLColumn) Name() string {
	return c.name
}

func (c MySQLColumn) DatabaseTypeName() string {
	return c.datatype
}

func (c MySQLColumn) Length() (int64, bool) {
	if c.maxLen.Valid {
		return c.maxLen.Int64, c.maxLen.Valid
	}

	return 0, false
}

func (c MySQLColumn) Nullable() (bool, bool) {
	if c.nullable.Valid {
		return c.nullable.String == "YES", true
	}

	return false, false
}

// DecimalSize return precision int64, scale int64, ok bool
func (c MySQLColumn) DecimalSize() (int64, int64, bool) {
	if c.precision.Valid {
		if c.scale.Valid {
			return c.precision.Int64, c.scale.Int64, true
		}

		return c.precision.Int64, 0, true
	}

	if c.datetimePrecision.Valid {
		return c.datetimePrecision.Int64, 0, true
	}

	return 0, 0, false
}

type MySQLMigrator struct {
	Migrator
}

func (dialector MySQL) Migrator(db *gorm.DB) gorm.Migrator {
	return MySQLMigrator{
		Migrator{
			Migrator: migrator.Migrator{
				Config: migrator.Config{
					DB:        db,
					Dialector: &dialector,
				},
			},
		},
	}
}

func (m MySQLMigrator) AlterColumn(value interface{}, field string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(field); field != nil {
			return m.DB.Exec(
				"ALTER TABLE ? MODIFY COLUMN ? ?",
				clause.Table{Name: stmt.Table}, clause.Column{Name: field.DBName}, m.FullDataTypeOf(field),
			).Error
		}
		return fmt.Errorf("failed to look up field with name: %s", field)
	})
}

func (m MySQLMigrator) RenameColumn(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if !m.Dialector.(*MySQL).DontSupportRenameColumn {
			return m.Migrator.RenameColumn(value, oldName, newName)
		}

		var field *schema.Field
		if f := stmt.Schema.LookUpField(oldName); f != nil {
			oldName = f.DBName
			field = f
		}

		if f := stmt.Schema.LookUpField(newName); f != nil {
			newName = f.DBName
			field = f
		}

		if field != nil {
			return m.DB.Exec(
				"ALTER TABLE ? CHANGE ? ? ?",
				clause.Table{Name: stmt.Table}, clause.Column{Name: oldName},
				clause.Column{Name: newName}, m.FullDataTypeOf(field),
			).Error
		}

		return fmt.Errorf("failed to look up field with name: %s", newName)
	})
}

func (m MySQLMigrator) RenameIndex(value interface{}, oldName, newName string) error {
	if !m.Dialector.(*MySQL).Config.DontSupportRenameIndex {
		return m.RunWithValue(value, func(stmt *gorm.Statement) error {
			return m.DB.Exec(
				"ALTER TABLE ? RENAME INDEX ? TO ?",
				clause.Table{Name: stmt.Table}, clause.Column{Name: oldName}, clause.Column{Name: newName},
			).Error
		})
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		err := m.DropIndex(value, oldName)
		if err != nil {
			return err
		}

		if idx := stmt.Schema.LookIndex(newName); idx == nil {
			if idx = stmt.Schema.LookIndex(oldName); idx != nil {
				opts := m.BuildIndexOptions(idx.Fields, stmt)
				values := []interface{}{clause.Column{Name: newName}, clause.Table{Name: stmt.Table}, opts}

				createIndexSQL := "CREATE "
				if idx.Class != "" {
					createIndexSQL += idx.Class + " "
				}
				createIndexSQL += "INDEX ? ON ??"

				if idx.Type != "" {
					createIndexSQL += " USING " + idx.Type
				}

				return m.DB.Exec(createIndexSQL, values...).Error
			}
		}

		return m.CreateIndex(value, newName)
	})

}

func (m MySQLMigrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	tx := m.DB.Session(&gorm.Session{})
	tx.Exec("SET FOREIGN_KEY_CHECKS = 0;")
	for i := len(values) - 1; i >= 0; i-- {
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return tx.Exec("DROP TABLE IF EXISTS ? CASCADE", clause.Table{Name: stmt.Table}).Error
		}); err != nil {
			return err
		}
	}
	tx.Exec("SET FOREIGN_KEY_CHECKS = 1;")
	return nil
}

func (m MySQLMigrator) DropConstraint(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)
		if chk != nil {
			return m.DB.Exec("ALTER TABLE ? DROP CHECK ?", clause.Table{Name: stmt.Table}, clause.Column{Name: chk.Name}).Error
		}
		if constraint != nil {
			name = constraint.Name
		}

		return m.DB.Exec(
			"ALTER TABLE ? DROP FOREIGN KEY ?", clause.Table{Name: table}, clause.Column{Name: name},
		).Error
	})
}

// ColumnTypes column types return columnTypes,error
func (m MySQLMigrator) ColumnTypes(value interface{}) ([]gorm.ColumnType, error) {
	columnTypes := make([]gorm.ColumnType, 0)
	err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var (
			currentDatabase = m.DB.Migrator().CurrentDatabase()
			columnTypeSQL   = "SELECT column_name, is_nullable, data_type, character_maximum_length, numeric_precision, numeric_scale "
		)

		if !m.Dialector.(*MySQL).DisableDatetimePrecision {
			columnTypeSQL += ", datetime_precision "
		}
		columnTypeSQL += "FROM information_schema.columns WHERE table_schema = ? AND table_name = ?"

		columns, rowErr := m.DB.Raw(columnTypeSQL, currentDatabase, stmt.Table).Rows()
		if rowErr != nil {
			return rowErr
		}

		defer columns.Close()

		for columns.Next() {
			var column MySQLColumn
			var values = []interface{}{&column.name, &column.nullable, &column.datatype,
				&column.maxLen, &column.precision, &column.scale}

			if !m.Dialector.(*MySQL).DisableDatetimePrecision {
				values = append(values, &column.datetimePrecision)
			}

			if scanErr := columns.Scan(values...); scanErr != nil {
				return scanErr
			}
			columnTypes = append(columnTypes, column)
		}

		return nil
	})

	return columnTypes, err
}
