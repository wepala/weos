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

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/cucumber/godog"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// --- small helpers ----------------------------------------------------------

func authIdentityContext(agentID, accountID string) context.Context {
	return auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: agentID, AccountIDs: []string{accountID}, ActiveAccountID: accountID,
	})
}

func entityGrant(agentID, accountID string) entities.FeatureGrantRecord {
	return entities.FeatureGrantRecord{
		SubjectType: "agent", SubjectID: agentID, AccountID: accountID, FeatureKey: grantableFeature,
		GrantedByID: agentID, Source: "acceptance",
	}
}

func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return "", fmt.Errorf("could not hash a password: %w", err)
	}
	return string(hash), nil
}

// stageConnector writes what an authorized connector leaves behind: a
// registration, an exchanged code and a refresh token naming the account.
func (w *deletionWorld) stageConnector(accountID, agentID string) error {
	client := &weosoauth.OAuthClient{
		ClientID: "connector-" + accountID, ClientName: "Claude", RedirectURIs: `["https://claude.ai/cb"]`,
		GrantTypes: `["authorization_code","refresh_token"]`, ResponseTypes: `["code"]`,
	}
	if err := w.db.Create(client).Error; err != nil {
		return fmt.Errorf("could not stage a connector registration: %w", err)
	}
	code := &weosoauth.OAuthAuthorizationCode{
		Code: "code-" + accountID, ClientID: client.ClientID, AgentID: agentID, AccountID: accountID,
		RedirectURI: "https://claude.ai/cb", CodeChallenge: "x", Status: weosoauth.StatusExchanged,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := w.db.Create(code).Error; err != nil {
		return fmt.Errorf("could not stage an authorization code: %w", err)
	}
	token := &weosoauth.OAuthRefreshToken{
		ID: "refresh-" + accountID, TokenHash: weosoauth.HashToken("raw-" + accountID), AgentID: agentID,
		AccountID: accountID, ClientID: client.ClientID, FamilyID: "refresh-" + accountID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := w.db.Create(token).Error; err != nil {
		return fmt.Errorf("could not stage a refresh token: %w", err)
	}
	return nil
}

// --- noting identifiers and checking every store ----------------------------

func (w *deletionWorld) noteIdentifiers(name string) error {
	id, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	noted := &notedIdentifiers{accountID: id}
	if err := w.db.Table("resources").Where("account_id = ?", id).Pluck("id", &noted.resourceURNs).Error; err != nil {
		return err
	}
	if err := w.db.Table("events").Where("json_extract(payload, '$.AccountID') = ? OR aggregate_id = ?", id, id).
		Pluck("id", &noted.eventIDs).Error; err != nil {
		return err
	}
	if err := w.db.Table("account_members").Where("account_id = ?", id).Pluck("agent_id", &noted.agentIDs).Error; err != nil {
		return err
	}
	if len(noted.resourceURNs) == 0 || len(noted.eventIDs) == 0 || len(noted.agentIDs) == 0 {
		return fmt.Errorf("%q was noted with %d resources, %d events and %d members; the staging left it too empty to prove anything",
			name, len(noted.resourceURNs), len(noted.eventIDs), len(noted.agentIDs))
	}
	w.noted[name] = noted
	return nil
}

func (w *deletionWorld) nothingRemainsTable(name string, _ *godog.Table) error {
	return w.nothingRemains(name)
}

// nothingRemains queries every store by the identifiers noted before the
// deletion — never through the purger's own enumeration — so a store the
// deletion forgot fails here rather than in the field.
func (w *deletionWorld) nothingRemains(name string) error {
	noted, ok := w.noted[name]
	if !ok {
		return fmt.Errorf("the identifiers of %q were never noted", name)
	}
	id := noted.accountID
	var problems []string
	check := func(store string, count int64, err error) {
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", store, err))
		} else if count != 0 {
			problems = append(problems, fmt.Sprintf("%s still holds %d row(s)", store, count))
		}
	}
	count := func(table, where string, args ...any) (int64, error) {
		var n int64
		err := w.db.Table(table).Where(where, args...).Count(&n).Error
		return n, err
	}

	n, err := count("events", "json_extract(payload, '$.AccountID') = ? OR aggregate_id = ?", id, id)
	check("events by the account id in the payload", n, err)
	n, err = count("events", "id IN ?", noted.eventIDs)
	check("events by the noted event ids", n, err)
	n, err = count("parked_events", "event_id IN ?", noted.eventIDs)
	check("parked events", n, err)
	n, err = count("resources", "account_id = ?", id)
	check("resources", n, err)
	var slugs []string
	if err := w.db.Table("resource_types").Pluck("slug", &slugs).Error; err != nil {
		return err
	}
	for _, slug := range slugs {
		table := w.projMgr.TableName(slug)
		if !w.db.Migrator().HasTable(table) || !w.db.Migrator().HasColumn(table, "account_id") {
			continue
		}
		n, err = count(table, "account_id = ?", id)
		check("projection table "+table, n, err)
	}
	n, err = count("triples", "subject IN ?", noted.resourceURNs)
	check("triples", n, err)
	n, err = count("event_references", "resource_urn IN ?", noted.resourceURNs)
	check("event_references", n, err)
	n, err = count("resource_permissions", "resource_id IN ?", noted.resourceURNs)
	check("resource_permissions", n, err)
	n, err = count("behavior_settings", "account_id = ?", id)
	check("behavior_settings", n, err)
	n, err = count("feature_grants", "account_id = ?", id)
	check("feature_grants", n, err)
	n, err = count("feature_settings", "scope_id = ?", id)
	check("feature_settings", n, err)
	for _, table := range []string{"accounts", "account_members", "auth_sessions", "invites"} {
		column := "account_id"
		if table == "accounts" {
			column = "id"
		}
		n, err = count(table, column+" = ?", id)
		check(table, n, err)
	}
	for _, table := range []string{"oauth_authorization_codes", "oauth_refresh_tokens"} {
		n, err = count(table, "account_id = ? OR agent_id IN ?", id, noted.agentIDs)
		check(table, n, err)
	}
	if w.db.Migrator().HasTable("casbin_rule") {
		n, err = count("casbin_rule", "ptype = 'g' AND v2 = ?", id)
		check("casbin grouping policies", n, err)
	}
	for _, agentID := range noted.agentIDs {
		elsewhere, err := count("account_members", "agent_id = ?", agentID)
		if err != nil {
			return err
		}
		if elsewhere > 0 {
			continue // a member with another account keeps their identity
		}
		n, err = count("agents", "id = ?", agentID)
		check("agents ("+agentID+")", n, err)
		n, err = count("credentials", "agent_id = ?", agentID)
		check("credentials ("+agentID+")", n, err)
		n, err = count("password_credentials",
			"credential_id IN (SELECT id FROM credentials WHERE agent_id = ?)", agentID)
		check("password_credentials ("+agentID+")", n, err)
	}
	if _, err := os.Stat(filepath.Join(w.uploadDir, "accounts", id)); !os.IsNotExist(err) {
		problems = append(problems, fmt.Sprintf("the file store still holds the folder accounts/%s (stat: %v)", id, err))
	}
	if len(problems) > 0 {
		return fmt.Errorf("something of %q remains:\n  %s", name, strings.Join(problems, "\n  "))
	}
	return nil
}

// --- deleting ---------------------------------------------------------------

// deleteWith sends DELETE /api/account with the cookie and body given, and
// records the answer — and the cookies it set — on the person.
func (w *deletionWorld) deleteWith(p *person, cookie, body string) error {
	return w.deleteWithContentType(p, cookie, body, "application/json")
}

func (w *deletionWorld) deleteWithContentType(p *person, cookie, body, contentType string) error {
	req, err := http.NewRequest(http.MethodDelete, w.server.URL+accountPath, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the deletion request failed: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	p.lastAnswer = &capturedAnswer{status: res.StatusCode, body: string(raw), code: refusalCode(raw)}
	w.lastCookies = res.Cookies()
	w.answers = append(w.answers, p.lastAnswer)
	return nil
}

func (w *deletionWorld) deletesAccount(email string) error {
	if w.oauth != nil {
		return w.demoDeletes(email)
	}
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	w.actor = email
	w.deletedAccountID = p.accountID
	if err := w.deleteWith(p, p.cookie, `{"confirm":"DELETE"}`); err != nil {
		return err
	}
	return nil
}

func (w *deletionWorld) asksToDeleteWithBody(email, body string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	w.actor = email
	return w.deleteWith(p, p.cookie, body)
}

func (w *deletionWorld) asksToDeleteAs(email, contentType string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	w.actor = email
	return w.deleteWithContentType(p, p.cookie, `{"confirm":"DELETE"}`, contentType)
}

func (w *deletionWorld) anonymousDelete() error {
	p := &person{email: "nobody@harborlegal.example"}
	w.people[p.email] = p
	w.order = append(w.order, p.email)
	w.actor = p.email
	return w.deleteWith(p, "", `{"confirm":"DELETE"}`)
}

func (w *deletionWorld) actorDeletes() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	return w.deleteWith(p, p.cookie, `{"confirm":"DELETE"}`)
}

func (w *deletionWorld) ownerDeletes(name string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	if err := w.deletesAccount(owner.email); err != nil {
		return err
	}
	if owner.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the owner of %q could not delete it: %s", name, describe(owner.lastAnswer))
	}
	return nil
}

