package projections

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"strconv"
	"strings"
	"time"

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
	//key interfaces for gorm models
	keys map[string]map[string]interface{}
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

func (p *GORMDB) GetByKey(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) (*weos.ContentEntity, error) {
	contentEntity, err := entityFactory.NewEntity(ctxt)
	if err != nil {
		return nil, err
	}
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

	model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), nil)

	result := p.db.Debug().Table(entityFactory.Name()).Preload(clause.Associations).Scopes(ContentQuery()).Find(&model, identifiers)
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, nil
	}

	data, err := json.Marshal(model)
	err = json.Unmarshal(data, &contentEntity)
	return contentEntity, err

}

//Deprecated: 06/05/2022 use GetContentEntity instead
//GetByEntityID get map of row using the entity id
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

func (p *GORMDB) Migrate(ctx context.Context, schema *openapi3.Swagger) error {

	var models []interface{}
	if schema != nil {
		for name, tschema := range schema.Components.Schemas {
			model, err := p.GORMModel(name, tschema.Value, nil)
			if err != nil {
				return err
			}
			json.Unmarshal([]byte(`{
						"table_alias": "`+name+`"
					}`), &model)
			models = append(models, model)
			//drop columns
			if p.db.Migrator().HasTable(model) {
				if columnsToRemove, ok := tschema.Value.Extensions["x-remove"]; ok {
					var columns []string
					err = json.Unmarshal(columnsToRemove.(json.RawMessage), &columns)
					if err != nil {
						return fmt.Errorf("x-remove should be a list of columns name to remove '%s'", err)
					}
					for _, column := range columns {
						if p.db.Migrator().HasColumn(model, column) {
							err = p.db.Migrator().DropColumn(model, column)
							if err != nil {
								return fmt.Errorf("could not remove column '%s'. if it is a primary key column try creating another primary key and then removing. original error '%s'", column, err)
							}
						}
					}
				}

			}
		}
	}

	err := p.db.Debug().Migrator().AutoMigrate(models...)
	return err
}

//GORMModel return gorm model that is generated recursively.
func (p *GORMDB) GORMModel(name string, schema *openapi3.Schema, payload []byte) (interface{}, error) {
	builder, _, err := p.GORMModelBuilder(name, schema)

	if err != nil {
		return nil, fmt.Errorf("unable to generate gorm model builder '%s'", err)
	}
	model := builder.Build().New()
	//if there is a payload let's serialize that
	if payload != nil {
		tpayload := make(map[string]interface{})
		err = json.Unmarshal(payload, &tpayload)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal payload into model '%s'", err)
		}
		tpayload["table_alias"] = name
		data, _ := json.Marshal(tpayload)
		err = json.Unmarshal(data, &model)
	}

	return model, nil
}

