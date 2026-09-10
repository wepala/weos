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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const (
	// CodeAccountErasureUnfinished is the code a deletion that failed part-way
	// answers with. The account is locked and the deletion can be run again.
	CodeAccountErasureUnfinished = "account_erasure_unfinished"
	// CodeAccountErasureInProgress is the code a second deletion request gets
	// while the first is still running. Nothing is wrong; the app should wait
	// for the first answer rather than send a third.
	CodeAccountErasureInProgress = "account_erasure_in_progress"
)

// deleteConfirmation is the one body DELETE /api/account accepts. The field is
// "confirm" and the value is the word DELETE exactly, decided at the plan gate
// over the design record's "confirmation" (wm-cqqxf).
const deleteConfirmation = "DELETE"

// exportTypeSlug is the one type the export holds. The document says so of
// itself, because it is offered beside an irreversible button (wm-os4la).
const exportTypeSlug = "recipe"

// AccountErasureRunner is what the delete route needs of the erasure service.
type AccountErasureRunner interface {
	Erase(ctx context.Context, cmd application.EraseAccountCommand) (*application.ErasureResult, error)
}

// AccountHandlerConfig wires the account routes.
type AccountHandlerConfig struct {
	Erasure        AccountErasureRunner
	Accounts       authrepos.AccountRepository
	Resources      repositories.ResourceRepository
	ResourceTypes  application.ResourceTypeService
	SessionManager session.SessionManager
	// Store is the cookie store the impersonation middleware uses. The delete
	// route reads it directly, because it must refuse while an impersonation
	// is active rather than act as the impersonated person.
	Store         sessions.Store
	JWTCookieName string
	SecureCookies bool
	Logger        entities.Logger
}

// AccountHandler serves the routes a person uses to end their own account:
// the deletion, and the export offered beside it.
type AccountHandler struct {
	cfg AccountHandlerConfig
}

func NewAccountHandler(cfg AccountHandlerConfig) *AccountHandler {
	if cfg.JWTCookieName == "" {
		cfg.JWTCookieName = "pericarp_token"
	}
	return &AccountHandler{cfg: cfg}
}

// AccountDeletedResponse is what an accepted deletion answers with.
type AccountDeletedResponse struct {
	AccountID string `json:"account_id"`
	// MembersLost is how many people belonged to the account when it went —
	// the caller included — so the app can say who else lost it.
	MembersLost int `json:"members_lost"`
}

// Delete erases the caller's active account. It requires the body
// {"confirm":"DELETE"} exactly, an owner or admin of the account, and no
// impersonation in progress. On success it signs the caller out: the session
// row is already gone, and both cookies are cleared here.
func (h *AccountHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()
	identity := auth.AgentFromCtx(ctx)
	if identity == nil || identity.ActiveAccountID == "" {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}
	if h.impersonating(c) {
		// An administrator acting as somebody must not be able to end that
		// person's account through their identity.
		return respondError(c, http.StatusForbidden, "account deletion is not available while impersonating")
	}
	if !readsConfirmation(c.Request()) {
		return respondError(c, http.StatusBadRequest,
			`deleting the account requires the body {"confirm":"DELETE"} sent as application/json`)
	}

	account, err := h.cfg.Accounts.FindByID(ctx, identity.ActiveAccountID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "account delete: load account failed", "account_id", identity.ActiveAccountID, "error", err)
		return respondError(c, http.StatusInternalServerError, "could not read the account")
	}
	if account == nil {
		return respondError(c, http.StatusNotFound, "account not found")
	}
	allowed, err := apimw.IsOwnerOrAdmin(ctx, h.cfg.Accounts, identity.ActiveAccountID, identity.AgentID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "account delete: role lookup failed", "account_id", identity.ActiveAccountID, "error", err)
		return respondError(c, http.StatusInternalServerError, "could not read the caller's role")
	}
	if !allowed {
		return respondError(c, http.StatusForbidden, "only an owner or admin may delete the account")
	}

	result, err := h.cfg.Erasure.Erase(ctx, application.EraseAccountCommand{
		AccountID:   identity.ActiveAccountID,
		RequestedBy: identity.AgentID,
	})
	if err != nil {
		if errors.Is(err, application.ErrAccountNotFound) {
			return respondError(c, http.StatusNotFound, "account not found")
		}
		if errors.Is(err, application.ErrErasureInProgress) {
			return respondErrorCode(c, http.StatusConflict,
				"a deletion of this account is already running; wait for it to finish",
				CodeAccountErasureInProgress)
		}
		h.cfg.Logger.Error(ctx, "account delete: erasure did not finish",
			"account_id", identity.ActiveAccountID, "error", err)
		return respondErrorCode(c, http.StatusInternalServerError,
			"the account's deletion did not finish; the account is locked and the deletion can be run again",
			CodeAccountErasureUnfinished)
	}

	h.signOut(c)
	return respond(c, http.StatusOK, AccountDeletedResponse{
		AccountID:   result.AccountID,
		MembersLost: result.MembersLost,
	})
}

