package dialects

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	dynamicstruct "github.com/ompluscator/dynamic-struct"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Migrator struct {
	migrator.Migrator
}

// AutoMigrate
func (m Migrator) AutoMigrate(values ...interface{}) error {
	for _, value := range m.ReorderModels(values, true) {
		tx := m.DB.Session(&gorm.Session{})
		if !tx.Migrator().HasTable(value) {
			if err := tx.Migrator().CreateTable(value); err != nil {
				return err
			}
		} else {
			if err := m.RunWithValue(value, func(stmt *gorm.Statement) (errr error) {

				s := map[string]interface{}{}
				b, _ := json.Marshal(value)
				json.Unmarshal(b, &s)

				if tableName, ok := s["table_alias"].(string); ok {
					if tableName != "" {
						value = tableName
					}
				}

				columnTypes, _ := m.DB.Migrator().ColumnTypes(value)

				for _, field := range stmt.Schema.FieldsByDBName {
					var foundColumn gorm.ColumnType

					for _, columnType := range columnTypes {
						if columnType.Name() == field.DBName {
							foundColumn = columnType
							break
						}
					}

					if foundColumn == nil {
						// not found, add column
						if err := tx.Migrator().AddColumn(value, field.DBName); err != nil {
							return err
						}
					} else if err := m.DB.Migrator().MigrateColumn(value, field, foundColumn); err != nil {
						// found, smart migrate
						return err
					}
				}

				for _, rel := range stmt.Schema.Relationships.Relations {
					if !m.DB.Config.DisableForeignKeyConstraintWhenMigrating {
						if constraint := rel.ParseConstraint(); constraint != nil &&
							constraint.Schema == stmt.Schema && !tx.Migrator().HasConstraint(value, constraint.Name) {
							if err := tx.Migrator().CreateConstraint(value, constraint.Name); err != nil {
								return err
							}
						}
					}

					for _, chk := range stmt.Schema.ParseCheckConstraints() {
						if !tx.Migrator().HasConstraint(value, chk.Name) {
							if err := tx.Migrator().CreateConstraint(value, chk.Name); err != nil {
								return err
							}
						}
					}
				}

				for _, idx := range stmt.Schema.ParseIndexes() {
					if !tx.Migrator().HasIndex(value, idx.Name) {
						if err := tx.Migrator().CreateIndex(value, idx.Name); err != nil {
							return err
						}
					}
				}

				return nil
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m Migrator) RunWithValue(value interface{}, fc func(*gorm.Statement) error) error {
	stmt := &gorm.Statement{DB: m.DB}
	if m.DB.Statement != nil {
		stmt.Table = m.DB.Statement.Table
		stmt.TableExpr = m.DB.Statement.TableExpr
	}

	//check if table is a dynamic struct that has a field called Table and use that instead
	reader := dynamicstruct.NewReader(value)
	if reader != nil && reader.GetField("Table") != nil && reader.GetField("Table").String() != "" {
		stmt.Table = reader.GetField("Table").String()
	}

	if table, ok := value.(string); ok {
		stmt.Table = table
	} else if err := stmt.ParseWithSpecialTableName(value, stmt.Table); err != nil {
		fmt.Printf("error creating table '%s'", err)
		return err
	}

	return fc(stmt)
}

func (m Migrator) CreateTable(values ...interface{}) error {
	for _, value := range m.ReorderModels(values, false) {
		tx := m.DB.Session(&gorm.Session{})
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) (errr error) {
			var (
				createTableSQL          = "CREATE TABLE ? ("
				values                  = []interface{}{m.CurrentTable(stmt)}
				hasPrimaryKeyInDataType bool
			)

			for _, dbName := range stmt.Schema.DBNames {
				field := stmt.Schema.FieldsByDBName[dbName]
				if !field.IgnoreMigration {
					createTableSQL += "? ?"
					hasPrimaryKeyInDataType = hasPrimaryKeyInDataType || strings.Contains(strings.ToUpper(string(field.DataType)), "PRIMARY KEY")
					values = append(values, clause.Column{Name: dbName}, m.DB.Migrator().FullDataTypeOf(field))
					createTableSQL += ","
				}
			}

			if !hasPrimaryKeyInDataType && len(stmt.Schema.PrimaryFields) > 0 {
				createTableSQL += "PRIMARY KEY ?,"
				primaryKeys := []interface{}{}
				for _, field := range stmt.Schema.PrimaryFields {
					primaryKeys = append(primaryKeys, clause.Column{Name: field.DBName})
				}

				values = append(values, primaryKeys)
			}

			for _, idx := range stmt.Schema.ParseIndexes() {
				if m.CreateIndexAfterCreateTable {
					defer func(value interface{}, name string) {
						if errr == nil {
							errr = tx.Migrator().CreateIndex(value, name)
						}
					}(value, idx.Name)
				} else {
					if idx.Class != "" {
						createTableSQL += idx.Class + " "
					}
					createTableSQL += "INDEX ? ?"

					if idx.Comment != "" {
						createTableSQL += fmt.Sprintf(" COMMENT '%s'", idx.Comment)
					}

					if idx.Option != "" {
						createTableSQL += " " + idx.Option
					}

					createTableSQL += ","
					values = append(values, clause.Expr{SQL: idx.Name}, tx.Migrator().(migrator.BuildIndexOptionsInterface).BuildIndexOptions(idx.Fields, stmt))
				}
			}

			for _, rel := range stmt.Schema.Relationships.Relations {
				if !m.DB.DisableForeignKeyConstraintWhenMigrating {
					if constraint := rel.ParseConstraint(); constraint != nil {
						if constraint.Schema == stmt.Schema {
							sql, vars := buildConstraint(constraint)
							createTableSQL += sql + ","
							values = append(values, vars...)
						}
					}
				}
			}

			for _, chk := range stmt.Schema.ParseCheckConstraints() {
				createTableSQL += "CONSTRAINT ? CHECK (?),"
				values = append(values, clause.Column{Name: chk.Name}, clause.Expr{SQL: chk.Constraint})
			}

			createTableSQL = strings.TrimSuffix(createTableSQL, ",")

			createTableSQL += ")"

			if tableOption, ok := m.DB.Get("gorm:table_options"); ok {
				createTableSQL += fmt.Sprint(tableOption)
			}

			errr = tx.Exec(createTableSQL, values...).Error
			return errr
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	for i := len(values) - 1; i >= 0; i-- {
		tx := m.DB.Session(&gorm.Session{})
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return tx.Exec("DROP TABLE IF EXISTS ?", m.CurrentTable(stmt)).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) HasTable(value interface{}) bool {
	var count int64

	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		currentDatabase := m.DB.Migrator().CurrentDatabase()
		return m.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ? AND table_type = ?", currentDatabase, stmt.Table, "BASE TABLE").Row().Scan(&count)
	})

	return count > 0
}

func (m Migrator) RenameTable(oldName, newName interface{}) error {
	var oldTable, newTable interface{}
	if v, ok := oldName.(string); ok {
		oldTable = clause.Table{Name: v}
	} else {
		stmt := &gorm.Statement{DB: m.DB}
		if err := stmt.Parse(oldName); err == nil {
			oldTable = m.CurrentTable(stmt)
		} else {
			return err
		}
	}

	if v, ok := newName.(string); ok {
		newTable = clause.Table{Name: v}
	} else {
		stmt := &gorm.Statement{DB: m.DB}
		if err := stmt.Parse(newName); err == nil {
			newTable = m.CurrentTable(stmt)
		} else {
			return err
		}
	}

	return m.DB.Exec("ALTER TABLE ? RENAME TO ?", oldTable, newTable).Error
}

func buildConstraint(constraint *schema.Constraint) (sql string, results []interface{}) {
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

// ReorderModels reorder models according to constraint dependencies
func (m Migrator) ReorderModels(values []interface{}, autoAdd bool) (results []interface{}) {
	type Dependency struct {
		*gorm.Statement
		Depends []*schema.Schema
	}

	var (
		modelNames, orderedModelNames []string
		orderedModelNamesMap          = map[string]bool{}
		parsedSchemas                 = map[*schema.Schema]bool{}
		valuesMap                     = map[string]Dependency{}
		insertIntoOrderedList         func(name string)
		parseDependence               func(value interface{}, addToList bool)
	)

	parseDependence = func(value interface{}, addToList bool) {
		dep := Dependency{
			Statement: &gorm.Statement{DB: m.DB, Dest: value},
		}
		beDependedOn := map[*schema.Schema]bool{}

		//This originally was using parse but we need to pass in the table name based on what we have set
		s := map[string]interface{}{}
		b, _ := json.Marshal(value)
		json.Unmarshal(b, &s)
		if tableName, ok := s["table_alias"].(string); ok {
			if err := dep.ParseWithSpecialTableName(value, tableName); err != nil {
				m.DB.Logger.Error(context.Background(), "failed to parse value %#v, got error %v", value, err)
			}
		} else {
			if err := dep.Parse(value); err != nil {
				m.DB.Logger.Error(context.Background(), "failed to parse value %#v, got error %v", value, err)
			}
		}

		if _, ok := parsedSchemas[dep.Statement.Schema]; ok {
			return
		}

		parsedSchemas[dep.Statement.Schema] = true

		for _, rel := range dep.Schema.Relationships.Relations {
			if c := rel.ParseConstraint(); c != nil && c.Schema == dep.Statement.Schema && c.Schema != c.ReferenceSchema {
				dep.Depends = append(dep.Depends, c.ReferenceSchema)
			}

			if rel.Type == schema.HasOne || rel.Type == schema.HasMany {
				beDependedOn[rel.FieldSchema] = true
			}

			if rel.JoinTable != nil {
				// append join value
				defer func(rel *schema.Relationship, joinValue interface{}) {
					if !beDependedOn[rel.FieldSchema] {
						dep.Depends = append(dep.Depends, rel.FieldSchema)
					} else {
						fieldValue := reflect.New(rel.FieldSchema.ModelType).Interface()
						parseDependence(fieldValue, autoAdd)
					}
					parseDependence(joinValue, autoAdd)
				}(rel, reflect.New(rel.JoinTable.ModelType).Interface())
			}
		}

		valuesMap[dep.Schema.Table] = dep

		if addToList {
			modelNames = append(modelNames, dep.Schema.Table)
		}
	}

	insertIntoOrderedList = func(name string) {
		if _, ok := orderedModelNamesMap[name]; ok {
			return // avoid loop
		}
		orderedModelNamesMap[name] = true

		if autoAdd {
			dep := valuesMap[name]
			for _, d := range dep.Depends {
				if _, ok := valuesMap[d.Table]; ok {
					insertIntoOrderedList(d.Table)
				} else {
					parseDependence(reflect.New(d.ModelType).Interface(), autoAdd)
					insertIntoOrderedList(d.Table)
				}
			}
		}

		orderedModelNames = append(orderedModelNames, name)
	}

	for _, value := range values {
		if v, ok := value.(string); ok {
			results = append(results, v)
		} else {
			parseDependence(value, true)
		}
	}

	for _, name := range modelNames {
		insertIntoOrderedList(name)
	}

	for _, name := range orderedModelNames {
		results = append(results, valuesMap[name].Statement.Dest)
	}

	if len(values) != len(modelNames) {
		fmt.Printf("expected %d models, got %d \n", len(values), len(modelNames))
	}
	return
}
