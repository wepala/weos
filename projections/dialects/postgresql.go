package dialects

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Postgres struct {
	postgres.Dialector
}

func NewPostgres(config postgres.Config) gorm.Dialector {
	return &Postgres{
		postgres.Dialector{Config: &config},
	}
}

func (dialector Postgres) Migrator(db *gorm.DB) gorm.Migrator {
	return PostgresMigrator{
		Migrator{
			Migrator: migrator.Migrator{
				Config: migrator.Config{
					DB:                          db,
					Dialector:                   dialector,
					CreateIndexAfterCreateTable: true,
				},
			},
		},
	}
}

type PostgresMigrator struct {
	Migrator
}

func (m PostgresMigrator) CurrentDatabase() (name string) {
	m.DB.Raw("SELECT CURRENT_DATABASE()").Scan(&name)
	return
}

func (m PostgresMigrator) BuildIndexOptions(opts []schema.IndexOption, stmt *gorm.Statement) (results []interface{}) {
	for _, opt := range opts {
		str := stmt.Quote(opt.DBName)
		if opt.Expression != "" {
			str = opt.Expression
		}

		if opt.Collate != "" {
			str += " COLLATE " + opt.Collate
		}

		if opt.Sort != "" {
			str += " " + opt.Sort
		}
		results = append(results, clause.Expr{SQL: str})
	}
	return
}

func (m PostgresMigrator) HasIndex(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw(
			"SELECT count(*) FROM pg_indexes WHERE tablename = ? AND indexname = ? AND schemaname = ?", curTable, name, currentSchema,
		).Scan(&count).Error
	})

	return count > 0
}

func (m PostgresMigrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			opts := m.BuildIndexOptions(idx.Fields, stmt)
			values := []interface{}{clause.Column{Name: idx.Name}, m.CurrentTable(stmt), opts}

			createIndexSQL := "CREATE "
			if idx.Class != "" {
				createIndexSQL += idx.Class + " "
			}
			createIndexSQL += "INDEX "

			if strings.TrimSpace(strings.ToUpper(idx.Option)) == "CONCURRENTLY" {
				createIndexSQL += "CONCURRENTLY "
			}

			createIndexSQL += "? ON ?"

			if idx.Type != "" {
				createIndexSQL += " USING " + idx.Type + "(?)"
			} else {
				createIndexSQL += " ?"
			}

			if idx.Where != "" {
				createIndexSQL += " WHERE " + idx.Where
			}

			return m.DB.Exec(createIndexSQL, values...).Error
		}

		return fmt.Errorf("failed to create index with name %v", name)
	})
}

func (m PostgresMigrator) RenameIndex(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Exec(
			"ALTER INDEX ? RENAME TO ?",
			clause.Column{Name: oldName}, clause.Column{Name: newName},
		).Error
	})
}

func (m PostgresMigrator) DropIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
	})
}

func (m PostgresMigrator) GetTables() (tableList []string, err error) {
	currentSchema, _ := m.CurrentSchema(m.DB.Statement, "")
	return tableList, m.DB.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_type = ?", currentSchema, "BASE TABLE").Scan(&tableList).Error
}

func (m PostgresMigrator) CreateTable(values ...interface{}) (err error) {
	if err = m.Migrator.CreateTable(values...); err != nil {
		return
	}
	for _, value := range m.ReorderModels(values, false) {
		if err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
			for _, field := range stmt.Schema.FieldsByDBName {
				if field.Comment != "" {
					if err := m.DB.Exec(
						"COMMENT ON COLUMN ?.? IS ?",
						m.CurrentTable(stmt), clause.Column{Name: field.DBName}, gorm.Expr(m.Migrator.Dialector.Explain("$1", field.Comment)),
					).Error; err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return
		}
	}
	return
}

func (m PostgresMigrator) HasTable(value interface{}) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ? AND table_type = ?", currentSchema, curTable, "BASE TABLE").Scan(&count).Error
	})
	return count > 0
}

