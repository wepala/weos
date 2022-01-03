package dialects

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
)

type Postgres struct {
	postgres.Dialector
}

func NewPostgres(config postgres.Config) gorm.Dialector {
	return &Postgres{
		postgres.Dialector{Config: &config},
	}
}

func (dialector Postgres) Migrator(db *gorm.DB) gorm.Migrator {
	return PostgresMigrator{
		Migrator{
			Migrator: migrator.Migrator{
				Config: migrator.Config{
					DB:        db,
					Dialector: dialector,
				},
			},
		},
	}
}

type PostgresMigrator struct {
	Migrator
}