// hasDeleted deletes as a Given: the deletion must have been accepted, and
// the person keeps the cookie they held, which is the point of the scenarios
// that use it.
func (w *deletionWorld) hasDeleted(email string) error {
	if err := w.deletesAccount(email); err != nil {
		return err
	}
	p := w.people[email]
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("%q could not delete their account: %s", email, describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) deletedByOwner(name string) error {
	return w.ownerDeletes(name)
}

func (w *deletionWorld) deletesAgainWithOldCookie(email string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if p.cookie == "" {
		return fmt.Errorf("%q holds no cookie from before", email)
	}
	w.actor = email
	return w.deleteWith(p, p.cookie, `{"confirm":"DELETE"}`)
}

func (w *deletionWorld) fileStoreWillRefuse(string) error {
	w.files.refuseNext = true
	return nil
}

// deletionFailedLeavingLock stages the failure the scenarios recover from: the
// owner's deletion is refused by the file store, so the lock is taken and
// nothing is removed.
func (w *deletionWorld) deletionFailedLeavingLock(name string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	w.files.refuseNext = true
	if err := w.deletesAccount(owner.email); err != nil {
		return err
	}
	if owner.lastAnswer.status == http.StatusOK {
		return fmt.Errorf("the deletion of %q was meant to fail but was accepted", name)
	}
	locked, err := w.locks.IsLocked(context.Background(), w.accounts[name])
	if err != nil || !locked {
		return fmt.Errorf("the failed deletion of %q left no lock (locked=%v, err=%v)", name, locked, err)
	}
	return nil
}

// impersonates starts an impersonation as the admin and keeps the cookie the
// instance set, so the admin's next request carries it.
func (w *deletionWorld) impersonates(admin, subject string) error {
	a, err := w.personNamed(admin)
	if err != nil {
		return err
	}
	s, err := w.personNamed(subject)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(a); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, w.server.URL+impersonatePath,
		strings.NewReader(fmt.Sprintf(`{"agent_id":%q}`, s.agentID)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", a.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%q could not impersonate %q: %d %s", admin, subject, res.StatusCode, raw)
	}
	extra := sessionCookieHeader(res.Cookies())
	if extra == "" {
		return fmt.Errorf("the impersonation set no cookie")
	}
	a.cookie = a.cookie + "; " + extra
	w.actor = admin
	return nil
}

