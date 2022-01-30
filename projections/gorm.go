package projections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jinzhu/inflection"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
	weos "github.com/wepala/weos/model"
	"github.com/wepala/weos/utils"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//GORMProjection interface struct
type GORMProjection struct {
	db              *gorm.DB
	logger          weos.Log
	migrationFolder string
	Schema          map[string]weosContext.ContentType
}

func (p *GORMProjection) GetByKey(ctxt context.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
	if s, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		//pulling the primary keys from the schema in order to match with the keys given for searching
		scheme := s.Builder.Build().New()
		pks, _ := json.Marshal(contentType.Schema.Extensions["x-identifier"])
		primaryKeys := []string{}
		json.Unmarshal(pks, &primaryKeys)

		if len(primaryKeys) == 0 {
			primaryKeys = append(primaryKeys, "id")
		}

		if len(primaryKeys) != len(identifiers) {
			return nil, fmt.Errorf("%d keys provided for %s but there should be %d keys", len(identifiers), contentType.Name, len(primaryKeys))
		}

		for _, k := range primaryKeys {
			found := false
			for i, _ := range identifiers {
				if k == i {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("no value for %s %s found", contentType.Name, k)
			}
		}

		result := p.db.Table(contentType.Name).Scopes(ContentQuery()).Find(scheme, identifiers)
		if result.Error != nil {
			return nil, result.Error
		}
		data, err := json.Marshal(scheme)
		if err != nil {
			return nil, err
		}
		val := map[string]interface{}{}
		json.Unmarshal(data, &val)
		return val, nil
	} else {
		return nil, fmt.Errorf("no content type '%s' exists", contentType.Name)
	}
}

func (p *GORMProjection) GetByEntityID(ctxt context.Context, contentType weosContext.ContentType, id string) (map[string]interface{}, error) {
	if s, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		scheme := s.Builder.Build().New()
		result := p.db.Table(contentType.Name).Scopes(ContentQuery()).Find(scheme, "weos_id = ?", id)

		if result.Error != nil {
			return nil, result.Error
		}
		data, err := json.Marshal(scheme)
		if err != nil {
			return nil, err
		}
		val := map[string]interface{}{}
		json.Unmarshal(data, &val)
		return val, nil
	} else {
		return nil, fmt.Errorf("no content type '%s' exists", contentType.Name)
	}
}

//Persist save entity information in database
func (p *GORMProjection) Persist(entities []weos.Entity) error {
	return nil
}

//Remove entity
func (p *GORMProjection) Remove(entities []weos.Entity) error {
	return nil
}

