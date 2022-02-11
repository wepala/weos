package projections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ds "github.com/ompluscator/dynamic-struct"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//GORMProjection interface struct
type GORMProjection struct {
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

func (p *GORMProjection) DB() *gorm.DB {
	return p.db
}

func (p *GORMProjection) GetByKey(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) (map[string]interface{}, error) {
	scheme, err := entityFactory.NewEntity(ctxt)
	if err != nil {
		return nil, err
	}
	//pulling the primary keys from the schema in order to match with the keys given for searching
	pks, _ := json.Marshal(scheme.Schema.Extensions["x-identifier"])
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

	result := p.db.Table(entityFactory.Name()).Scopes(ContentQuery()).Find(scheme.Property, identifiers)
	if result.Error != nil {
		return nil, result.Error
	}
	data, err := json.Marshal(scheme.Property)
	if err != nil {
		return nil, err
	}
	val := map[string]interface{}{}
	json.Unmarshal(data, &val)
	return val, nil

}

func (p *GORMProjection) GetByEntityID(ctx context.Context, entityFactory weos.EntityFactory, id string) (map[string]interface{}, error) {
	scheme, err := entityFactory.NewEntity(ctx)
	if err != nil {
		return nil, err
	}
	result := p.db.Table(entityFactory.Name()).Scopes(ContentQuery()).Find(scheme.Property, "weos_id = ?", id)

	if result.Error != nil {
		return nil, result.Error
	}
	data, err := json.Marshal(scheme.Property)
	if err != nil {
		return nil, err
	}
	val := map[string]interface{}{}
	json.Unmarshal(data, &val)
	return val, nil

}

//Persist save entity information in database
func (p *GORMProjection) Persist(entities []weos.Entity) error {
	return nil
}

//Remove entity
func (p *GORMProjection) Remove(entities []weos.Entity) error {
	return nil
}

func (p *GORMProjection) Migrate(ctx context.Context, builders map[string]ds.Builder) error {

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
	}

	err = p.db.Migrator().AutoMigrate(tables...)
	return err
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) {
		entityFactory := weos.GetEntityFactory(ctx)
		switch event.Type {
		case "create":
			//using the schema ensures no nested fields are left out in creation
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error get a copy of the entity '%s'", err)
				}
				eventPayload := entity.Property
				mapPayload := map[string]interface{}{}
				err = json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}
				mapPayload["sequence_no"] = event.Meta.SequenceNo

				bytes, _ := json.Marshal(mapPayload)
				err = json.Unmarshal(bytes, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}
				db := p.db.Table(entityFactory.Name()).Create(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", entityFactory.Name(), db.Error)
				}
			}
		case "update":
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
				}
				eventPayload := entity.Property
				mapPayload := map[string]interface{}{}
				err = json.Unmarshal(event.Payload, &mapPayload)
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
							p.logger.Errorf("error clearing association %s for %s, got %s", strings.Title(key), entityFactory.Name(), err)
						}
					}
				}

				//update database value
				db := p.db.Table(entityFactory.Name()).Updates(eventPayload)
				if db.Error != nil {
					p.logger.Errorf("error creating %s, got %s", entityFactory.Name(), db.Error)
				}
			}
		case "delete":
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
				}
				db := p.db.Table(entityFactory.Name()).Where("weos_id = ?", event.Meta.EntityID).Delete(entity.Property)
				if db.Error != nil {
					p.logger.Errorf("error deleting %s, got %s", entityFactory.Name(), db.Error)
				}
			}
		}
	}
}

func (p *GORMProjection) GetContentEntity(ctx context.Context, entityFactory weos.EntityFactory, weosID string) (*weos.ContentEntity, error) {
	newEntity, err := entityFactory.NewEntity(ctx)
	if err != nil {
		return nil, err
	}

	result := p.db.Table(entityFactory.TableName()).Find(newEntity.Property, "weos_id = ? ", weosID)
	if result.Error != nil {
		p.logger.Errorf("unexpected error retrieving created blog, got: '%s'", result.Error)
	}
	//set result to entity
	rowData, err := json.Marshal(newEntity.Property)
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
func (p *GORMProjection) GetContentEntities(ctx context.Context, entityFactory weos.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
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
func NewProjection(ctx context.Context, db *gorm.DB, logger weos.Log) (*GORMProjection, error) {

	projection := &GORMProjection{
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
							db.Where(filter.Field+" "+operator+" ?", "%"+filter.Value.(string)+"%")
						} else {
							db.Where(filter.Field+" "+operator+" ?", filter.Value)
						}

					} else {
						db.Where(filter.Field+" "+operator+" ?", filter.Values)
					}

				}
			}
			return db
		}
	}

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
