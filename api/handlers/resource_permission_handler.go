package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/entities"

	"github.com/labstack/echo/v4"
)

type ResourcePermissionHandler struct {
	permService application.ResourcePermissionService
}

func NewResourcePermissionHandler(svc application.ResourcePermissionService) *ResourcePermissionHandler {
	return &ResourcePermissionHandler{permService: svc}
}

type grantRequest struct {
	AgentID string   `json:"agent_id"`
	Actions []string `json:"actions"`
}

type permissionResponse struct {
	ResourceID string   `json:"resource_id"`
	AgentID    string   `json:"agent_id"`
	Actions    []string `json:"actions"`
	GrantedBy  string   `json:"granted_by"`
	GrantedAt  string   `json:"granted_at"`
}

func (h *ResourcePermissionHandler) Grant(c echo.Context) error {
	resourceID := c.Param("id")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return respondError(c, http.StatusBadRequest, "failed to read request body")
	}

	var req grantRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid JSON")
	}
	if req.AgentID == "" || len(req.Actions) == 0 {
		return respondError(c, http.StatusBadRequest, "agent_id and actions are required")
	}

	actionsJSON, err := json.Marshal(req.Actions)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to encode actions")
	}
	cmd := application.GrantPermissionCommand{
		ResourceID: resourceID,
		AgentID:    req.AgentID,
		Actions:    actionsJSON,
	}

	if err := h.permService.Grant(c.Request().Context(), cmd); err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}

	return respond(c, http.StatusCreated, map[string]string{"status": "granted"})
}

func (h *ResourcePermissionHandler) Revoke(c echo.Context) error {
	resourceID := c.Param("id")
	agentID := c.Param("agentId")

	cmd := application.RevokePermissionCommand{
		ResourceID: resourceID,
		AgentID:    agentID,
	}

	if err := h.permService.Revoke(c.Request().Context(), cmd); err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *ResourcePermissionHandler) List(c echo.Context) error {
	resourceID := c.Param("id")

	perms, err := h.permService.ListForResource(c.Request().Context(), resourceID)
	if err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}

	items := make([]permissionResponse, 0, len(perms))
	for _, p := range perms {
		items = append(items, permissionResponse{
			ResourceID: p.ResourceID,
			AgentID:    p.AgentID,
			Actions:    p.Actions,
			GrantedBy:  p.GrantedBy,
			GrantedAt:  p.GrantedAt.Format(time.RFC3339),
		})
	}

	return respond(c, http.StatusOK, items)
}
