package projections

import (
	"encoding/json"
	weosContext "github.com/wepala/weos/context"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"strings"
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
			//TODO the event payload should be a struct based on the schema that came in the context
			var eventPayload map[string]interface{}
			contentType := weosContext.GetContentType(ctx)
			err := json.Unmarshal(event.Payload, &eventPayload)
			if err != nil {
				p.logger.Errorf("error unmarshalling event '%s'", err)
			}
			eventPayload["sequence_no"] = event.Meta.SequenceNo
			db := p.db.Table(contentType.Name).Create(eventPayload)
			if db.Error != nil {
				p.logger.Errorf("error creating %s, got %s", contentType.Name, db.Error)
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
