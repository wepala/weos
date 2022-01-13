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

func (p *GORMProjection) GetByID(ctxt context.Context, contentType weosContext.ContentType, identifier []interface{}) (interface{}, error) {

	return nil, nil
}

func (p *GORMProjection) GetByEntityID(ctxt context.Context, contentType weosContext.ContentType, id string) (interface{}, error) {
	return nil, nil
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
			//using the schema ensures no nested fields are left out in creation
			eventPayload, ok := p.Schema[strings.Title(contentType.Name)]
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {
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
			eventPayload, ok := p.Schema[strings.Title(contentType.Name)]
			mapPayload := map[string]interface{}{}
			if !ok {
				p.logger.Errorf("found no content type %s", contentType.Name)
			} else {

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

	newEntity, err := new(weos.ContentEntity).FromSchemaWithValues(ctx, contentType.Schema, payload)
	if err != nil {
		p.logger.Errorf("unexpected error creating entity, got: '%s'", err)
	}

	return newEntity, nil
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