func (p *GORMDB) GORMModelBuilder(name string, ref *openapi3.Schema) (ds.Builder, map[string]interface{}, error) {
	titleCaseName := cases.Title(language.English).String(name)
	//get the builder from "cache". This is to avoid issues with the gorm cache that uses the model interface to create a cache key
	if builder, ok := p.Schema[titleCaseName]; ok {
		return builder, p.keys[titleCaseName], nil
	}

	pks, _ := json.Marshal(ref.Extensions["x-identifier"])
	dfs, _ := json.Marshal(ref.Extensions["x-remove"])

	primaryKeys := []string{}
	deletedFields := []string{}
	//this is used to store the default values of the primary keys so that the foreign key relationships can be setup
	primaryKeysMap := make(map[string]interface{})

	json.Unmarshal(pks, &primaryKeys)
	json.Unmarshal(dfs, &deletedFields)

	//was a primary key removed but not removed in the x-identifier fields?
	for i, k := range primaryKeys {
		for _, d := range deletedFields {
			if strings.EqualFold(k, d) {
				if len(primaryKeys) == 1 {
					primaryKeys = []string{}
				} else {
					primaryKeys[i] = primaryKeys[len(primaryKeys)-1]
					primaryKeys = primaryKeys[:len(primaryKeys)-1]
				}
			}
		}
	}

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}
	instance := ds.NewStruct()
	//add default weos_id field
	instance.AddField("WeosID", "", `json:"weos_id" gorm:"unique;<-:create"`)
	instance.AddField("SequenceNo", uint(0), `json:"sequence_no"`)
	//add table field so that it works with gorm functions that try to fetch the name.
	//It's VERY important that the gorm default is set for this (spent hours trying to figure out why table names wouldn't show for related entities)
	instance.AddField("Table", cases.Title(language.English).String(name), `json:"table_alias" gorm:"default:`+cases.Title(language.English).String(name)+`"`)
	for tname, prop := range ref.Properties {
		found := false

		for _, n := range deletedFields {
			if strings.EqualFold(n, tname) {
				found = true
			}
		}
		//this field should not be added to the schema
		if found {
			continue
		}

		tagString := `json:"` + tname + `"`
		var gormParts []string
		for _, req := range ref.Required {
			if strings.EqualFold(req, tname) {
				gormParts = append(gormParts, "NOT NULL")
			}
		}

		uniquebytes, _ := json.Marshal(prop.Value.Extensions["x-unique"])
		if len(uniquebytes) != 0 {
			unique := false
			json.Unmarshal(uniquebytes, &unique)
			if unique {
				gormParts = append(gormParts, "unique")
			}
		}

		if strings.Contains(strings.Join(primaryKeys, " "), strings.ToLower(tname)) {
			gormParts = append(gormParts, "primaryKey", "size:512")
			//only add NOT null if it's not already in the array to avoid issue if a user also add the field to the required array
			if !strings.Contains(strings.Join(gormParts, ";"), "NOT NULL") {
				gormParts = append(gormParts, "NOT NULL")
			}
			//if the property is part of a key then it should not be nullable (this causes issues when generating the model for gorm)
			prop.Value.Nullable = false
		}

		defaultValue, gormParts, valueKeys := p.GORMPropertyDefaultValue(name, tname, prop, gormParts)

		//setup gorm field tag string
		if len(gormParts) > 0 {
			gormString := strings.Join(gormParts, ";")
			tagString += ` gorm:"` + gormString + `"`
		}

		instance.AddField(cases.Title(language.English).String(tname), defaultValue, tagString)

		//if there are value keys it's because there is a Belongs to relationship and we need to add properties for that to work with GORM https://gorm.io/docs/belongs_to.html
		if len(valueKeys) > 0 {
			for keyName, tdefaultValue := range valueKeys {
				keyNameTitleCase := cases.Title(language.English).String(tname) + cases.Title(language.English).String(keyName)
				//convert the type to a pointer so that the foreign key relationship will not be required (otherwise the debug will show that an item with a foreign key relationship saved but in reality it didn't)
				var defaultValuePointer interface{}
				switch tdefaultValue.(type) {
				case string:
					var tpointer *string
					defaultValuePointer = tpointer
				case uint:
					var tpointer *uint
					defaultValuePointer = tpointer
				case float32:
					var tpointer *float32
					defaultValuePointer = tpointer
				case float64:
					var tpointer *float64
					defaultValuePointer = tpointer
				case int:
					var tpointer *int
					defaultValuePointer = tpointer
				case int32:
					var tpointer *int32
					defaultValuePointer = tpointer
				case int64:
					var tpointer *int64
					defaultValuePointer = tpointer
				}
				instance.AddField(keyNameTitleCase, defaultValuePointer, `json:"-"`)
			}
		}
		if weos.InList(primaryKeys, tname) {
			primaryKeysMap[tname] = defaultValue
		}
	}
	if len(primaryKeys) == 1 && primaryKeys[0] == "id" && !instance.HasField("Id") {
		instance.AddField("Id", uint(0), `json:"id" gorm:"primaryKey;size:512"`)
		primaryKeysMap["Id"] = uint(0)
	}

	//add to "cache"
	p.Schema[titleCaseName] = instance
	p.keys[titleCaseName] = primaryKeysMap

	return instance, primaryKeysMap, nil
}