// --- requests afterwards ----------------------------------------------------

func (w *deletionWorld) actorRequests() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	return w.request(p, http.MethodGet, projectsPath, "")
}

func (w *deletionWorld) personRequests(email string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	w.actor = email
	return w.request(p, http.MethodGet, projectsPath, "")
}

func (w *deletionWorld) requestOnSecondDevice() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if w.secondDevice == "" {
		return fmt.Errorf("no second device was signed in")
	}
	first := p.cookie
	p.cookie = w.secondDevice
	err = w.request(p, http.MethodGet, projectsPath, "")
	p.cookie = first
	return err
}

func (w *deletionWorld) readsWhoTheyAre() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	return w.request(p, http.MethodGet, mePath, "")
}

func (w *deletionWorld) readsWhoTheyAreOnSecondDevice() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if w.secondDevice == "" {
		return fmt.Errorf("no second device was signed in")
	}
	first := p.cookie
	p.cookie = w.secondDevice
	err = w.request(p, http.MethodGet, mePath, "")
	p.cookie = first
	return err
}

func (w *deletionWorld) projectsSeenInclude(email, name string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("%q was no longer served: %s", email, describe(p.lastAnswer))
	}
	if !strings.Contains(p.lastAnswer.body, name) {
		return fmt.Errorf("%q no longer sees %q: %s", email, name, p.lastAnswer.body)
	}
	return nil
}

func (w *deletionWorld) accountReadsPhoto(name, filename string) error {
	owner, err := w.ownerOf(name)
	if err != nil {
		return err
	}
	photo, ok := w.photos[name]
	if !ok {
		return fmt.Errorf("%q has no stored photo", name)
	}
	answer, err := w.get(owner.cookie, w.server.URL+photo.path)
	if err != nil {
		return err
	}
	if answer.status != http.StatusOK || answer.body != string(photo.body) {
		return fmt.Errorf("%q can no longer read %q: %s", name, filename, describe(answer))
	}
	return nil
}

