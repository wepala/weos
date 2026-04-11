package gorm

import (
	"weos/domain/entities"
	"weos/domain/repositories"

	"gorm.io/gorm"
)

// NewProjectionManagerForTest creates a ProjectionManager without fx wiring.
func NewProjectionManagerForTest(db *gorm.DB, logger entities.Logger) repositories.ProjectionManager {
	return &projectionManager{db: db, logger: logger}
}

// NewResourceRepositoryForTest creates a ResourceRepository without fx wiring.
func NewResourceRepositoryForTest(
	db *gorm.DB, projMgr repositories.ProjectionManager, logger entities.Logger,
) repositories.ResourceRepository {
	return &ResourceRepository{db: db, projMgr: projMgr, logger: logger}
}

// NewResourceTypeRepositoryForTest creates a ResourceTypeRepository without fx wiring.
func NewResourceTypeRepositoryForTest(db *gorm.DB) repositories.ResourceTypeRepository {
	return &ResourceTypeRepository{db: db}
}

// NewTripleRepositoryForTest creates a TripleRepository without fx wiring.
func NewTripleRepositoryForTest(db *gorm.DB) repositories.TripleRepository {
	return &TripleRepository{db: db}
}
