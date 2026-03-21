// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"net/http"
	"strconv"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

type OrganizationHandler struct {
	service       application.OrganizationService
	personService application.PersonService
}

func NewOrganizationHandler(
	service application.OrganizationService,
	personService application.PersonService,
) *OrganizationHandler {
	return &OrganizationHandler{service: service, personService: personService}
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
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateOrganizationCommand{Name: req.Name, Slug: req.Slug},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "organization not found"})
	}
	return c.JSON(http.StatusOK, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	result, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	items := make([]OrganizationResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toOrganizationResponse(e))
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func (h *OrganizationHandler) Update(c echo.Context) error {
	var req UpdateOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateOrganizationCommand{
			ID:          c.Param("id"),
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			URL:         req.URL,
			LogoURL:     req.LogoURL,
			Status:      req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toOrganizationResponse(entity))
}

func (h *OrganizationHandler) Delete(c echo.Context) error {
	cmd := application.DeleteOrganizationCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *OrganizationHandler) Members(c echo.Context) error {
	orgID := c.Param("id")
	if _, err := h.service.GetByID(c.Request().Context(), orgID); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "organization not found"})
	}

	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	result, err := h.personService.ListByOrganization(
		c.Request().Context(), orgID, cursor, limit,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	items := make([]PersonResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toPersonResponse(e))
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func toOrganizationResponse(e *entities.Organization) OrganizationResponse {
	return OrganizationResponse{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		URL:         e.URL(),
		LogoURL:     e.LogoURL(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt().Format(time.RFC3339),
	}
}