// readsConfirmation reports whether the request is exactly the confirmation:
// a JSON body with one field, named confirm in that spelling and case, whose
// value is the word DELETE exactly, sent as application/json. Anything else —
// a different field, a different case, an extra field, a trailing space, a
// second document, another content type, no body — is refused, and changes
// nothing. It is read this strictly because encoding/json's struct decoding
// matches names case-insensitively and ignores fields it does not know, and
// the handler's promise is the other way round (wm-q7knw).
func readsConfirmation(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != echo.MIMEApplicationJSON {
		return false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil || len(fields) != 1 {
		return false
	}
	if dec.More() {
		return false
	}
	value, ok := fields["confirm"]
	if !ok {
		return false
	}
	var confirm string
	if err := json.Unmarshal(value, &confirm); err != nil {
		return false
	}
	return confirm == deleteConfirmation
}

// impersonating reports whether the request carries an active impersonation
// session — the same cookie the Impersonation middleware reads.
func (h *AccountHandler) impersonating(c echo.Context) bool {
	if h.cfg.Store == nil {
		return false
	}
	sess, err := h.cfg.Store.Get(c.Request(), apimw.ImpersonationSessionName)
	if err != nil || sess == nil {
		return false
	}
	impersonated, _ := sess.Values[apimw.KeyImpersonatedAgentID].(string)
	return impersonated != ""
}

// signOut clears the session cookie and the JWT cookie, the way Logout does.
// The session row itself went with the account.
func (h *AccountHandler) signOut(c echo.Context) {
	w := c.Response().Writer
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.JWTCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	if h.cfg.SessionManager != nil {
		if err := h.cfg.SessionManager.DestroyHTTPSession(w, c.Request()); err != nil {
			// The account is gone and the session row with it; a cookie the
			// browser keeps is refused on its next request either way.
			h.cfg.Logger.Warn(c.Request().Context(), "account delete: could not clear the session cookie", "error", err)
		}
	}
}

// Export hands back the active account's recipes as one JSON-LD document.
//
// It holds recipes and nothing else — no pantry, no photos, no meal logs —
// and the document says so about itself, because it is offered on the screen
// where the account is deleted and a person keeping it must know what they
// keep. It filters on the account explicitly rather than on what the caller
// may read: a person who belongs to two accounts is not handed the other
// account's recipes as their own.
func (h *AccountHandler) Export(c echo.Context) error {
	ctx := c.Request().Context()
	identity := auth.AgentFromCtx(ctx)
	if identity == nil || identity.ActiveAccountID == "" {
		return respondError(c, http.StatusUnauthorized, "not authenticated")
	}
	doc, err := h.buildExport(ctx, identity.ActiveAccountID)
	if err != nil {
		h.cfg.Logger.Error(ctx, "account export failed", "account_id", identity.ActiveAccountID, "error", err)
		return respondError(c, http.StatusInternalServerError, "could not build the export")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "could not build the export")
	}
	// A JSON-LD document at the top level, the way a resource read answers a
	// JSON-LD client: the envelope would make it something else.
	c.Response().Header().Set("Content-Disposition", `attachment; filename="recipes.jsonld"`)
	return c.Blob(http.StatusOK, "application/ld+json", raw)
}