func (p *GORMProjection) Migrate(ctx context.Context) error {

	//we may need to reorder the creation so that tables don't reference things that don't exist as yet.
	var err error
	var tables []interface{}
	for name, s := range p.Schema {
		f := s.Builder.GetField("Table")
		f.SetTag(`json:"table_alias" gorm:"default:` + name + `"`)
		instance := s.Builder.Build().New()
		err := json.Unmarshal([]byte(`{
			"table_alias": "`+name+`"
		}`), &instance)
		if err != nil {
			p.logger.Errorf("unable to set the table name '%s'", err)
			return err
		}

		dfs, _ := json.Marshal(s.Schema.Extensions["x-remove"])
		deletedFields := []string{}
		json.Unmarshal(dfs, &deletedFields)

		for i, f := range deletedFields {
			deletedFields[i] = utils.SnakeCase(f)
		}

		tables = append(tables, instance)
		columns, err := p.db.Migrator().ColumnTypes(instance)
		if err != nil {
			p.logger.Errorf("unable to get columns from table %s with error '%s'", name, err)
		}
		if len(columns) != 0 {
			reader := ds.NewReader(instance)
			readerFields := reader.GetAllFields()
			jsonFields := []string{}
			for _, r := range readerFields {
				jsonFields = append(jsonFields, utils.SnakeCase(r.Name()))
			}

			builder := ds.ExtendStruct(instance)
			for _, c := range columns {
				if !utils.Contains(jsonFields, c.Name()) && !utils.Contains(deletedFields, c.Name()) {
					if !utils.Contains(deletedFields, c.Name()) {
						var val interface{}
						dType := strings.ToLower(c.DatabaseTypeName())
						jsonString := `json:"` + c.Name() + `"`
						switch dType {
						case "text", "varchar", "char", "longtext":
							val = ""
							jsonString += `gorm:"size:512"`
						case "integer", "int8", "int", "smallint", "bigint":
							val = 0
						case "real", "float8", "numeric", "float4", "double", "decimal":
							val = 0.0
						case "bool", "boolean":
							val = false
						case "timetz", "timestamptz", "date", "datetime", "timestamp":
							val = time.Time{}
						}
						builder.AddField(strings.Title(c.Name()), val, jsonString)
					}
				}
			}

			constraintDeleted := false
			var deleteConstraintError error
			b := builder.Build().New()
			json.Unmarshal([]byte(`{
						"table_alias": "`+name+`"
					}`), &b)
			currDBPK := []string{}

			//get current primary keys
			if p.db.Dialector.Name() == "mysql" {
				db := p.db.Raw(fmt.Sprintf("SELECT COLUMN_NAME FROM %s WHERE TABLE_NAME = ? AND CONSTRAINT_NAME = ?", "INFORMATION_SCHEMA.KEY_COLUMN_USAGE"), name, "PRIMARY").Scan(&currDBPK)
				if db.Error != nil {
					p.logger.Errorf("got error getting primary keys for table '%s', %s", name, db.Error)
					return err
				}
			} else if p.db.Dialector.Name() == "postgres" {
				db := p.db.Raw(fmt.Sprintf(`SELECT c.column_name
				FROM information_schema.table_constraints tc 
				JOIN information_schema.constraint_column_usage AS ccu USING (constraint_schema, constraint_name) 
				JOIN information_schema.columns AS c ON c.table_schema = tc.constraint_schema
				  AND tc.table_name = c.table_name AND ccu.column_name = c.column_name
				WHERE constraint_type = 'PRIMARY KEY' and tc.table_name = '%s';
				`, name)).Scan(&currDBPK)
				if db.Error != nil {
					p.logger.Errorf("got error getting primary keys for table '%s', %s", name, db.Error)
					return err
				}
			}

			//if column exists in table but not in new schema, alter column
			for _, c := range columns {
				if !utils.Contains(jsonFields, c.Name()) {
					if p.db.Dialector.Name() == "sqlite" {
						//cannot check for nullable in sqlite.  if we are unable to alter the field, remake table
						deleteConstraintError = p.db.Migrator().AlterColumn(b, c.Name())
						if deleteConstraintError != nil {
							p.logger.Errorf("got error removing null column %s", deleteConstraintError)

						}
					} else {
						if nullable, ok := c.Nullable(); ok {
							if !nullable {
								for _, keyString := range currDBPK {
									//if primary key changed, remake table and relations
									if strings.EqualFold(keyString, c.Name()) {
										constraintDeleted = true
										break
									}
								}
								if !constraintDeleted {
									//remove constraint
									if p.db.Dialector.Name() == "mysql" {
										//gorm tags being overwritten works for mysql
										err = p.db.Debug().Migrator().AlterColumn(b, c.Name())
										if err != nil {
											p.logger.Errorf("got error removing null column %s", err)
											return err
										}
									} else if p.db.Dialector.Name() == "postgres" {
										//explicit constraint dropping for postgres
										db := p.db.Debug().Exec(fmt.Sprintf(`ALTER TABLE "%s" ALTER COLUMN "%s" DROP NOT NULL;`, name, c.Name()))
										if db.Error != nil {
											p.logger.Errorf("got error removing null column %s", db.Error)
											return db.Error
										}
									}
								}
							}
						}
					}

				}
			}

			//drop columns with x-remove tag
			for _, f := range deletedFields {
				if p.db.Migrator().HasColumn(instance, f) {
					deleteConstraintError = p.db.Migrator().DropColumn(instance, f)
					if deleteConstraintError != nil {
						p.logger.Errorf("unable to drop column %s from table %s with error '%s'", f, name, err)
						if p.db.Dialector.Name() != "sqlite" {
							return deleteConstraintError
						}
					}
				} else {
					p.logger.Errorf("unable to drop column %s from table %s.  property does not exist", f, name)
				}
			}

			//remake table if primary key constraints are changed
			if constraintDeleted || deleteConstraintError != nil {

				//check if changing primary key affects relationship tables
				tables, err := p.db.Migrator().GetTables()
				if err != nil {
					p.logger.Errorf("got error getting current tables %s", err)
					return err
				}

				//check foreign keys
				for _, t := range tables {
					pluralName := strings.ToLower(inflection.Plural(name))

					if strings.Contains(strings.ToLower(t), name+"_") || strings.Contains(t, "_"+pluralName) {
						return weos.NewError(fmt.Sprintf("a relationship exists that uses constraints from table %s", name), fmt.Errorf("a relationship exists that uses constraints from table %s", name))
					}
				}

				b := builder.Build().New()
				err = json.Unmarshal([]byte(`{
						"table_alias": "temp"
					}`), &b)
				if err != nil {
					p.logger.Errorf("unable to set the table name '%s'", err)
					return err
				}
				err = p.db.Migrator().CreateTable(b)
				if err != nil {
					p.logger.Errorf("got error creating temporary table %s", err)
					return err
				}
				tableVals := []map[string]interface{}{}
				p.db.Table(name).Find(&tableVals)
				if len(tableVals) != 0 {
					db := p.db.Table("temp").Create(&tableVals)
					if db.Error != nil {
						p.logger.Errorf("got error transfering table values %s", db.Error)
						return err
					}
				}

				err = p.db.Migrator().DropTable(name)
				if err != nil {
					p.logger.Errorf("got error dropping table%s", err)
					return err
				}
				err = p.db.Migrator().RenameTable("temp", name)
				if err != nil {
					p.logger.Errorf("got error renaming temporary table %s", err)
					return err
				}

			}
		}
	}
	err = p.db.Migrator().AutoMigrate(tables...)
	return err
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) {
		switch event.Type {
		case "create":
			contentType := weosContext.GetContentType(ctx)
			//using the schema ensures no nested fields are left out in creation
			payload, ok := p.Schema[strings.Title(contentType.Name)]
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {
				eventPayload := payload.Builder.Build().New()
				mapPayload := map[string]interface{}{}
				err := json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}
				mapPayload["sequence_no"] = event.Meta.SequenceNo

				bytes, _ := json.Marshal(mapPayload)
				err = json.Unmarshal(bytes, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}
				db := p.db.Table(contentType.Name).Create(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", contentType.Name, db.Error)
				}
			}
		case "update":
			contentType := weosContext.GetContentType(ctx)
			payload, ok := p.Schema[strings.Title(contentType.Name)]
			mapPayload := map[string]interface{}{}
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {
				eventPayload := payload.Builder.Build().New()
				err := json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}

				//set sequence number
				mapPayload["sequence_no"] = event.Meta.SequenceNo

				bytes, _ := json.Marshal(mapPayload)
				err = json.Unmarshal(bytes, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}

				reader := ds.NewReader(eventPayload)

				//replace associations
				for key, entity := range mapPayload {
					//many to many association
					if _, ok := entity.([]interface{}); ok {
						field := reader.GetField(strings.Title(key))
						err = p.db.Model(eventPayload).Association(strings.Title(key)).Replace(field.Interface())
						if err != nil {
							p.logger.Errorf("error clearing association %s for %s, got %s", strings.Title(key), contentType.Name, err)
						}
					}
				}

				//update database value
				db := p.db.Table(contentType.Name).Updates(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", contentType.Name, db.Error)
				}
			}

		}
	}
}

