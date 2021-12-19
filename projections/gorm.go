package projections

import (
	"github.com/stoewer/go-strcase"
	weos "github.com/wepala/weos-content-service/model"
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
	for name, s := range p.Schema {
		//can't automigrate the whole array.  would cause errors.  We may need to think through how this is done as the create table does not autmomigrate
		err := p.db.Migrator().CreateTable(s)
		if err != nil {
			return err
		}

		err = p.db.Migrator().RenameTable("", strcase.SnakeCase(name))
		if err != nil {
			return err
		}

	}
	return nil
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return nil
}

//NewProjection creates an instance of the projection
func NewProjection(structs map[string]interface{}, application weos.Application) (*GORMProjection, error) {
	dbStructs := make(map[string]interface{})

	for name, s := range structs {
		dbStructs[name] = s
	}
	projection := &GORMProjection{
		db:     application.DB(),
		logger: application.Logger(),
		Schema: dbStructs,
	}
	application.AddProjection(projection)
	return projection, nil
}
