package projections

import (
	"encoding/json"
	"strings"

	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos-service/context"
	weos "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

//GORMProjection interface struct
type GORMProjection struct {
	db              *gorm.DB
	logger          weos.Log
	migrationFolder string
	Schema          map[string]interface{}
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
