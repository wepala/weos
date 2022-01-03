package dialects

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
)

type MySQL struct {
	mysql.Dialector
}

func NewMySQL(config mysql.Config) gorm.Dialector {
	return &MySQL{mysql.Dialector{Config: &config}}
}

type MySQLMigrator struct {
	Migrator
}

func (dialector MySQL) Migrator(db *gorm.DB) gorm.Migrator {
	return MySQLMigrator{
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
