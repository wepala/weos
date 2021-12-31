package projections

import (
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

//Migrate projections
func (p *GORMProjection) Migrate(ctx context.Context) error {

	//we may need to reorder the creation so that tables don't reference things that don't exist as yet.
	var err error
	var tables []interface{}
	for _, s := range p.Schema {
		tables = append(tables, s)
		//fmt.Print(reflect.TypeOf(s))
		//if !p.db.Migrator().HasTable(name) {
		//	err = p.db.Migrator().CreateTable(s)
		//	if err != nil {
		//		return err
		//	}
		//	err = p.db.Migrator().RenameTable("", name)
		//	if err != nil {
		//		return err
		//	}
		//}

	}
	//p.db.Statement = &gorm.Statement{Table: "Blog", ConnPool: p.db.ConnPool, DB: p.db}
	err = p.db.Callback().Create().Before("gorm:create").Register("table_name", func(db *gorm.DB) {
		if db.Statement.Table == "" { // if the table name is empty then let's infer from a property on the object
			for _, field := range db.Statement.Schema.Fields {
				if field.Name == "table_alias" {
					// Get value from field
					if fieldValue, isZero := field.ValueOf(db.Statement.ReflectValue); !isZero {
						if value, ok := fieldValue.(string); ok {
							db.Statement.Table = value
						}
					}

				}
			}
		}
	})
	if err != nil {
		return err
	}
	//err = p.db.Migrator().CreateTable(tables[0])
	//if err != nil {
	//	return err
	//}

	err = p.db.AutoMigrate(tables...)
	return err
}

func (p *GORMProjection) GetEventHandler() weos.EventHandler {
	return nil
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