func (m PostgresMigrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	tx := m.DB.Session(&gorm.Session{})
	for i := len(values) - 1; i >= 0; i-- {
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return tx.Exec("DROP TABLE IF EXISTS ? CASCADE", m.CurrentTable(stmt)).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m PostgresMigrator) AddColumn(value interface{}, field string) error {
	if err := m.Migrator.AddColumn(value, field); err != nil {
		return err
	}
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(field); field != nil {
			if field.Comment != "" {
				if err := m.DB.Exec(
					"COMMENT ON COLUMN ?.? IS ?",
					m.CurrentTable(stmt), clause.Column{Name: field.DBName}, gorm.Expr(m.Migrator.Dialector.Explain("$1", field.Comment)),
				).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (m PostgresMigrator) HasColumn(value interface{}, field string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		name := field
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(field); field != nil {
				name = field.DBName
			}
		}

		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?",
			currentSchema, curTable, name,
		).Scan(&count).Error
	})

	return count > 0
}

func (m PostgresMigrator) MigrateColumn(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	// skip primary field
	if !field.PrimaryKey {
		if err := m.Migrator.MigrateColumn(value, field, columnType); err != nil {
			return err
		}
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var description string
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		values := []interface{}{currentSchema, curTable, field.DBName, stmt.Table, currentSchema}
		checkSQL := "SELECT description FROM pg_catalog.pg_description "
		checkSQL += "WHERE objsubid = (SELECT ordinal_position FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?) "
		checkSQL += "AND objoid = (SELECT oid FROM pg_catalog.pg_class WHERE relname = ? AND relnamespace = "
		checkSQL += "(SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = ?))"
		m.DB.Raw(checkSQL, values...).Scan(&description)
		comment := field.Comment
		if comment != "" {
			comment = comment[1 : len(comment)-1]
		}
		if field.Comment != "" && comment != description {
			if err := m.DB.Exec(
				"COMMENT ON COLUMN ?.? IS ?",
				m.CurrentTable(stmt), clause.Column{Name: field.DBName}, gorm.Expr(m.Migrator.Dialector.Explain("$1", field.Comment)),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (m PostgresMigrator) HasConstraint(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)
		currentSchema, curTable := m.CurrentSchema(stmt, table)
		if constraint != nil {
			name = constraint.Name
		} else if chk != nil {
			name = chk.Name
		}

		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.table_constraints WHERE table_schema = ? AND table_name = ? AND constraint_name = ?",
			currentSchema, curTable, name,
		).Scan(&count).Error
	})

	return count > 0
}

func (m PostgresMigrator) GetRows(currentSchema interface{}, table interface{}) (*sql.Rows, error) {
	name := table.(string)
	if currentSchema != nil {
		if _, ok := currentSchema.(string); ok {
			name = fmt.Sprintf("%v.%v", currentSchema, table)
		}
	}
	return m.DB.Session(&gorm.Session{}).Table(name).Limit(1).Rows()
}