func (p *GORMProjection) GetContentEntity(ctx context.Context, weosID string) (*weos.ContentEntity, error) {
	contentType := weosContext.GetContentType(ctx)

	output := map[string]interface{}{}
	result := p.db.Table(strings.Title(strings.Title(contentType.Name))).Find(&output, "weos_id = ? ", weosID)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retreiving created blog, got: '%s'", result.Error)
	}

	payload, err := json.Marshal(output)
	if err != nil {
		p.logger.Errorf("unexpected error marshalling payload, got: '%s'", err)
	}

	newEntity, err := new(weos.ContentEntity).FromSchema(ctx, contentType.Schema)
	if err != nil {
		p.logger.Errorf("unexpected error creating entity, got: '%s'", err)
	}

	err = json.Unmarshal(payload, &newEntity.BasicEntity)
	if err != nil {
		p.logger.Errorf("unexpected error unmarshalling entity, got: '%s'", err)
	}
	err = json.Unmarshal(payload, &newEntity.Property)
	if err != nil {
		p.logger.Errorf("unexpected error unmarshalling entity, got: '%s'", err)
	}
	if output["sequence_no"] != nil {
		newEntity.SequenceNo = output["sequence_no"].(int64)
	}
	return newEntity, nil
}

//GetContentEntities returns a list of content entities as well as the total found
func (p *GORMProjection) GetContentEntities(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	var count int64
	var result *gorm.DB
	var schemes interface{}
	contentType := weosContext.GetContentType(ctx)
	if s, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		schemes = s.Builder.Build().NewSliceOfStructs()
		scheme := s.Builder.Build().New()

		result = p.db.Table(contentType.Name).Scopes(ContentQuery()).Model(scheme).Omit("weos_id, sequence_no, table").Count(&count).Scopes(paginate(page, limit), sort(sortOptions)).Find(schemes)
	}
	bytes, err := json.Marshal(schemes)
	if err != nil {
		return nil, 0, err
	}
	var entities []map[string]interface{}
	json.Unmarshal(bytes, &entities)
	return entities, count, result.Error
}

