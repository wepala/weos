package dialects

import (
	"errors"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	"regexp"
	"strings"
)

type SQLite struct {
	sqlite.Dialector
}

func (dialector *SQLite) Migrator(db *gorm.DB) gorm.Migrator {
	return SQLiteMigrator{
		Migrator{
			migrator.Migrator{
				Config: migrator.Config{
					DB:                          db,
					Dialector:                   dialector,
					CreateIndexAfterCreateTable: true,
				},
			},
		},
	}
}

type SQLiteMigrator struct {
	Migrator
}

func (m *SQLiteMigrator) RunWithoutForeignKey(fc func() error) error {
	var enabled int
	m.DB.Raw("PRAGMA foreign_keys").Scan(&enabled)
	if enabled == 1 {
		m.DB.Exec("PRAGMA foreign_keys = OFF")
		defer m.DB.Exec("PRAGMA foreign_keys = ON")
	}

	return fc()
}

func (m SQLiteMigrator) HasTable(value interface{}) bool {
	var count int
	m.Migrator.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", stmt.Table).Row().Scan(&count)
	})
	return count > 0
}

func (m SQLiteMigrator) DropTable(values ...interface{}) error {
	return m.RunWithoutForeignKey(func() error {
		values = m.ReorderModels(values, false)
		tx := m.DB.Session(&gorm.Session{})

		for i := len(values) - 1; i >= 0; i-- {
			if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
				return tx.Exec("DROP TABLE IF EXISTS ?", clause.Table{Name: stmt.Table}).Error
			}); err != nil {
				return err
			}
		}

		return nil
	})
}

func (m SQLiteMigrator) GetTables() (tableList []string, err error) {
	return tableList, m.DB.Raw("SELECT name FROM sqlite_master where type=?", "table").Scan(&tableList).Error
}

func (m SQLiteMigrator) HasColumn(value interface{}, name string) bool {
	var count int
	m.Migrator.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				name = field.DBName
			}
		}

		if name != "" {
			m.DB.Raw(
				"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND (sql LIKE ? OR sql LIKE ? OR sql LIKE ?)",
				"table", stmt.Table, `%"`+name+`" %`, `%`+name+` %`, "%`"+name+"`%",
			).Row().Scan(&count)
		}
		return nil
	})
	return count > 0
}

func (m SQLiteMigrator) AlterColumn(value interface{}, name string) error {
	return m.RunWithoutForeignKey(func() error {
		return m.recreateTable(value, nil, func(rawDDL string, stmt *gorm.Statement) (sql string, sqlArgs []interface{}, err error) {
			if field := stmt.Schema.LookUpField(name); field != nil {
				reg, err := regexp.Compile("(`|'|\"| )" + field.DBName + "(`|'|\"| ) .*?,")
				if err != nil {
					return "", nil, err
				}

				createSQL := reg.ReplaceAllString(rawDDL, fmt.Sprintf("`%v` ?,", field.DBName))

				return createSQL, []interface{}{m.FullDataTypeOf(field)}, nil

			} else {
				return "", nil, fmt.Errorf("failed to alter field with name %v", name)
			}
		})
	})
}

func (m SQLiteMigrator) DropColumn(value interface{}, name string) error {
	return m.recreateTable(value, nil, func(rawDDL string, stmt *gorm.Statement) (sql string, sqlArgs []interface{}, err error) {
		if field := stmt.Schema.LookUpField(name); field != nil {
			name = field.DBName
		}

		reg, err := regexp.Compile("(`|'|\"| )" + name + "(`|'|\"| ) .*?,")
		if err != nil {
			return "", nil, err
		}

		createSQL := reg.ReplaceAllString(rawDDL, "")

		return createSQL, nil, nil
	})
}

func (m SQLiteMigrator) CreateConstraint(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)

		return m.recreateTable(value, &table,
			func(rawDDL string, stmt *gorm.Statement) (sql string, sqlArgs []interface{}, err error) {
				var (
					constraintName   string
					constraintSql    string
					constraintValues []interface{}
				)

				if constraint != nil {
					constraintName = constraint.Name
					constraintSql, constraintValues = buildConstraint(constraint)
				} else if chk != nil {
					constraintName = chk.Name
					constraintSql = "CONSTRAINT ? CHECK (?)"
					constraintValues = []interface{}{clause.Column{Name: chk.Name}, clause.Expr{SQL: chk.Constraint}}
				} else {
					return "", nil, nil
				}

				createDDL, err := parseDDL(rawDDL)
				if err != nil {
					return "", nil, err
				}
				createDDL.addConstraint(constraintName, constraintSql)
				createSQL := createDDL.compile()

				return createSQL, constraintValues, nil
			})
	})
}