func (w *deletionWorld) personRequestsFlatPhoto(email, filename string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	photo, ok := w.legacy[filename]
	if !ok {
		return fmt.Errorf("no flat file named %q has been staged", filename)
	}
	w.servedWant = photo.body
	_, err = w.get(p.cookie, w.server.URL+photo.path)
	return err
}

func (w *deletionWorld) oldPhotoNotFound() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	var old *storedPhoto
	for _, photo := range w.photos {
		old = photo
	}
	if old == nil {
		return fmt.Errorf("no photo was stored before the deletion")
	}
	answer, err := w.get(p.cookie, w.server.URL+old.path)
	if err != nil {
		return err
	}
	if answer.status != http.StatusNotFound {
		return fmt.Errorf("the old photo URL answered %s, want 404", describe(answer))
	}
	return nil
}

// rebuildProjections clears every synchronous read model and replays the
// event history through the same handlers the write path uses, so what comes
// back is exactly what the surviving events describe.
func (w *deletionWorld) rebuildProjections() error {
	var slugs []string
	if err := w.db.Table("resource_types").Pluck("slug", &slugs).Error; err != nil {
		return err
	}
	for _, slug := range slugs {
		table := w.projMgr.TableName(slug)
		if !w.db.Migrator().HasTable(table) {
			continue
		}
		if err := w.db.Exec("DELETE FROM " + table).Error; err != nil { //nolint:gosec // table names come from the type registry
			return fmt.Errorf("could not clear %s before the rebuild: %w", table, err)
		}
	}
	if err := w.db.Exec("DELETE FROM resources").Error; err != nil {
		return err
	}
	runtime := application.ReprojectRuntime{EventStore: w.eventStore, Dispatcher: w.dispatcher, Logger: w.logger}
	if _, err := application.Reproject(context.Background(), runtime, application.ReprojectOptions{}); err != nil {
		return fmt.Errorf("the rebuild from event history failed: %w", err)
	}
	return nil
}

// --- what came back ---------------------------------------------------------