func (m PostgresMigrator) ColumnTypes(value interface{}) (columnTypes []gorm.ColumnType, err error) {
	columnTypes = make([]gorm.ColumnType, 0)
	err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var (
			currentDatabase      = m.DB.Migrator().CurrentDatabase()
			currentSchema, table = m.CurrentSchema(stmt, stmt.Table)
			columns, err         = m.DB.Raw(
				"SELECT c.column_name, c.is_nullable = 'YES', c.udt_name, c.character_maximum_length, c.numeric_precision, c.numeric_precision_radix, c.numeric_scale, c.datetime_precision, 8 * typlen, c.column_default, pd.description FROM information_schema.columns AS c JOIN pg_type AS pgt ON c.udt_name = pgt.typname LEFT JOIN pg_catalog.pg_description as pd ON pd.objsubid = c.ordinal_position AND pd.objoid = (SELECT oid FROM pg_catalog.pg_class WHERE relname = c.table_name AND relnamespace = (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = c.table_schema)) where table_catalog = ? AND table_schema = ? AND table_name = ?",
				currentDatabase, currentSchema, table).Rows()
		)

		if err != nil {
			return err
		}

		for columns.Next() {
			var (
				column = &migrator.ColumnType{
					PrimaryKeyValue: sql.NullBool{Valid: true},
					UniqueValue:     sql.NullBool{Valid: true},
				}
				datetimePrecision sql.NullInt64
				radixValue        sql.NullInt64
				typeLenValue      sql.NullInt64
			)

			err = columns.Scan(
				&column.NameValue, &column.NullableValue, &column.DataTypeValue, &column.LengthValue, &column.DecimalSizeValue,
				&radixValue, &column.ScaleValue, &datetimePrecision, &typeLenValue, &column.DefaultValueValue, &column.CommentValue,
			)
			if err != nil {
				return err
			}

			if typeLenValue.Valid && typeLenValue.Int64 > 0 {
				column.LengthValue = typeLenValue
			}

			if strings.HasPrefix(column.DefaultValueValue.String, "nextval('") && strings.HasSuffix(column.DefaultValueValue.String, "seq'::regclass)") {
				column.AutoIncrementValue = sql.NullBool{Bool: true, Valid: true}
				column.DefaultValueValue = sql.NullString{}
			}

			if column.DefaultValueValue.Valid {
				column.DefaultValueValue.String = regexp.MustCompile("'(.*)'::[\\w]+$").ReplaceAllString(column.DefaultValueValue.String, "$1")
			}

			if datetimePrecision.Valid {
				column.DecimalSizeValue = datetimePrecision
			}

			columnTypes = append(columnTypes, column)
		}
		columns.Close()

		// assign sql column type
		{
			rows, rowsErr := m.GetRows(currentSchema, table)
			if rowsErr != nil {
				return rowsErr
			}
			rawColumnTypes, err := rows.ColumnTypes()
			if err != nil {
				return err
			}
			for _, columnType := range columnTypes {
				for _, c := range rawColumnTypes {
					if c.Name() == columnType.Name() {
						columnType.(*migrator.ColumnType).SQLColumnType = c
						break
					}
				}
			}
			rows.Close()
		}

		// check primary, unique field
		{
			columnTypeRows, err := m.DB.Raw("SELECT c.column_name, constraint_type FROM information_schema.table_constraints tc JOIN information_schema.constraint_column_usage AS ccu USING (constraint_schema, constraint_name) JOIN information_schema.columns AS c ON c.table_schema = tc.constraint_schema AND tc.table_name = c.table_name AND ccu.column_name = c.column_name WHERE constraint_type IN ('PRIMARY KEY', 'UNIQUE') AND c.table_catalog = ? AND c.table_schema = ? AND c.table_name = ?", currentDatabase, currentSchema, table).Rows()
			if err != nil {
				return err
			}

			for columnTypeRows.Next() {
				var name, columnType string
				columnTypeRows.Scan(&name, &columnType)
				for _, c := range columnTypes {
					mc := c.(*migrator.ColumnType)
					if mc.NameValue.String == name {
						switch columnType {
						case "PRIMARY KEY":
							mc.PrimaryKeyValue = sql.NullBool{Bool: true, Valid: true}
						case "UNIQUE":
							mc.UniqueValue = sql.NullBool{Bool: true, Valid: true}
						}
						break
					}
				}
			}
			columnTypeRows.Close()
		}

		// check column type
		{
			dataTypeRows, err := m.DB.Raw(`SELECT a.attname as column_name, format_type(a.atttypid, a.atttypmod) AS data_type
		FROM pg_attribute a JOIN pg_class b ON a.attrelid = b.relfilenode AND relnamespace = (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = ?)
		WHERE a.attnum > 0 -- hide internal columns
		AND NOT a.attisdropped -- hide deleted columns
		AND b.relname = ?`, currentSchema, table).Rows()
			if err != nil {
				return err
			}

			for dataTypeRows.Next() {
				var name, dataType string
				dataTypeRows.Scan(&name, &dataType)
				for _, c := range columnTypes {
					mc := c.(*migrator.ColumnType)
					if mc.NameValue.String == name {
						mc.ColumnTypeValue = sql.NullString{String: dataType, Valid: true}
						break
					}
				}
			}
			dataTypeRows.Close()
		}

		return err
	})
	return
}

func (m PostgresMigrator) CurrentSchema(stmt *gorm.Statement, table string) (interface{}, interface{}) {
	if strings.Contains(table, ".") {
		if tables := strings.Split(table, `.`); len(tables) == 2 {
			return tables[0], tables[1]
		}
	}

	if stmt.TableExpr != nil {
		if tables := strings.Split(stmt.TableExpr.SQL, `"."`); len(tables) == 2 {
			return strings.TrimPrefix(tables[0], `"`), table
		}
	}
	return clause.Expr{SQL: "CURRENT_SCHEMA()"}, table
}