// AccountExport is the shape of the export document.
type AccountExport struct {
	Context json.RawMessage   `json:"@context"`
	Type    string            `json:"@type"`
	Scope   AccountExportNote `json:"weos:exportScope"`
	Graph   []json.RawMessage `json:"@graph"`
}

// AccountExportNote is the document's description of its own scope.
type AccountExportNote struct {
	AccountID string   `json:"accountId"`
	Includes  []string `json:"includes"`
	Excludes  []string `json:"excludes"`
	Note      string   `json:"note"`
}

func (h *AccountHandler) buildExport(ctx context.Context, accountID string) (*AccountExport, error) {
	doc := &AccountExport{
		Context: json.RawMessage(`{"weos":"https://weos.io/vocab/"}`),
		Type:    "weos:AccountExport",
		Scope: AccountExportNote{
			AccountID: accountID,
			Includes:  []string{exportTypeSlug},
			Excludes:  []string{"pantry", "food-item", "photos", "meal logs", "everything that is not a recipe"},
			Note:      "This export holds the account's recipes and nothing else. Pantry items, photos, meal logs and every other kind of data are not in it.",
		},
		Graph: []json.RawMessage{},
	}
	rt, err := h.cfg.ResourceTypes.GetBySlug(ctx, exportTypeSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return doc, nil // the type is not installed, so there are no recipes to export
		}
		return nil, err
	}
	if ldCtx := rt.Context(); len(ldCtx) > 0 {
		merged, mergeErr := mergeContexts(ldCtx, doc.Context)
		if mergeErr != nil {
			return nil, mergeErr
		}
		doc.Context = merged
	}

	filters := []repositories.FilterCondition{{Field: "accountId", Operator: "eq", Value: accountID}}
	// IsAdmin here means "no per-creator filter": the account filter above is
	// the whole of the scope, deliberately.
	scope := &repositories.VisibilityScope{AccountID: accountID, IsAdmin: true}
	cursor := ""
	for {
		page, err := h.cfg.Resources.FindAllByTypeWithFilters(ctx, exportTypeSlug, filters, cursor, 200,
			repositories.SortOptions{}, scope)
		if err != nil {
			return nil, err
		}
		for _, resource := range page.Data {
			// Defense in depth against a listing that ignored the filter: a
			// row from any other account is never written into the document.
			if resource.AccountID() != accountID {
				continue
			}
			doc.Graph = append(doc.Graph, exportNodes(resource.Data())...)
		}
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	return doc, nil
}

// exportNodes lifts a stored resource into graph nodes. A resource is stored
// as a JSON-LD document of its own, with a @context and a @graph; the export
// carries the type's context once at the top, so each node is written without
// its own copy. A document with no @graph is one node.
func exportNodes(data json.RawMessage) []json.RawMessage {
	var body struct {
		Graph []json.RawMessage `json:"@graph"`
	}
	if err := json.Unmarshal(data, &body); err == nil && len(body.Graph) > 0 {
		return body.Graph
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(data, &node); err != nil {
		return []json.RawMessage{data}
	}
	delete(node, "@context")
	raw, err := json.Marshal(node)
	if err != nil {
		return []json.RawMessage{data}
	}
	return []json.RawMessage{raw}
}

// mergeContexts adds the export's own terms to the type's context. An object
// context takes the terms; any other shape (a string, an array) is wrapped in
// an array alongside them, which JSON-LD allows.
func mergeContexts(typeCtx, extra json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(typeCtx, &object); err == nil && object != nil {
		var terms map[string]json.RawMessage
		if err := json.Unmarshal(extra, &terms); err != nil {
			return nil, err
		}
		for term, value := range terms {
			if _, taken := object[term]; !taken {
				object[term] = value
			}
		}
		return json.Marshal(object)
	}
	return json.Marshal([]json.RawMessage{typeCtx, extra})
}
