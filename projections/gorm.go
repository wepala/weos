package projections

import (
	"github.com/wepala/weos"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

//GORMProjection interface struct
type GORMProjection struct {
	db              *gorm.DB
	logger          weos.Log
	migrationFolder string
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
	panic("implement me")
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	panic("implement me")
}

//NewProjection creates an instance of the projection
func NewProjection(application weos.Application) (*GORMProjection, error) {
	projection := &GORMProjection{
		db:     application.DB(),
		logger: application.Logger(),
	}
	application.AddProjection(projection)
	return projection, nil
}