func (m SQLiteMigrator) DropConstraint(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)
		if constraint != nil {
			name = constraint.Name
		} else if chk != nil {
			name = chk.Name
		}

		return m.recreateTable(value, &table,
			func(rawDDL string, stmt *gorm.Statement) (sql string, sqlArgs []interface{}, err error) {
				createDDL, err := parseDDL(rawDDL)
				if err != nil {
					return "", nil, err
				}
				createDDL.removeConstraint(name)
				createSQL := createDDL.compile()

				return createSQL, nil, nil
			})
	})
}

func (m SQLiteMigrator) HasConstraint(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)
		if constraint != nil {
			name = constraint.Name
		} else if chk != nil {
			name = chk.Name
		}

		m.DB.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND (sql LIKE ? OR sql LIKE ? OR sql LIKE ?)",
			"table", table, `%CONSTRAINT "`+name+`" %`, `%CONSTRAINT `+name+` %`, "%CONSTRAINT `"+name+"`%",
		).Row().Scan(&count)

		return nil
	})

	return count > 0
}

func (m SQLiteMigrator) CurrentDatabase() (name string) {
	var null interface{}
	m.DB.Raw("PRAGMA database_list").Row().Scan(&null, &name, &null)
	return
}

func (m SQLiteMigrator) BuildIndexOptions(opts []schema.IndexOption, stmt *gorm.Statement) (results []interface{}) {
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

func (m Migrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			opts := m.BuildIndexOptions(idx.Fields, stmt)
			values := []interface{}{clause.Column{Name: idx.Name}, clause.Table{Name: stmt.Table}, opts}

			createIndexSQL := "CREATE "
			if idx.Class != "" {
				createIndexSQL += idx.Class + " "
			}
			createIndexSQL += "INDEX ?"

			if idx.Type != "" {
				createIndexSQL += " USING " + idx.Type
			}
			createIndexSQL += " ON ??"

			if idx.Where != "" {
				createIndexSQL += " WHERE " + idx.Where
			}

			return m.DB.Exec(createIndexSQL, values...).Error
		}

		return fmt.Errorf("failed to create index with name %v", name)
	})
}

func (m SQLiteMigrator) HasIndex(value interface{}, name string) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		if name != "" {
			m.DB.Raw(
				"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?", "index", stmt.Table, name,
			).Row().Scan(&count)
		}
		return nil
	})
	return count > 0
}

func (m Migrator) RenameIndex(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var sql string
		m.DB.Raw("SELECT sql FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?", "index", stmt.Table, oldName).Row().Scan(&sql)
		if sql != "" {
			return m.DB.Exec(strings.Replace(sql, oldName, newName, 1)).Error
		}
		return fmt.Errorf("failed to find index with name %v", oldName)
	})
}

func (m Migrator) DropIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
	})
}

func sqliteBuildConstraint(constraint *schema.Constraint) (sql string, results []interface{}) {
	sql = "CONSTRAINT ? FOREIGN KEY ? REFERENCES ??"
	if constraint.OnDelete != "" {
		sql += " ON DELETE " + constraint.OnDelete
	}

	if constraint.OnUpdate != "" {
		sql += " ON UPDATE " + constraint.OnUpdate
	}

	var foreignKeys, references []interface{}
	for _, field := range constraint.ForeignKeys {
		foreignKeys = append(foreignKeys, clause.Column{Name: field.DBName})
	}

	for _, field := range constraint.References {
		references = append(references, clause.Column{Name: field.DBName})
	}
	results = append(results, clause.Table{Name: constraint.Name}, foreignKeys, clause.Table{Name: constraint.ReferenceSchema.Table}, references)
	return
}

func (m SQLiteMigrator) getRawDDL(table string) (string, error) {
	var createSQL string
	m.DB.Raw("SELECT sql FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?", "table", table, table).Row().Scan(&createSQL)

	if m.DB.Error != nil {
		return "", m.DB.Error
	}
	return createSQL, nil
}

