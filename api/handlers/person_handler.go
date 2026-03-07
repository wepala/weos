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

type PersonHandler struct {
	service application.PersonService
}

func NewPersonHandler(service application.PersonService) *PersonHandler {
	return &PersonHandler{service: service}
}

type CreatePersonRequest struct {
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Email      string `json:"email"`
}

type UpdatePersonRequest struct {
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url"`
	Status     string `json:"status"`
}

type PersonResponse struct {
	ID         string `json:"id"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

func (h *PersonHandler) Create(c echo.Context) error {
	var req CreatePersonRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreatePersonCommand{
			GivenName:  req.GivenName,
			FamilyName: req.FamilyName,
			Email:      req.Email,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toPersonResponse(entity))
}

func (h *PersonHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "person not found"})
	}
	return c.JSON(http.StatusOK, toPersonResponse(entity))
}

func (h *PersonHandler) List(c echo.Context) error {
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

func (h *PersonHandler) Update(c echo.Context) error {
	var req UpdatePersonRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdatePersonCommand{
			ID:         c.Param("id"),
			GivenName:  req.GivenName,
			FamilyName: req.FamilyName,
			Email:      req.Email,
			AvatarURL:  req.AvatarURL,
			Status:     req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toPersonResponse(entity))
}

func (h *PersonHandler) Delete(c echo.Context) error {
	cmd := application.DeletePersonCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toPersonResponse(e *entities.Person) PersonResponse {
	return PersonResponse{
		ID:         e.GetID(),
		GivenName:  e.GivenName(),
		FamilyName: e.FamilyName(),
		Name:       e.Name(),
		Email:      e.Email(),
		AvatarURL:  e.AvatarURL(),
		Status:     e.Status(),
		CreatedAt:  e.CreatedAt().Format(time.RFC3339),
	}
}
