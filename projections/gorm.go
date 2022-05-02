package projections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jinzhu/inflection"
	ds "github.com/ompluscator/dynamic-struct"
	weos "github.com/wepala/weos/model"
	"github.com/wepala/weos/utils"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//GORMDB interface struct
type GORMDB struct {
	db              *gorm.DB
	logger          weos.Log
	migrationFolder string
	Schema          map[string]ds.Builder
}

type FilterProperty struct {
	Field    string        `json:"field"`
	Operator string        `json:"operator"`
	Value    interface{}   `json:"value"`
	Values   []interface{} `json:"values"`
}

func (p *GORMDB) DB() *gorm.DB {
	return p.db
}

func (p *GORMDB) GetByKey(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) (map[string]interface{}, error) {
	contentEntity, err := entityFactory.NewEntity(ctxt)
	if err != nil {
		return nil, err
	}
	scheme, err := contentEntity.GORMModel(ctxt)
	//pulling the primary keys from the schema in order to match with the keys given for searching
	pks, _ := json.Marshal(contentEntity.Schema.Extensions["x-identifier"])
	primaryKeys := []string{}
	json.Unmarshal(pks, &primaryKeys)

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	if len(primaryKeys) != len(identifiers) {
		return nil, fmt.Errorf("%d keys provided for %s but there should be %d keys", len(identifiers), entityFactory.Name(), len(primaryKeys))
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
			return nil, fmt.Errorf("no value for %s %s found", entityFactory.Name(), k)
		}
	}

	result := p.db.Table(entityFactory.Name()).Scopes(ContentQuery()).Find(scheme, identifiers)
	if result.Error != nil {
		return nil, result.Error
	}
	data, err := json.Marshal(contentEntity.ToMap())
	if err != nil {
		return nil, err
	}
	val := map[string]interface{}{}
	json.Unmarshal(data, &val)
	return val, nil

}

func (p *GORMDB) GetByEntityID(ctx context.Context, entityFactory weos.EntityFactory, id string) (map[string]interface{}, error) {
	//scheme, err := entityFactory.NewEntity(ctx)
	tstruct := entityFactory.DynamicStruct(ctx).New()
	//if err != nil {
	//	return nil, err
	//}
	result := p.db.Table(entityFactory.Name()).Scopes(ContentQuery()).Find(tstruct, "weos_id = ?", id)

	if result.Error != nil {
		return nil, result.Error
	}
	data, err := json.Marshal(tstruct)
	if err != nil {
		return nil, err
	}
	val := map[string]interface{}{}
	json.Unmarshal(data, &val)
	return val, nil

}

//Persist save entity information in database
func (p *GORMDB) Persist(entities []weos.Entity) error {
	return nil
}

//Remove entity
func (p *GORMDB) Remove(entities []weos.Entity) error {
	return nil
}