func (m SQLiteMigrator) recreateTable(value interface{}, tablePtr *string,
	getCreateSQL func(rawDDL string, stmt *gorm.Statement) (sql string, sqlArgs []interface{}, err error)) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		table := stmt.Table
		if tablePtr != nil {
			table = *tablePtr
		}

		rawDDL, err := m.getRawDDL(table)
		if err != nil {
			return err
		}

		newTableName := table + "__temp"

		createSQL, sqlArgs, err := getCreateSQL(rawDDL, stmt)
		if err != nil {
			return err
		}
		if createSQL == "" {
			return nil
		}

		tableReg, err := regexp.Compile(" ('|`|\"| )" + table + "('|`|\"| ) ")
		if err != nil {
			return err
		}
		createSQL = tableReg.ReplaceAllString(createSQL, fmt.Sprintf(" `%v` ", newTableName))

		createDDL, err := parseDDL(createSQL)
		if err != nil {
			return err
		}
		columns := createDDL.getColumns()

		return m.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(createSQL, sqlArgs...).Error; err != nil {
				return err
			}

			queries := []string{
				fmt.Sprintf("INSERT INTO `%v`(%v) SELECT %v FROM `%v`", newTableName, strings.Join(columns, ","), strings.Join(columns, ","), table),
				fmt.Sprintf("DROP TABLE `%v`", table),
				fmt.Sprintf("ALTER TABLE `%v` RENAME TO `%v`", newTableName, table),
			}
			for _, query := range queries {
				if err := tx.Exec(query).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

type ddl struct {
	head   string
	fields []string
}

func parseDDL(sql string) (*ddl, error) {
	reg := regexp.MustCompile("(?i)(CREATE TABLE [\"`]?[\\w\\d]+[\"`]?)(?: \\((.*)\\))?")
	sections := reg.FindStringSubmatch(sql)

	if sections == nil {
		return nil, errors.New("invalid DDL")
	}

	ddlHead := sections[1]
	ddlBody := sections[2]
	ddlBodyRunes := []rune(ddlBody)
	fields := []string{}

	bracketLevel := 0
	var quote rune = 0
	buf := ""

	for i := 0; i < len(ddlBodyRunes); i++ {
		c := ddlBodyRunes[i]
		var next rune = 0
		if i+1 < len(ddlBodyRunes) {
			next = ddlBodyRunes[i+1]
		}

		if c == '\'' || c == '"' || c == '`' {
			if c == next {
				// Skip escaped quote
				buf += string(c)
				i++
			} else if quote > 0 {
				quote = 0
			} else {
				quote = c
			}
		} else if quote == 0 {
			if c == '(' {
				bracketLevel++
			} else if c == ')' {
				bracketLevel--
			} else if bracketLevel == 0 {
				if c == ',' {
					fields = append(fields, strings.TrimSpace(buf))
					buf = ""
					continue
				}
			}
		}

		if bracketLevel < 0 {
			return nil, errors.New("invalid DDL, unbalanced brackets")
		}

		buf += string(c)
	}

	if bracketLevel != 0 {
		return nil, errors.New("invalid DDL, unbalanced brackets")
	}

	if buf != "" {
		fields = append(fields, strings.TrimSpace(buf))
	}

	return &ddl{head: ddlHead, fields: fields}, nil
}

func (d *ddl) compile() string {
	if len(d.fields) == 0 {
		return d.head
	}

	return fmt.Sprintf("%s (%s)", d.head, strings.Join(d.fields, ","))
}

func (d *ddl) addConstraint(name string, sql string) {
	reg := regexp.MustCompile("^CONSTRAINT [\"`]?" + regexp.QuoteMeta(name) + "[\"` ]")

	for i := 0; i < len(d.fields); i++ {
		if reg.MatchString(d.fields[i]) {
			d.fields[i] = sql
			return
		}
	}

	d.fields = append(d.fields, sql)
}

func (d *ddl) removeConstraint(name string) bool {
	reg := regexp.MustCompile("^CONSTRAINT [\"`]?" + regexp.QuoteMeta(name) + "[\"` ]")

	for i := 0; i < len(d.fields); i++ {
		if reg.MatchString(d.fields[i]) {
			d.fields = append(d.fields[:i], d.fields[i+1:]...)
			return true
		}
	}
	return false
}

func (d *ddl) hasConstraint(name string) bool {
	reg := regexp.MustCompile("^CONSTRAINT [\"`]?" + regexp.QuoteMeta(name) + "[\"` ]")

	for _, f := range d.fields {
		if reg.MatchString(f) {
			return true
		}
	}
	return false
}

func (d *ddl) getColumns() []string {
	res := []string{}

	for _, f := range d.fields {
		fUpper := strings.ToUpper(f)
		if strings.HasPrefix(fUpper, "PRIMARY KEY") ||
			strings.HasPrefix(fUpper, "CHECK") ||
			strings.HasPrefix(fUpper, "CONSTRAINT") {
			continue
		}

		reg := regexp.MustCompile("^[\"`]?([\\w\\d]+)[\"`]?")
		match := reg.FindStringSubmatch(f)

		if match != nil {
			res = append(res, "`"+match[1]+"`")
		}
	}
	return res
}
