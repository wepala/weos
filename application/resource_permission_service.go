package application

import (
	"context"
	"encoding/json"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

type GrantPermissionCommand struct {
	ResourceID string          `json:"resource_id"`
	AgentID    string          `json:"agent_id"`
	Actions    json.RawMessage `json:"actions"` // JSON array: ["read","modify","delete"]
}

type RevokePermissionCommand struct {
	ResourceID string `json:"resource_id"`
	AgentID    string `json:"agent_id"`
}

type ResourcePermissionService interface {
	Grant(ctx context.Context, cmd GrantPermissionCommand) error
	Revoke(ctx context.Context, cmd RevokePermissionCommand) error
	ListForResource(ctx context.Context, resourceID string) ([]*entities.ResourcePermission, error)
}

type resourcePermissionService struct {
	permRepo     repositories.ResourcePermissionRepository
	resourceRepo repositories.ResourceRepository
	accountRepo  authrepos.AccountRepository
	logger       entities.Logger
}

func ProvideResourcePermissionService(params struct {
	fx.In
	PermRepo     repositories.ResourcePermissionRepository
	ResourceRepo repositories.ResourceRepository
	AccountRepo  authrepos.AccountRepository
	Logger       entities.Logger
}) ResourcePermissionService {
	return &resourcePermissionService{
		permRepo:     params.PermRepo,
		resourceRepo: params.ResourceRepo,
		accountRepo:  params.AccountRepo,
		logger:       params.Logger,
	}
}

func (s *resourcePermissionService) canManagePermissions(
	ctx context.Context, resource *entities.Resource,
) error {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil // system context
	}
	// Admin/owner can manage any resource's permissions
	role, _ := s.accountRepo.FindMemberRole(ctx, identity.ActiveAccountID, identity.AgentID)
	if role == "admin" || role == "owner" {
		return nil
	}
	// Only the creator can grant/revoke permissions
	if resource.CreatedBy() == identity.AgentID {
		return nil
	}
	return entities.ErrAccessDenied
}

func (s *resourcePermissionService) Grant(
	ctx context.Context, cmd GrantPermissionCommand,
) error {
	resource, err := s.resourceRepo.FindByID(ctx, cmd.ResourceID)
	if err != nil {
		return fmt.Errorf("resource not found: %w", err)
	}
	if err := s.canManagePermissions(ctx, resource); err != nil {
		return err
	}

	var actions []string
	if err := json.Unmarshal(cmd.Actions, &actions); err != nil {
		return fmt.Errorf("invalid actions: %w", err)
	}

	identity := auth.AgentFromCtx(ctx)
	grantedBy := ""
	if identity != nil {
		grantedBy = identity.AgentID
	}

	perm := &entities.ResourcePermission{
		ResourceID: cmd.ResourceID,
		AgentID:    cmd.AgentID,
		Actions:    actions,
		GrantedBy:  grantedBy,
	}
	if err := s.permRepo.Grant(ctx, perm); err != nil {
		return fmt.Errorf("failed to grant permission: %w", err)
	}

	s.logger.Info(ctx, "permission granted",
		"resource", cmd.ResourceID, "agent", cmd.AgentID, "actions", actions)
	return nil
}

func (s *resourcePermissionService) Revoke(
	ctx context.Context, cmd RevokePermissionCommand,
) error {
	resource, err := s.resourceRepo.FindByID(ctx, cmd.ResourceID)
	if err != nil {
		return fmt.Errorf("resource not found: %w", err)
	}
	if err := s.canManagePermissions(ctx, resource); err != nil {
		return err
	}

	if err := s.permRepo.Revoke(ctx, cmd.ResourceID, cmd.AgentID); err != nil {
		return fmt.Errorf("failed to revoke permission: %w", err)
	}

	s.logger.Info(ctx, "permission revoked",
		"resource", cmd.ResourceID, "agent", cmd.AgentID)
	return nil
}

func (s *resourcePermissionService) ListForResource(
	ctx context.Context, resourceID string,
) ([]*entities.ResourcePermission, error) {
	resource, err := s.resourceRepo.FindByID(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("resource not found: %w", err)
	}
	if err := s.canManagePermissions(ctx, resource); err != nil {
		return nil, err
	}
	return s.permRepo.FindByResource(ctx, resourceID)
}
