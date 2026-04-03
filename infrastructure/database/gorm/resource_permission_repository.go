package gorm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"

	"github.com/segmentio/ksuid"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourcePermissionRepository struct {
	db *gorm.DB
}

type ResourcePermissionRepositoryResult struct {
	fx.Out
	Repository repositories.ResourcePermissionRepository
}

func ProvideResourcePermissionRepository(db *gorm.DB) (ResourcePermissionRepositoryResult, error) {
	return ResourcePermissionRepositoryResult{
		Repository: &ResourcePermissionRepository{db: db},
	}, nil
}

func (r *ResourcePermissionRepository) Grant(
	ctx context.Context, perm *entities.ResourcePermission,
) error {
	if perm.ID == "" {
		perm.ID = ksuid.New().String()
	}
	if perm.GrantedAt.IsZero() {
		perm.GrantedAt = time.Now()
	}

	model, err := models.FromResourcePermission(perm)
	if err != nil {
		return fmt.Errorf("failed to convert permission: %w", err)
	}

	// Upsert: if (resource_id, agent_id) exists, update actions.
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource_id"}, {Name: "agent_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"actions", "granted_by", "granted_at"}),
	}).Create(model)
	if result.Error != nil {
		return fmt.Errorf("failed to grant permission: %w", result.Error)
	}
	return nil
}

func (r *ResourcePermissionRepository) Revoke(
	ctx context.Context, resourceID, agentID string,
) error {
	result := r.db.WithContext(ctx).
		Where("resource_id = ? AND agent_id = ?", resourceID, agentID).
		Delete(&models.ResourcePermission{})
	if result.Error != nil {
		return fmt.Errorf("failed to revoke permission: %w", result.Error)
	}
	return nil
}

func (r *ResourcePermissionRepository) FindByResource(
	ctx context.Context, resourceID string,
) ([]*entities.ResourcePermission, error) {
	var rows []models.ResourcePermission
	if err := r.db.WithContext(ctx).
		Where("resource_id = ?", resourceID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to find permissions: %w", err)
	}

	result := make([]*entities.ResourcePermission, 0, len(rows))
	for i := range rows {
		e, err := rows[i].ToEntity()
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (r *ResourcePermissionRepository) HasPermission(
	ctx context.Context, resourceID, agentID, action string,
) (bool, error) {
	var row models.ResourcePermission
	err := r.db.WithContext(ctx).
		Where("resource_id = ? AND agent_id = ?", resourceID, agentID).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check permission: %w", err)
	}

	var actions []string
	if err := json.Unmarshal([]byte(row.Actions), &actions); err != nil {
		return false, nil
	}
	for _, a := range actions {
		if a == action {
			return true, nil
		}
	}
	return false, nil
}
