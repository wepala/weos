package repositories

import (
	"context"

	"github.com/wepala/weos/domain/entities"
)

// ResourcePermissionRepository manages instance-level resource permissions.
type ResourcePermissionRepository interface {
	Grant(ctx context.Context, perm *entities.ResourcePermission) error
	Revoke(ctx context.Context, resourceID, agentID string) error
	FindByResource(ctx context.Context, resourceID string) ([]*entities.ResourcePermission, error)
	HasPermission(ctx context.Context, resourceID, agentID, action string) (bool, error)
}
