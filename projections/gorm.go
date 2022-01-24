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
		tables = append(tables, instance)

		dfs, _ := json.Marshal(s.Schema.Extensions["x-remove"])
		deletedFields := []string{}
		json.Unmarshal(dfs, &deletedFields)
		for _, f := range deletedFields {
			if p.db.Migrator().HasColumn(instance, f) {
				err = p.db.Migrator().DropColumn(instance, f)
				if err != nil {
					p.logger.Errorf("unable to drop column %s from table %s with error '%s'", f, name, err)
					return err
				}
			}
		}
		fmt.Print(deletedFields)
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
				return db.Preload(clause.Associations, func(tx *gorm.DB) *gorm.DB { return tx.Omit("weos_id, sequence_no") })
			}
		}
	}
	return projection, nil
}