func (p *GORMDB) GORMPropertyDefaultValue(parentName string, name string, schema *openapi3.SchemaRef, gormParts []string) (interface{}, []string, map[string]interface{}) {
	var defaultValue interface{}
	if schema.Value != nil {
		switch schema.Value.Type {
		case "integer":
			switch schema.Value.Format {
			case "int32":
				if schema.Value.Nullable {
					var value *int32
					defaultValue = value
				} else {
					var value int32
					defaultValue = value
				}
			case "int64":
				if schema.Value.Nullable {
					var value *int64
					defaultValue = value
				} else {
					var value int64
					defaultValue = value
				}
			case "uint":
				if schema.Value.Nullable {
					var value *uint
					defaultValue = value
				} else {
					var value uint
					defaultValue = value
				}
			default:
				if schema.Value.Nullable {
					var value *int
					defaultValue = value
				} else {
					var value int
					defaultValue = value
				}
			}
		case "number":
			switch schema.Value.Format {
			case "float32":
				if schema.Value.Nullable {
					var value *float32
					defaultValue = value
				} else {
					var value float32
					defaultValue = value
				}
			case "float64":
				if schema.Value.Nullable {
					var value *float64
					defaultValue = value
				} else {
					var value float64
					defaultValue = value
				}
			default:
				if schema.Value.Nullable {
					var value *float32
					defaultValue = value
				} else {
					var value float32
					defaultValue = value
				}
			}

		case "string":
			switch schema.Value.Format {
			case "date-time":
				timeNow := weos.NewTime(time.Now())
				defaultValue = &timeNow
			default:
				if schema.Value.Nullable {
					var strings *string
					defaultValue = strings
				} else {
					var strings string
					defaultValue = strings
				}
			}
		case "array":
			if schema.Value != nil && schema.Value.Items != nil && schema.Value.Items.Value != nil {
				tbuilder, _, err := p.GORMModelBuilder(strings.Replace(schema.Value.Items.Ref, "#/components/schemas/", "", -1), schema.Value.Items.Value)
				if err != nil {
					return nil, nil, nil
				}
				defaultValue = tbuilder.Build().NewSliceOfStructs()
				json.Unmarshal([]byte(`[{
						"table_alias": "`+cases.Title(language.English).String(name)+`"
					}]`), &defaultValue)
				//setup gorm field tag string
				gormParts = append(gormParts, "many2many:"+utils.SnakeCase(parentName)+"_"+utils.SnakeCase(name))
			}
		default:
			//Belongs to https://gorm.io/docs/belongs_to.html
			if schema.Ref != "" && schema.Value != nil {
				tbuilder, keys, err := p.GORMModelBuilder(name, schema.Value)
				if err != nil {
					return nil, nil, nil
				}
				//setup key for rthe gorm tag
				keyNames := []string{}
				foreignKeys := []string{}
				for v, _ := range keys {
					keyNames = append(keyNames, v)
				}
				for _, v := range keyNames {
					foreignKeys = append(foreignKeys, cases.Title(language.English).String(name)+cases.Title(language.English).String(v))
				}
				defaultValue = tbuilder.Build().New()
				json.Unmarshal([]byte(`{
						"table_alias": "`+cases.Title(language.English).String(name)+`"
					}`), &defaultValue)
				gormParts = append(gormParts, "foreignKey:"+strings.Join(foreignKeys, ","))
				gormParts = append(gormParts, "References:"+strings.Join(keyNames, ","))
				return defaultValue, gormParts, keys
			}
			//TODO I think here is where I'd put code to setup a json blob
		}

	}
	return defaultValue, gormParts, nil
}