func (w *deletionWorld) actorStatusIs(want int) error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != want {
		return fmt.Errorf("expected %d, got %s", want, describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) actorCodeIs(want string) error {
	p, err := w.current()
	if err != nil {
		return err
	}
	return matchCode(p, want)
}

func (w *deletionWorld) refusalIsNotErasurePending() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer != nil && p.lastAnswer.code == apimw.CodeAccountErasurePending {
		return fmt.Errorf("the refusal offers a deletion path to a suspended account: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) answerClearsCookies() error {
	cleared := map[string]bool{}
	for _, c := range w.lastCookies {
		if c.MaxAge < 0 || c.Value == "" {
			cleared[c.Name] = true
		}
	}
	if !cleared["weos-session"] || !cleared["pericarp_token"] {
		return fmt.Errorf("the answer cleared %v, want both weos-session and pericarp_token", cleared)
	}
	return nil
}

func (w *deletionWorld) membersLost() (int, error) {
	p, err := w.current()
	if err != nil {
		return 0, err
	}
	var body struct {
		Data handlers.AccountDeletedResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(p.lastAnswer.body), &body); err != nil {
		return 0, fmt.Errorf("the deletion answer is not the envelope: %s", describe(p.lastAnswer))
	}
	return body.Data.MembersLost, nil
}

func (w *deletionWorld) answerReportsMembersLost(want int) error {
	got, err := w.membersLost()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the answer reports %d people lost the account, want %d", got, want)
	}
	return nil
}

func (w *deletionWorld) answerSaysUnfinished() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer == nil || p.lastAnswer.status < 500 {
		return fmt.Errorf("expected the deletion to report a failure, got %s", describe(p.lastAnswer))
	}
	if p.lastAnswer.code != handlers.CodeAccountErasureUnfinished || !strings.Contains(p.lastAnswer.body, "run again") {
		return fmt.Errorf("the answer does not say the deletion is unfinished and can be run again: %s", describe(p.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) answerSaysMemberCount(want int) error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the identity read was not served: %s", describe(p.lastAnswer))
	}
	var body struct {
		Data struct {
			MemberCount *int `json:"member_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(p.lastAnswer.body), &body); err != nil || body.Data.MemberCount == nil {
		return fmt.Errorf("the identity read reports no member count: %s", describe(p.lastAnswer))
	}
	if *body.Data.MemberCount != want {
		return fmt.Errorf("the identity read reports %d members, want %d", *body.Data.MemberCount, want)
	}
	return nil
}

func (w *deletionWorld) sessionAccount() (string, error) {
	p, err := w.current()
	if err != nil {
		return "", err
	}
	info, err := w.authService.ValidateSession(context.Background(), p.sessionID)
	if err != nil || info == nil {
		return "", fmt.Errorf("could not read the session of %q: %v", p.email, err)
	}
	return info.AccountID, nil
}

func (w *deletionWorld) actingAccountIs(name string) error {
	got, err := w.sessionAccount()
	if err != nil {
		return err
	}
	if got != w.accounts[name] {
		return fmt.Errorf("requests act in account %q, want %q (%s)", got, w.accounts[name], name)
	}
	return nil
}

func (w *deletionWorld) actingAccountIsNot(name string) error {
	got, err := w.sessionAccount()
	if err != nil {
		return err
	}
	if got == "" || got == w.accounts[name] {
		return fmt.Errorf("requests act in %q, which is %s or nothing", got, name)
	}
	return nil
}

func (w *deletionWorld) actingAccountIsNotTheDeleted() error {
	got, err := w.sessionAccount()
	if err != nil {
		return err
	}
	if got == "" || got == w.deletedAccountID {
		return fmt.Errorf("requests act in %q, which is the deleted account %q or nothing", got, w.deletedAccountID)
	}
	return nil
}

func (w *deletionWorld) pantryHoldsNothing() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	for _, path := range []string{"/api/pantry", "/api/food-item"} {
		if err := w.request(p, http.MethodGet, path, ""); err != nil {
			return err
		}
		if p.lastAnswer.status != http.StatusOK {
			return fmt.Errorf("%s was not served: %s", path, describe(p.lastAnswer))
		}
		var body struct {
			Data []any `json:"data"`
		}
		if err := json.Unmarshal([]byte(p.lastAnswer.body), &body); err != nil {
			return fmt.Errorf("%s is not a listing: %s", path, describe(p.lastAnswer))
		}
		if len(body.Data) != 0 {
			return fmt.Errorf("%s still holds %d item(s): %s", path, len(body.Data), p.lastAnswer.body)
		}
	}
	return nil
}

func (w *deletionWorld) recipesSeen(name string, want bool) error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, "/api/recipe", ""); err != nil {
		return err
	}
	if p.lastAnswer.status != http.StatusOK {
		return fmt.Errorf("the recipe listing was not served: %s", describe(p.lastAnswer))
	}
	if got := strings.Contains(p.lastAnswer.body, name); got != want {
		return fmt.Errorf("recipe %q visible=%v, want %v: %s", name, got, want, p.lastAnswer.body)
	}
	return nil
}

func (w *deletionWorld) recipeStillThere(name, accountName string) error {
	owner, err := w.ownerOf(accountName)
	if err != nil {
		return err
	}
	if err := w.request(owner, http.MethodGet, "/api/recipe", ""); err != nil {
		return err
	}
	if owner.lastAnswer.status != http.StatusOK || !strings.Contains(owner.lastAnswer.body, name) {
		return fmt.Errorf("the recipe %q is no longer there for %q: %s", name, accountName, describe(owner.lastAnswer))
	}
	return nil
}

func (w *deletionWorld) recipeStillStored(name, accountName string) error {
	var n int64
	err := w.db.Table("resources").Where("account_id = ? AND type_slug = 'recipe' AND data LIKE ?",
		w.accounts[accountName], "%"+name+"%").Count(&n).Error
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("the recipe %q is no longer stored for %q", name, accountName)
	}
	return nil
}

func (w *deletionWorld) accountStoredAndLocked(name string) error {
	ctx := context.Background()
	account, err := w.accountRepo.FindByID(ctx, w.accounts[name])
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("the account %q is gone even though its deletion did not finish", name)
	}
	if account.Active() {
		return fmt.Errorf("the account %q is still active after a deletion began", name)
	}
	locked, err := w.locks.IsLocked(ctx, account.GetID())
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("the account %q is not marked as being erased", name)
	}
	return nil
}

// --- the operator -----------------------------------------------------------

// operatorRuns runs the built binary against the instance's store, with the
// account's id substituted for "<the id of X>".
func (w *deletionWorld) operatorRuns(command string) error {
	for name, id := range w.accounts {
		command = strings.ReplaceAll(command, "<the id of "+name+">", id)
	}
	binary, err := weosBinary()
	if err != nil {
		return err
	}
	args := strings.Fields(command)
	if len(args) > 0 && args[0] == "weos" {
		args = args[1:]
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = w.tmpDir
	cmd.Env = append(os.Environ(),
		"DATABASE_DSN="+w.dsn,
		"STORAGE_LOCAL_PATH="+w.uploadDir,
		"LOG_LEVEL=error",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	w.lastRun = commandRun{stdout: stdout.String(), stderr: stderr.String(), args: args}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return fmt.Errorf("could not run the command: %w", runErr)
		}
		w.lastRun.exitCode = exit.ExitCode()
	}
	return nil
}

func (w *deletionWorld) commandSucceeded() error {
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("the command exited %d: %s", w.lastRun.exitCode, w.lastRun.said())
	}
	return nil
}

func (w *deletionWorld) commandFailed() error {
	if w.lastRun.exitCode == 0 {
		return fmt.Errorf("the command exited 0, want a failure: %s", w.lastRun.said())
	}
	return nil
}

func (w *deletionWorld) failureNames(text string) error {
	if !strings.Contains(w.lastRun.said(), text) {
		return fmt.Errorf("the failure does not name %q: %s", text, w.lastRun.said())
	}
	return nil
}

// --- the connector, on the demo instance ------------------------------------

// bootDemo boots the OAuth suite's demo instance with the account routes
// mounted on it, so a connector's token can be tried after a deletion.
func (w *deletionWorld) bootDemo() error {
	w.oauth = &oauthWorld{}
	w.oauth.mountExtraRoutes = func(api *echo.Group, _ config.Config, sessionStore sessions.Store, _ authapp.JWTService) {
		accountHandler := handlers.NewAccountHandler(handlers.AccountHandlerConfig{
			Erasure:        w.demoErasure(),
			Accounts:       w.oauth.accountRepo,
			SessionManager: w.oauth.sessionManager,
			Store:          sessionStore,
			Logger:         w.oauth.logger,
		})
		api.DELETE("/account", accountHandler.Delete, apimw.SessionAuthForErasure(
			w.oauth.sessionManager, w.oauth.authService, w.oauth.accountRepo, w.oauth.erasureLocks, w.oauth.logger))
	}
	return w.oauth.boot(bootOpts{password: true, dynamicRegistration: true})
}

// demoErasure resolves the erasure service out of the demo instance lazily:
// the routes are mounted before the world's fields are all populated.
func (w *deletionWorld) demoErasure() handlers.AccountErasureRunner {
	return demoErasureRunner{w: w}
}

type demoErasureRunner struct{ w *deletionWorld }

func (r demoErasureRunner) Erase(ctx context.Context, cmd application.EraseAccountCommand) (*application.ErasureResult, error) {
	if r.w.oauth.erasure == nil {
		return nil, errors.New("the demo instance has no erasure service")
	}
	return r.w.oauth.erasure.Erase(ctx, cmd)
}

func (w *deletionWorld) demoAccount(email, password string) error {
	return w.oauth.anAccount(email, password)
}

func (w *deletionWorld) claudeAuthorizedBy(email string) error {
	if err := w.oauth.authorizedBySigningIn(email); err != nil {
		return err
	}
	if w.oauth.accessToken() == "" {
		return fmt.Errorf("the connector got no token: %v", w.oauth.tokens)
	}
	return nil
}

// claudeAlsoAuthorizedBy runs a second person's authorization in a fresh
// browser, keeping the first connector's token as the one "Claude" means.
func (w *deletionWorld) claudeAlsoAuthorizedBy(email string) error {
	w.firstToken = w.oauth.accessToken()
	// The first person's browser is put back afterwards: what the scenario
	// does next, it does as them.
	firstBrowser := w.oauth.client.Jar
	defer func() { w.oauth.client.Jar = firstBrowser }()
	jar, _ := cookiejar.New(nil)
	w.oauth.client.Jar = jar
	w.oauth.code = ""
	if err := w.oauth.signedInAs(email); err != nil {
		return err
	}
	if err := w.oauth.authorize(nil); err != nil {
		return err
	}
	if w.oauth.code == "" {
		return fmt.Errorf("no authorization code was issued for %q: %s %s", email, w.oauth.last.location, w.oauth.last.body)
	}
	if err := w.oauth.exchange(claudeVerifier); err != nil {
		return err
	}
	w.oauth.secondToken = w.oauth.accessToken()
	if w.oauth.secondToken == "" {
		return fmt.Errorf("the second connector got no token")
	}
	// Put the first person's token back as the current one.
	w.oauth.tokens = append(w.oauth.tokens, map[string]any{"access_token": w.firstToken})
	w.oauth.tokenStatus = append(w.oauth.tokenStatus, http.StatusOK)
	return nil
}

// demoDeletes deletes as the person the demo instance's browser is signed in
// as. Nothing asserts on this answer, so it fails loudly here instead.
func (w *deletionWorld) demoDeletes(email string) error {
	res, err := w.demoRequest(http.MethodDelete, accountPath, `{"confirm":"DELETE"}`)
	if err != nil {
		return err
	}
	if res.status != http.StatusOK {
		return fmt.Errorf("%q could not delete their account on the demo instance: %s", email, describe(res))
	}
	w.connectorStatuses = nil
	return nil
}

func (w *deletionWorld) demoRequest(method, path, body string) (*capturedAnswer, error) {
	req, err := http.NewRequest(method, w.oauth.server.URL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := w.oauth.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return &capturedAnswer{status: res.StatusCode, body: string(raw), code: refusalCode(raw)}, nil
}

// claudeListsTools asks with whatever token is current. A refusal is recorded
// rather than returned: the scenario decides what it means.
func (w *deletionWorld) claudeListsTools() error {
	_ = w.oauth.listTools()
	w.connectorStatuses = append(w.connectorStatuses, w.oauth.lastMCPStatus)
	return nil
}

func (w *deletionWorld) claudeCreatesTask(name string) error {
	_ = w.oauth.createTask(name)
	w.connectorStatuses = append(w.connectorStatuses, w.oauth.lastMCPStatus)
	return nil
}

func (w *deletionWorld) claudeActingAsListsTools(string) error {
	token := w.oauth.secondToken
	if token == "" {
		return fmt.Errorf("the second person's connector has no token")
	}
	w.oauth.mcpSessionID = ""
	w.oauth.mcpInitializedFor = ""
	if err := w.oauth.initializeMCP(token); err != nil {
		return fmt.Errorf("the second connector was refused: %w", err)
	}
	reply, err := w.oauth.mcpCall(token, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	w.oauth.last = authorizeAnswer{status: http.StatusOK, body: fmt.Sprintf("%v", reply)}
	return nil
}

func (w *deletionWorld) claudePresentsRefreshToken() error {
	var refresh string
	for i := len(w.oauth.tokens) - 1; i >= 0 && refresh == ""; i-- {
		if w.oauth.tokenStatus[i] == http.StatusOK {
			refresh, _ = w.oauth.tokens[i]["refresh_token"].(string)
		}
	}
	if refresh == "" {
		return fmt.Errorf("the connector was never issued a refresh token: %v", w.oauth.tokens)
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("client_id", w.oauth.clientID)
	res, err := w.oauth.client.PostForm(w.oauth.server.URL+"/oauth/token", form)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	w.oauth.tokens = append(w.oauth.tokens, payload)
	w.oauth.tokenStatus = append(w.oauth.tokenStatus, res.StatusCode)
	return nil
}

func (w *deletionWorld) bothConnectorRequestsRefused() error {
	if len(w.connectorStatuses) < 2 {
		return fmt.Errorf("Claude made %d request(s), not two", len(w.connectorStatuses))
	}
	for i, status := range w.connectorStatuses {
		if status != http.StatusUnauthorized {
			return fmt.Errorf("Claude's request %d answered %d, want 401", i+1, status)
		}
	}
	return nil
}

func (w *deletionWorld) instanceListedTools() error { return w.oauth.listedTools() }

func (w *deletionWorld) exchangeRefused() error { return w.oauth.lastExchangeRefused() }

// noAccessTokenIssued reads the LAST exchange, not every one: the first
// exchange, before the deletion, was meant to succeed.
func (w *deletionWorld) noAccessTokenIssued() error {
	if len(w.oauth.tokens) == 0 {
		return fmt.Errorf("no exchange was attempted")
	}
	last := w.oauth.tokens[len(w.oauth.tokens)-1]
	if token, _ := last["access_token"].(string); token != "" {
		return fmt.Errorf("an access token was issued: %v", last)
	}
	return nil
}

func (w *deletionWorld) claudeStillRegistered() error {
	var n int64
	if err := w.oauth.db.Table("oauth_clients").Where("client_id = ?", w.oauth.clientID).Count(&n).Error; err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("the connector registration %q is gone", w.oauth.clientID)
	}
	return nil
}

// noRowNamesDeletedAccount checks the store the connector's write would have
// recreated rows in. The demo instance runs no per-account graph, so the
// directory half of the promise has nothing to hold: the base it would live
// under does not exist.
func (w *deletionWorld) noRowNamesDeletedAccount() error {
	var ids []string
	if err := w.oauth.db.Table("accounts").Pluck("id", &ids).Error; err != nil {
		return err
	}
	var n int64
	err := w.oauth.db.Table("resources").Where("account_id <> '' AND account_id NOT IN (SELECT id FROM accounts)").Count(&n).Error
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("%d resource row(s) name an account that no longer exists", n)
	}
	if entries, err := os.ReadDir(filepath.Join(w.oauth.tmpDir, "graph")); err == nil && len(entries) > 0 {
		return fmt.Errorf("a graph store directory was created: %v", entries)
	}
	return nil
}

// --- exporting --------------------------------------------------------------

func (w *deletionWorld) exportsAccount(email string) error {
	p, err := w.personNamed(email)
	if err != nil {
		return err
	}
	if err := w.ensureSignedIn(p); err != nil {
		return err
	}
	w.actor = email
	answer, err := w.get(p.cookie, w.server.URL+accountExportPath)
	if err != nil {
		return err
	}
	w.export = answer
	p.lastAnswer = answer
	return nil
}

func (w *deletionWorld) anonymousExport() error {
	p := &person{email: "nobody@harborlegal.example"}
	w.people[p.email] = p
	w.order = append(w.order, p.email)
	w.actor = p.email
	answer, err := w.get("", w.server.URL+accountExportPath)
	if err != nil {
		return err
	}
	w.export = answer
	p.lastAnswer = answer
	return nil
}

type exportDocument struct {
	Context json.RawMessage  `json:"@context"`
	Graph   []map[string]any `json:"@graph"`
	Scope   struct {
		Includes []string `json:"includes"`
		Note     string   `json:"note"`
	} `json:"weos:exportScope"`
}

func (w *deletionWorld) exportDoc() (*exportDocument, error) {
	if w.export == nil {
		return nil, fmt.Errorf("no export was requested")
	}
	if w.export.status != http.StatusOK {
		return nil, fmt.Errorf("the export was not served: %s", describe(w.export))
	}
	var doc exportDocument
	if err := json.Unmarshal([]byte(w.export.body), &doc); err != nil {
		return nil, fmt.Errorf("the export is not a JSON document: %w", err)
	}
	return &doc, nil
}

func (w *deletionWorld) exportIsJSONLD() error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	if len(doc.Context) == 0 || doc.Graph == nil {
		return fmt.Errorf("the export lacks a @context or a @graph: %s", w.export.body)
	}
	return nil
}

func exportNames(doc *exportDocument) []string {
	names := []string{}
	for _, node := range doc.Graph {
		if name, ok := node["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func (w *deletionWorld) exportHolds(recipes ...string) error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	names := exportNames(doc)
	for _, want := range recipes {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("the export does not hold the recipe %q: %v", want, names)
		}
	}
	return nil
}

func (w *deletionWorld) exportDoesNotHold(recipe string) error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	for _, name := range exportNames(doc) {
		if name == recipe {
			return fmt.Errorf("the export holds %q, which belongs to another account", recipe)
		}
	}
	return nil
}

// exportHoldsNoPantryOrPhoto reads the graph, not the whole document: the
// document's own description of what it excludes names those things on
// purpose.
func (w *deletionWorld) exportHoldsNoPantryOrPhoto() error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	for _, node := range doc.Graph {
		raw, _ := json.Marshal(node)
		text := strings.ToLower(string(raw))
		for _, forbidden := range []string{"basmati", "kitchen", "pantry", "fooditem", "food-item", "imageobject", "lasagna.jpg"} {
			if strings.Contains(text, forbidden) {
				return fmt.Errorf("the export's graph holds a node mentioning %q: %s", forbidden, raw)
			}
		}
	}
	return nil
}

func (w *deletionWorld) exportIsEmpty() error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	if len(doc.Graph) != 0 {
		return fmt.Errorf("the export holds %d node(s), want none", len(doc.Graph))
	}
	return nil
}

func (w *deletionWorld) exportSaysRecipesOnly() error {
	doc, err := w.exportDoc()
	if err != nil {
		return err
	}
	if len(doc.Scope.Includes) != 1 || doc.Scope.Includes[0] != "recipe" {
		return fmt.Errorf("the export says it includes %v, want recipes only", doc.Scope.Includes)
	}
	if !strings.Contains(strings.ToLower(doc.Scope.Note), "nothing else") {
		return fmt.Errorf("the export does not say it holds nothing else: %q", doc.Scope.Note)
	}
	return nil
}
