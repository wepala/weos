package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/labstack/echo/v4"
)

type PersonHandler struct {
	resourceService application.ResourceService
}

func NewPersonHandler(resourceService application.ResourceService) *PersonHandler {
	return &PersonHandler{resourceService: resourceService}
}

type CreatePersonRequest struct {
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Email      string `json:"email"`
}

type UpdatePersonRequest struct {
	GivenName  string  `json:"given_name"`
	FamilyName string  `json:"family_name"`
	Email      string  `json:"email"`
	AvatarURL  string  `json:"avatar_url"`
	Status     *string `json:"status,omitempty"`
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
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	data, _ := json.Marshal(map[string]any{
		"givenName":  req.GivenName,
		"familyName": req.FamilyName,
		"email":      req.Email,
		"status":     "active",
	})
	entity, err := h.resourceService.Create(
		c.Request().Context(),
		application.CreateResourceCommand{TypeSlug: "person", Data: data},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusCreated, toPersonResponse(entity))
}

func (h *PersonHandler) Get(c echo.Context) error {
	entity, err := h.resourceService.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return respondError(c, http.StatusNotFound, "person not found")
	}
	return respond(c, http.StatusOK, toPersonResponse(entity))
}

var personFieldMap = map[string]string{
	"given_name":  "givenName",
	"family_name": "familyName",
	"email":       "email",
	"avatar_url":  "avatarURL",
	"name":        "name",
}

func (h *PersonHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	filters := parseFilters(c)
	for i, f := range filters {
		if mapped, ok := personFieldMap[f.Field]; ok {
			filters[i].Field = mapped
		}
	}
	var result repositories.PaginatedResponse[*entities.Resource]
	var err error
	if len(filters) > 0 {
		result, err = h.resourceService.ListWithFilters(
			c.Request().Context(), "person", filters, cursor, limit, repositories.SortOptions{},
		)
	} else {
		result, err = h.resourceService.List(
			c.Request().Context(), "person", cursor, limit, repositories.SortOptions{},
		)
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	items := make([]PersonResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toPersonResponse(e))
	}
	return respondPaginated(c, http.StatusOK, items, result.Cursor, result.HasMore)
}

func (h *PersonHandler) Update(c echo.Context) error {
	var req UpdatePersonRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	existing, err := h.resourceService.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return respondError(c, http.StatusNotFound, "person not found")
	}
	existingFields, _ := application.ExtractResourceFields(existing)
	fields := map[string]any{
		"givenName":  req.GivenName,
		"familyName": req.FamilyName,
		"email":      req.Email,
		"avatarURL":  req.AvatarURL,
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	} else if s := application.StringField(existingFields, "status"); s != "" {
		fields["status"] = s
	}
	data, _ := json.Marshal(fields)
	entity, err := h.resourceService.Update(
		c.Request().Context(),
		application.UpdateResourceCommand{ID: c.Param("id"), Data: data},
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, toPersonResponse(entity))
}

func (h *PersonHandler) Delete(c echo.Context) error {
	cmd := application.DeleteResourceCommand{ID: c.Param("id")}
	if err := h.resourceService.Delete(c.Request().Context(), cmd); err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func toPersonResponse(r *entities.Resource) PersonResponse {
	fields, _ := application.ExtractResourceFields(r)
	status := application.StringField(fields, "status")
	if status == "" {
		status = r.Status()
	}
	return PersonResponse{
		ID:         r.GetID(),
		GivenName:  application.StringField(fields, "givenName"),
		FamilyName: application.StringField(fields, "familyName"),
		Name:       application.StringField(fields, "name"),
		Email:      application.StringField(fields, "email"),
		AvatarURL:  application.StringField(fields, "avatarURL"),
		Status:     status,
		CreatedAt:  r.CreatedAt().Format(time.RFC3339),
	}
}
