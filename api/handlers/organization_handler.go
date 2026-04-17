package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/entities"
	"github.com/wepala/weos/domain/repositories"

	"github.com/labstack/echo/v4"
)

type OrganizationHandler struct {
	resourceService application.ResourceService
}

func NewOrganizationHandler(resourceService application.ResourceService) *OrganizationHandler {
	return &OrganizationHandler{resourceService: resourceService}
}

type CreateOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UpdateOrganizationRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	URL         string `json:"url"`
	LogoURL     string `json:"logo_url"`
	Status      string `json:"status"`
}

type OrganizationResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	LogoURL     string `json:"logo_url,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func (h *OrganizationHandler) Create(c echo.Context) error {
	var req CreateOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	data, _ := json.Marshal(map[string]any{
		"name": req.Name,
		"slug": req.Slug,
	})
	entity, err := h.resourceService.Create(
		c.Request().Context(),
		application.CreateResourceCommand{TypeSlug: "organization", Data: data},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusCreated, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) Get(c echo.Context) error {
	entity, err := h.resourceService.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return respondError(c, http.StatusNotFound, "organization not found")
	}
	return respond(c, http.StatusOK, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	result, err := h.resourceService.List(
		c.Request().Context(), "organization", cursor, limit, repositories.SortOptions{},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	items := make([]OrganizationResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toOrganizationResponse(e))
	}
	return respondPaginated(c, http.StatusOK, items, result.Cursor, result.HasMore)
}

func (h *OrganizationHandler) Update(c echo.Context) error {
	var req UpdateOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	data, _ := json.Marshal(map[string]any{
		"name":        req.Name,
		"slug":        req.Slug,
		"description": req.Description,
		"url":         req.URL,
		"logoURL":     req.LogoURL,
	})
	entity, err := h.resourceService.Update(
		c.Request().Context(),
		application.UpdateResourceCommand{ID: c.Param("id"), Data: data},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) Delete(c echo.Context) error {
	cmd := application.DeleteResourceCommand{ID: c.Param("id")}
	if err := h.resourceService.Delete(c.Request().Context(), cmd); err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *OrganizationHandler) Members(c echo.Context) error {
	orgID := c.Param("id")
	if _, err := h.resourceService.GetByID(c.Request().Context(), orgID); err != nil {
		return respondError(c, http.StatusNotFound, "organization not found")
	}

	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	filters := []repositories.FilterCondition{
		{Field: "organization_id", Operator: "eq", Value: orgID},
	}
	result, err := h.resourceService.ListWithFilters(
		c.Request().Context(), "person", filters, cursor, limit, repositories.SortOptions{},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	items := make([]PersonResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toPersonResponse(e))
	}
	return respondPaginated(c, http.StatusOK, items, result.Cursor, result.HasMore)
}

func toOrganizationResponse(r *entities.Resource) OrganizationResponse {
	fields, _ := application.ExtractResourceFields(r)
	return OrganizationResponse{
		ID:          r.GetID(),
		Name:        application.StringField(fields, "name"),
		Slug:        application.StringField(fields, "slug"),
		Description: application.StringField(fields, "description"),
		URL:         application.StringField(fields, "url"),
		LogoURL:     application.StringField(fields, "logoURL"),
		Status:      r.Status(),
		CreatedAt:   r.CreatedAt().Format(time.RFC3339),
	}
}
