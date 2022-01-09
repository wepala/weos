package projections

import (
	"encoding/json"
	"fmt"
	"strings"

	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos-service/context"
	weos "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//GORMProjection interface struct
type GORMProjection struct {
	db              *gorm.DB
	logger          weos.Log
	migrationFolder string
	Schema          map[string]interface{}
}

func (p *GORMProjection) GetByKey(ctxt context.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
	if scheme, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		//pulling the primary keys from the schema in order to match with the keys given for searching
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

		//gorm sqlite generates the query incorrectly for composite keys when preloading
		result := p.db.Table(contentType.Name).Preload(clause.Associations, func(tx *gorm.DB) *gorm.DB { return tx.Omit("weos_id, sequence_no") }).First(scheme, identifiers)
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
	if scheme, ok := p.Schema[strings.Title(contentType.Name)]; ok {
		result := p.db.Table(contentType.Name).Preload(clause.Associations, func(tx *gorm.DB) *gorm.DB { return tx.Omit("weos_id, sequence_no") }).Where("weos_id = ?", id).Take(scheme)
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
	for _, s := range p.Schema {
		tables = append(tables, s)
	}

	err = p.db.Migrator().AutoMigrate(tables...)
	return err
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) {
		switch event.Type {
		case "create":
			contentType := weosContext.GetContentType(ctx)
			eventPayload, ok := p.Schema[strings.Title(contentType.Name)]
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {
				err := json.Unmarshal(event.Payload, &eventPayload)
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
			eventPayload, ok := p.Schema[strings.Title(contentType.Name)]
			mapPayload := map[string]interface{}{}
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {
				err := json.Unmarshal(event.Payload, &eventPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}

				err = json.Unmarshal(event.Payload, &mapPayload)
				if err != nil {
					p.logger.Errorf("error unmarshalling event '%s'", err)
				}
				reader := ds.NewReader(eventPayload)

				//replace associations
				for key, entity := range mapPayload {
					//many to many association
					if _, ok := entity.([]interface{}); ok {
						field := reader.GetField(strings.Title(key))
						err = p.db.Debug().Model(eventPayload).Association(strings.Title(key)).Replace(field.Interface())
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

//NewProjection creates an instance of the projection
func NewProjection(ctx context.Context, application weos.Service, schemas map[string]interface{}) (*GORMProjection, error) {

	projection := &GORMProjection{
		db:     application.DB(),
		logger: application.Logger(),
		Schema: schemas,
	}
	application.AddProjection(projection)
	return projection, nil
}
