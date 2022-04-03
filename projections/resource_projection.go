package projections

import (
	"encoding/json"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"time"
)

//Resource Model
type Resource struct {
	Path     string                 `json:"@id" gorm:"primaryKey;"`
	ID       string                 `json:"id"`
	Context  map[string]interface{} `json:"@context"`
	Schema   json.RawMessage        `json:"schema"`
	Content  json.RawMessage        `json:"content"`
	Created  time.Time              `json:"created" gorm:"autoCreateTime"`
	Modified time.Time              `json:"modified" gorm:"autoUpdateTime"`
}

func (r *Resource) FromEvent(event *model.Event) (*Resource, error) {
	return r, nil
}

//Collection of resources
type Collection struct {
	Resource
	TotalItems int         `json:"totalItems"`
	Items      []*Resource `json:"items"`
}

type GORMResourceProjection struct {
	db     *gorm.DB
	logger model.Log
}

func (G *GORMResourceProjection) Migrate(ctx context.Context, builders map[string]ds.Builder, deletedFields map[string][]string) error {
	return G.db.AutoMigrate(&Resource{}, &Collection{})
}

func (G *GORMResourceProjection) GetEventHandler() model.EventHandler {
	return func(ctx context.Context, event model.Event) error {
		switch event.Type {
		case "create":
			resource, err := new(Resource).FromEvent(&event)
			if err != nil {
				return err
			}
			G.db.Save(resource)
		case "delete":

		}
		return nil
	}
}

func (G *GORMResourceProjection) GetContentEntity(ctx context.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetByKey(ctxt context.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (map[string]interface{}, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetByEntityID(ctxt context.Context, entityFactory model.EntityFactory, id string) (map[string]interface{}, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetContentEntities(ctx context.Context, entityFactory model.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetByProperties(ctxt context.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]map[string]interface{}, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetByID(ctxt context.Context, id string) (model.Entity, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) GetCollection(ctx context.Context, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	//TODO implement me
	panic("implement me")
}

func (G *GORMResourceProjection) DB() *gorm.DB {
	return G.db
}

//NewGORMResourceProjection creates an instance of the projection
func NewGORMResourceProjection(ctx context.Context, db *gorm.DB, logger model.Log) (Projection, error) {

	projection := &GORMResourceProjection{
		db:     db,
		logger: logger,
	}

	return projection, nil
}