//paginate to query results
func paginate(page int, limit int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		actualLimit := limit
		actualPage := page
		if actualLimit == 0 {
			actualLimit = -1
		}
		if actualPage == 0 {
			actualPage = 1
		}
		return db.Offset((page - 1) * limit).Limit(actualLimit)
	}
}

// function that sorts the query results
func sort(order map[string]string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		for key, value := range order {
			//only support certain values since GORM doesn't protect the order function https://gorm.io/docs/security.html#SQL-injection-Methods
			if (value != "asc" && value != "desc" && value != "") || (key != "id") {
				return db
			}
			db.Order(key + " " + value)
		}

		return db
	}
}

//query modifier for making queries to the database
type QueryModifier func() func(db *gorm.DB) *gorm.DB

var ContentQuery QueryModifier

//NewProjection creates an instance of the projection
func NewProjection(ctx context.Context, application weos.Service, schemas map[string]weosContext.ContentType) (*GORMProjection, error) {

	projection := &GORMProjection{
		db:     application.DB(),
		logger: application.Logger(),
		Schema: schemas,
	}
	application.AddProjection(projection)

	ContentQuery = func() func(db *gorm.DB) *gorm.DB {
		return func(db *gorm.DB) *gorm.DB {
			if projection.db.Dialector.Name() == "sqlite" {
				//gorm sqlite generates the query incorrectly if there are composite keys when preloading
				//https://github.com/go-gorm/gorm/issues/3585
				return db
			} else {
				return db.Preload(clause.Associations, func(tx *gorm.DB) *gorm.DB { return tx.Omit("weos_id, sequence_no, table") })
			}
		}
	}
	return projection, nil
}
