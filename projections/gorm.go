package projections

import (
	"encoding/json"
	"fmt"
	"strings"

	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
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

func (p *GORMProjection) DB() *gorm.DB {
	return p.db
}

func (p *GORMProjection) GetByKey(ctxt context.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
	if s, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		//pulling the primary keys from the schema in order to match with the keys given for searching
		scheme := s.Build().New()
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
		scheme := s.Build().New()
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
		switch event.Type {
		case "create":
			entityFactory := weos.GetEntityFactory(ctx)
			//using the schema ensures no nested fields are left out in creation
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error get a copy of the entity '%s'", err)
				}
				eventPayload := entity.Property
				mapPayload := entity.ToMap()
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
			entityFactory := weos.GetEntityFactory(ctx)
			if entityFactory != nil {
				entity, err := entityFactory.NewEntity(ctx)
				if err != nil {
					p.logger.Errorf("error creating entity '%s'", err)
				}
				eventPayload := entity.Property
				mapPayload := entity.ToMap()
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
		schemes = s.Build().NewSliceOfStructs()
		scheme := s.Build().New()

		result = p.db.Table(contentType.Name).Scopes(ContentQuery()).Model(&scheme).Omit("weos_id, sequence_no, table").Count(&count).Scopes(paginate(page, limit), sort(sortOptions)).Find(schemes)
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
func NewProjection(ctx context.Context, db *gorm.DB, logger weos.Log) (*GORMProjection, error) {

	projection := &GORMProjection{
		db:     db,
		logger: logger,
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