func (p *GORMDB) Migrate(ctx context.Context, builders map[string]ds.Builder, deleted map[string][]string) error {

	//we may need to reorder the creation so that tables don't reference things that don't exist as yet.
	var err error
	var tables []interface{}
	for name, s := range builders {
		f := s.GetField("Table")
		f.SetTag(`json:"table_alias" gorm:"default:` + name + `"`)
		instance := s.Build().New()
		err := json.Unmarshal([]byte(`{
			"table_alias": "`+name+`"
		}`), &instance)
		if err != nil {
			p.logger.Errorf("unable to set the table name '%s'", err)
			return err
		}
		tables = append(tables, instance)

		var deletedFields []string
		deletedFields = deleted[name]

		for i, f := range deletedFields {
			deletedFields[i] = utils.SnakeCase(f)
		}

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
							var strings *string
							val = strings
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

			var deleteConstraintError error
			b := builder.Build().New()
			json.Unmarshal([]byte(`{
						"table_alias": "`+name+`"
					}`), &b)

			//drop columns with x-remove tag
			for _, f := range deletedFields {
				if p.db.Migrator().HasColumn(b, f) {

					deleteConstraintError = p.db.Migrator().DropColumn(b, f)
					if deleteConstraintError != nil {
						p.logger.Errorf("unable to drop column %s from table %s with error '%s'", f, name, err)
						break
					}
				} else {
					p.logger.Errorf("unable to drop column %s from table %s.  property does not exist", f, name)
				}
			}

			//get columns after db drop
			columns, err = p.db.Migrator().ColumnTypes(instance)
			if err != nil {
				p.logger.Errorf("unable to get columns from table %s with error '%s'", name, err)
			}
			//if column exists in table but not in new schema, alter column
			if deleteConstraintError == nil {
				for _, c := range columns {
					if !utils.Contains(jsonFields, c.Name()) {
						deleteConstraintError = p.db.Migrator().AlterColumn(b, c.Name())
						if deleteConstraintError != nil {
							p.logger.Errorf("got error updating constraint %s", err)
							break
						}
					}
				}
			}

			//remake table if primary key constraints are changed
			if deleteConstraintError != nil {

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

func (p *GORMDB) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) error {
		entityFactory := weos.GetEntityFactory(ctx)
		switch event.Type {
		case "create":
			//using the schema ensures no nested fields are left out in creation
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error get a copy of the entity '%s'", err)
					return err
				}
				eventPayload := entity.ToMap()
				mapPayload := map[string]interface{}{}
				err = json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
					return err
				}
				mapPayload["sequence_no"] = event.Meta.SequenceNo
				//Adding the entityid to the payload since the event payload doesnt have it
				mapPayload["weos_id"] = event.Meta.EntityID

				bytes, _ := json.Marshal(mapPayload)
				err = json.Unmarshal(bytes, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
					return err
				}

				db := p.db.Table(entityFactory.Name()).Create(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", entityFactory.Name(), db.Error)
					return db.Error
				}
			}
		case "update":
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
					return err
				}
				eventPayload := entity.ToMap()
				mapPayload := map[string]interface{}{}
				err = json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
					return err
				}

				//set sequence number
				mapPayload["sequence_no"] = event.Meta.SequenceNo

				bytes, _ := json.Marshal(mapPayload)
				err = json.Unmarshal(bytes, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
					return err
				}

				reader := ds.NewReader(eventPayload)

				//replace associations
				for key, entity := range mapPayload {
					//many to many association
					if _, ok := entity.([]interface{}); ok {
						field := reader.GetField(strings.Title(key))
						err = p.db.Model(eventPayload).Association(strings.Title(key)).Replace(field.Interface())
						if err != nil {
							p.logger.Errorf("error clearing association %s for %s, got %s", strings.Title(key), entityFactory.Name(), err)
							return err
						}
					}
				}

				//update database value
				db := p.db.Table(entityFactory.Name()).Updates(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", entityFactory.Name(), db.Error)
					return db.Error
				}
			}
		case "delete":
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
					return err
				}

				db := p.db.Table(entityFactory.Name()).Where("weos_id = ?", event.Meta.EntityID).Delete(entity.GORMModel(context.TODO()))
				if db.Error != nil {
					p.logger.Errorf("error deleting %s, got %s", entityFactory.Name(), db.Error)
					return db.Error
				}
			}
		}
		return nil
	}
}

func (p *GORMDB) GetContentEntity(ctx context.Context, entityFactory weos.EntityFactory, weosID string) (*weos.ContentEntity, error) {
	newEntity, err := entityFactory.NewEntity(ctx)
	if err != nil {
		return nil, err
	}

	result := p.db.Table(entityFactory.TableName()).Find(newEntity, "weos_id = ? ", weosID)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retrieving created blog, got: '%s'", result.Error)
	}
	//set result to entity
	rowData, err := json.Marshal(newEntity.ToMap())
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(rowData, &newEntity)
	if err != nil {
		return nil, err
	}

	//because we're unmarshallign to the property field directly the weos id and sequence no. is not being set on the entity itself. The ideal fix is to make a custom unmarshal routine for ContentEntity
	return newEntity, nil
}