func (p *GORMDB) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) error {
		entityFactory := weos.GetEntityFactory(ctx)
		switch event.Type {
		case "create":
			//using the schema ensures no nested fields are left out in creation
			if entityFactory != nil {
				entity, err := entityFactory.CreateEntityWithValues(ctx, event.Payload)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
					return err
				}
				entity.SequenceNo = event.Meta.SequenceNo
				//Adding the entityid to the payload since the event payload doesn't have it
				entity.ID = event.Meta.EntityID
				payload, err := json.Marshal(entity.ToMap())
				model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), payload)
				json.Unmarshal([]byte(`{"weos_id":"`+entity.ID+`","sequence_no":`+strconv.Itoa(int(entity.SequenceNo))+`}`), &model)
				db := p.db.Debug().Table(entityFactory.Name()).Create(model)
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
				entity.ID = event.Meta.EntityID
				err = json.Unmarshal(event.Payload, &entity)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
					return err
				}
				entity.SequenceNo = event.Meta.SequenceNo
				payload, err := json.Marshal(entity)
				model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), payload)
				json.Unmarshal([]byte(`{"weos_id":"`+entity.ID+`","sequence_no":"`+strconv.Itoa(int(entity.SequenceNo))+`"}`), &model)
				reader := ds.NewReader(model)

				//replace associations
				for key, property := range entityFactory.Schema().Properties {
					//check to see if the property is an array with items defined that is a reference to another schema (inline array will be stored as json in the future)
					if property.Value != nil && property.Value.Type == "array" && property.Value.Items != nil && property.Value.Items.Ref != "" {
						field := reader.GetField(strings.Title(key))
						err = p.db.Debug().Model(model).Association(strings.Title(key)).Replace(field.Interface())
						if err != nil {
							p.logger.Errorf("error clearing association %s for %s, got %s", strings.Title(key), entityFactory.Name(), err)
							return err
						}
					}
				}

				//update database value
				db := p.db.Debug().Table(entityFactory.Name()).Updates(model)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", entityFactory.Name(), db.Error)
					return db.Error
				}
			}
		case "delete":
			if entityFactory != nil {
				model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), nil)
				if err != nil {
					p.logger.Errorf("error generating entity model '%s'", err)
					return err
				}
				db := p.db.Debug().Table(entityFactory.Name()).Where("weos_id = ?", event.Meta.EntityID).Delete(model)
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

	model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), nil)

	result := p.db.Debug().Table(entityFactory.TableName()).Preload(clause.Associations).Find(&model, "weos_id = ? ", weosID)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retrieving entity , got: '%s'", result.Error)
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, nil
	}

	data, err := json.Marshal(model)
	err = json.Unmarshal(data, &newEntity)

	return newEntity, err
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

//GetList returns a list of content entities as well as the total found
func (p *GORMDB) GetList(ctx context.Context, entityFactory weos.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*weos.ContentEntity, int64, error) {
	var count int64
	var result *gorm.DB
	var schemes []*weos.ContentEntity
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
	model, err := p.GORMModel(entityFactory.Name(), entityFactory.Schema(), nil)
	result = p.db.Table(entityFactory.Name()).Scopes(FilterQuery(filtersProp)).Model(model).Omit("weos_id, sequence_no, table").Count(&count).Scopes(paginate(page, limit), sort(sortOptions)).Find(schemes)
	if err != nil {
		return nil, 0, err
	}
	return schemes, count, result.Error
}

func (p *GORMDB) GetByProperties(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) ([]*weos.ContentEntity, error) {
	results := entityFactory.DynamicStruct(ctxt).NewSliceOfStructs()
	result := p.db.Table(entityFactory.TableName()).Scopes(ContentQuery()).Find(results, identifiers)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retrieving created entity, got: '%s'", result.Error)
	}
	bytes, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	var entities []*weos.ContentEntity
	err = json.Unmarshal(bytes, &entities)
	return entities, err
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
		Schema: make(map[string]ds.Builder),
		keys:   make(map[string]map[string]interface{}),
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
