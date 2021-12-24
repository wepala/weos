package projections

import (
	"fmt"
	"reflect"

	"github.com/getkin/kin-openapi/openapi3"
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
	var schemes []interface{}
	for _, s := range p.Schema {
		schemes = append(schemes, s)
		fmt.Print(reflect.TypeOf(s))
		// if !p.db.Migrator().HasTable(name) {
		// 	err = p.db.Migrator().CreateTable(s)
		// 	if err != nil {
		// 		return err
		// 	}
		// 	err = p.db.Migrator().RenameTable("", name)
		// 	if err != nil {
		// 		return err
		// 	}
		// }

	}
	p.db.AutoMigrate(schemes...)
	return err
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return nil
}

//NewProjection creates an instance of the projection
func NewProjection(ctx context.Context, application weos.Service, schemas map[string]*openapi3.SchemaRef) (*GORMProjection, error) {

	structs, err := CreateSchema(ctx, schemas)
	if err != nil {
		return nil, err
	}
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