//GetContentEntities returns a list of content entities as well as the total found
func (p *GORMDB) GetContentEntities(ctx context.Context, entityFactory weos.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	var count int64
	var result *gorm.DB
	var schemes interface{}
	if entityFactory == nil {
		return nil, int64(0), fmt.Errorf("no entity factory found")
	}
	var filtersProp map[string]FilterProperty
	props, _ := json.Marshal(filterOptions)
	json.Unmarshal(props, &filtersProp)
	filtersProp, err := DateTimeCheck(entityFactory, filtersProp)
	if err != nil {
		return nil, int64(0), err
	}
	builder := entityFactory.DynamicStruct(ctx)
	if builder != nil {
		schemes = builder.NewSliceOfStructs()
		scheme := builder.New()
		result = p.db.Table(entityFactory.Name()).Scopes(FilterQuery(filtersProp)).Model(&scheme).Omit("weos_id, sequence_no, table").Count(&count).Scopes(paginate(page, limit), sort(sortOptions)).Find(schemes)
	}
	bytes, err := json.Marshal(schemes)
	if err != nil {
		return nil, 0, err
	}
	var entities []map[string]interface{}
	json.Unmarshal(bytes, &entities)
	return entities, count, result.Error
}

func (p *GORMDB) GetByProperties(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) ([]map[string]interface{}, error) {
	results := entityFactory.Builder(ctxt).Build().NewSliceOfStructs()
	result := p.db.Table(entityFactory.TableName()).Scopes(ContentQuery()).Find(results, identifiers)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retrieving created blog, got: '%s'", result.Error)
	}
	bytes, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	var entities []map[string]interface{}
	json.Unmarshal(bytes, &entities)
	return entities, nil
}

//DateTimeChecks checks to make sure the format is correctly as well as it manipulates the date
func DateTimeCheck(entityFactory weos.EntityFactory, properties map[string]FilterProperty) (map[string]FilterProperty, error) {
	var err error
	schema := entityFactory.Schema()
	for key, value := range properties {
		if schema.Properties[key] != nil && schema.Properties[key].Value.Format == "date-time" {
			_, err := time.Parse(time.RFC3339, value.Value.(string))
			if err != nil {
				return nil, err
			}

		}
	}

	return properties, err
}

//paginate is used for querying results
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

//sort is used to sort the query results
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
type QueryFilterModifier func(options map[string]FilterProperty) func(db *gorm.DB) *gorm.DB

var ContentQuery QueryModifier
var FilterQuery QueryFilterModifier

//NewProjection creates an instance of the projection
func NewProjection(ctx context.Context, db *gorm.DB, logger weos.Log) (*GORMDB, error) {

	projection := &GORMDB{
		db:     db,
		logger: logger,
	}

	FilterQuery = func(options map[string]FilterProperty) func(db *gorm.DB) *gorm.DB {
		return func(db *gorm.DB) *gorm.DB {
			if options != nil {
				for _, filter := range options {
					operator := "="
					switch filter.Operator {
					case "gt":
						operator = ">"
					case "lt":
						operator = "<"
					case "ne":
						operator = "!="
					case "like":
						if projection.db.Dialector.Name() == "postgres" {
							operator = "ILIKE"
						} else {
							operator = " LIKE"
						}
					case "in":
						operator = "IN"

					}

					if len(filter.Values) == 0 {
						if filter.Operator == "like" {
							db.Where(utils.SnakeCase(filter.Field)+" "+operator+" ?", "%"+filter.Value.(string)+"%")
						} else {
							db.Where(utils.SnakeCase(filter.Field)+" "+operator+" ?", filter.Value)
						}

					} else {
						db.Where(utils.SnakeCase(filter.Field)+" "+operator+" ?", filter.Values)
					}

				}
			}
			return db
		}
	}

	ContentQuery = func() func(db *gorm.DB) *gorm.DB {
		return func(db *gorm.DB) *gorm.DB {
			if projection.db.Dialector.Name() == "sqlite" {
				//gorm sqlite generates the query incorrectly if there are composite keys when preloading.  This may cause panics.
				//https://github.com/go-gorm/gorm/issues/3585
				return db
			} else {
				return db.Preload(clause.Associations, func(tx *gorm.DB) *gorm.DB { return tx.Omit("weos_id, sequence_no, table") })
			}
		}
	}
	return projection, nil
}
