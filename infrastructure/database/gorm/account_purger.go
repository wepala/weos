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

package gorm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// purgeChunk bounds every IN list the purge binds. SQLite caps bound
// parameters per statement (999 on older builds), so a large account is
// deleted in slices of this many ids rather than in one statement.
const purgeChunk = 500

// AccountErasureLocks is the GORM AccountErasureLocks.
type AccountErasureLocks struct {
	db *gorm.DB
}

// ProvideAccountErasureLocks supplies the lock repository. The table is
// migrated with the rest of the core models in ProvideGormDB.
func ProvideAccountErasureLocks(db *gorm.DB) repositories.AccountErasureLocks {
	return &AccountErasureLocks{db: db}
}

func (r *AccountErasureLocks) Lock(ctx context.Context, accountID, requestedBy string) error {
	if accountID == "" {
		return errors.New("account erasure: account id must not be empty")
	}
	// A re-run takes the lock it already holds; the first request's record
	// of who asked and when is kept.
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}},
		DoNothing: true,
	}).Create(&models.AccountErasure{
		AccountID:   accountID,
		RequestedBy: requestedBy,
		StartedAt:   time.Now(),
	}).Error
	if err != nil {
		return fmt.Errorf("account erasure: lock account %q: %w", accountID, err)
	}
	return nil
}

func (r *AccountErasureLocks) IsLocked(ctx context.Context, accountID string) (bool, error) {
	if accountID == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AccountErasure{}).
		Where("account_id = ?", accountID).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("account erasure: read lock for %q: %w", accountID, err)
	}
	return count > 0, nil
}

// AccountPurger is the GORM AccountDataPurger.
//
// This is the one place in the tree that deletes event rows. Article I of
// the constitution names account erasure as its single exception and confines
// it here; nothing else holds this type, and nothing reachable from a unit of
// work calls it.
type AccountPurger struct {
	db      *gorm.DB
	projMgr repositories.ProjectionManager
	logger  entities.Logger
}

// ProvideAccountPurger supplies the purger for the erasure service.
func ProvideAccountPurger(params struct {
	fx.In
	DB      *gorm.DB
	ProjMgr repositories.ProjectionManager
	Logger  entities.Logger
}) repositories.AccountDataPurger {
	return &AccountPurger{db: params.DB, projMgr: params.ProjMgr, logger: params.Logger}
}

// NewAccountPurgerForTest builds a purger without fx wiring.
func NewAccountPurgerForTest(db *gorm.DB, projMgr repositories.ProjectionManager, logger entities.Logger) *AccountPurger {
	return &AccountPurger{db: db, projMgr: projMgr, logger: logger}
}

func (p *AccountPurger) Enumerate(ctx context.Context, accountID string) (*repositories.AccountEnumeration, error) {
	if accountID == "" {
		return nil, errors.New("account erasure: account id must not be empty")
	}
	db := p.db.WithContext(ctx)
	urns, err := resourceURNsOf(db, accountID)
	if err != nil {
		return nil, err
	}
	var members int64
	if err := db.Table("account_members").Where("account_id = ?", accountID).Count(&members).Error; err != nil {
		return nil, fmt.Errorf("account erasure: count members of %q: %w", accountID, err)
	}
	return &repositories.AccountEnumeration{ResourceURNs: urns, Members: int(members)}, nil
}

// Purge deletes every row the account owns in one transaction. The order is
// the one the design record fixes: events and their parked copies first, then
// everything keyed by the account's resources, then the account's settings,
// connector access, authorization, memberships, the members who belonged to
// nothing else, and last the account row and its erasure lock.
func (p *AccountPurger) Purge(ctx context.Context, accountID string) (*repositories.PurgeReport, error) {
	if accountID == "" {
		return nil, errors.New("account erasure: account id must not be empty")
	}
	report := &repositories.PurgeReport{}
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A gone account row with nothing else naming the account is a
		// deletion that already finished — the second of two that arrived
		// together, or a re-run — and is reported as such rather than
		// purged again to no effect (wm-4cysr). A gone row with rows still
		// naming the account is the orphan case, and those are purged.
		var rows int64
		if err := tx.Table("accounts").Where("id = ?", accountID).Count(&rows).Error; err != nil {
			return fmt.Errorf("account erasure: read the account row of %q: %w", accountID, err)
		}
		if rows == 0 {
			left, err := remains(tx, accountID)
			if err != nil {
				return err
			}
			if !left {
				return repositories.ErrNothingToPurge
			}
		}
		facts, err := p.gather(tx, accountID)
		if err != nil {
			return err
		}
		report.Members = len(facts.members)
		report.DeletedAgents = facts.deletedAgents

		steps := []func(*gorm.DB, *accountFacts, *repositories.PurgeReport) error{
			p.purgeEvents,
			purgeResourceIndexes,
			p.purgeProjections,
			purgeResources,
			purgeSettings,
			purgeConnectorAccess,
			purgeAuthorization,
			purgeIdentity,
			purgeAccountRow,
		}
		for _, step := range steps {
			if err := step(tx, facts, report); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

// accountFacts is what the purge enumerates before it deletes anything. Every
// delete below works from these ids, never from a join made after an earlier
// step removed the rows the join would have needed.
type accountFacts struct {
	accountID     string
	members       []string
	deletedAgents []string
	credentials   []string // credential ids of the deleted agents
	// invites is every invitation that goes: the account's own, and any from
	// another account that names a deleted person by agent id or by email.
	invites      []string
	resourceURNs []string
	// aggregates is every aggregate whose events go: the account, its
	// resources, the aggregates that named it in a payload, and the auth
	// aggregates of the deleted agents. Events are deleted by this set and
	// only by this set, which is what keeps an event of a surviving aggregate
	// safe even when it shares a transaction with one of these.
	aggregates []string
}

func (p *AccountPurger) gather(tx *gorm.DB, accountID string) (*accountFacts, error) {
	facts := &accountFacts{accountID: accountID}
	aggregates := map[string]bool{accountID: true}

	if err := tx.Table("account_members").Where("account_id = ?", accountID).
		Pluck("agent_id", &facts.members).Error; err != nil {
		return nil, fmt.Errorf("account erasure: list members of %q: %w", accountID, err)
	}
	for _, agentID := range facts.members {
		var elsewhere int64
		err := tx.Table("account_members").
			Where("agent_id = ? AND account_id <> ?", agentID, accountID).Count(&elsewhere).Error
		if err != nil {
			return nil, fmt.Errorf("account erasure: read other memberships of %q: %w", agentID, err)
		}
		if elsewhere == 0 {
			facts.deletedAgents = append(facts.deletedAgents, agentID)
			aggregates[agentID] = true
		}
	}

	urns, err := resourceURNsOf(tx, accountID)
	if err != nil {
		return nil, err
	}
	facts.resourceURNs = urns
	for _, urn := range urns {
		aggregates[urn] = true
	}

	// Aggregates that named the account in an event payload: resources whose
	// rows are already gone, and triple aggregates. Enumerated by the payload
	// column so a store that arrived without its projections still purges.
	var named []string
	if err := tx.Table("events").Where(payloadAccountClause(tx), accountID).
		Distinct().Pluck("aggregate_id", &named).Error; err != nil {
		return nil, fmt.Errorf("account erasure: enumerate events naming %q: %w", accountID, err)
	}
	for _, id := range named {
		aggregates[id] = true
	}

	if len(facts.deletedAgents) > 0 {
		var err error
		if facts.credentials, err = pluckByChunk(tx, "credentials", "id", "agent_id", facts.deletedAgents); err != nil {
			return nil, fmt.Errorf("account erasure: list credentials of deleted agents: %w", err)
		}
		passwordCredentials, err := pluckByChunk(tx, "password_credentials", "id", "credential_id", facts.credentials)
		if err != nil {
			return nil, fmt.Errorf("account erasure: list password credentials of deleted agents: %w", err)
		}
		sessions, err := pluckByChunk(tx, "auth_sessions", "id", "agent_id", facts.deletedAgents)
		if err != nil {
			return nil, fmt.Errorf("account erasure: list sessions of deleted agents: %w", err)
		}
		for _, id := range facts.credentials {
			aggregates[id] = true
		}
		for _, id := range passwordCredentials {
			aggregates[id] = true
		}
		for _, id := range sessions {
			aggregates[id] = true
		}
	}

	// The account's own invitations, and — because an invitation into
	// another account names the person by the skeleton agent it minted and
	// by their email — any from another account that names a deleted person
	// either way. Left behind, the first dangles and the second keeps the
	// email after everything else is gone (wm-i2oni).
	if err := tx.Table("invites").Where("account_id = ?", accountID).Pluck("id", &facts.invites).Error; err != nil {
		return nil, fmt.Errorf("account erasure: list invites of %q: %w", accountID, err)
	}
	if len(facts.deletedAgents) > 0 {
		byAgent, err := pluckByChunk(tx, "invites", "id", "invitee_agent_id", facts.deletedAgents)
		if err != nil {
			return nil, fmt.Errorf("account erasure: list invites naming deleted agents: %w", err)
		}
		facts.invites = append(facts.invites, byAgent...)
		var emails []string
		if err := forEachChunk(facts.deletedAgents, func(chunk []string) error {
			var found []string
			if err := tx.Table("credentials").Where("agent_id IN ? AND email <> ''", chunk).Distinct().Pluck("email", &found).Error; err != nil {
				return err
			}
			emails = append(emails, found...)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("account erasure: list emails of deleted agents: %w", err)
		}
		byEmail, err := pluckByChunk(tx, "invites", "id", "email", emails)
		if err != nil {
			return nil, fmt.Errorf("account erasure: list invites naming deleted emails: %w", err)
		}
		facts.invites = append(facts.invites, byEmail...)
	}
	for _, id := range facts.invites {
		aggregates[id] = true
	}

	facts.aggregates = make([]string, 0, len(aggregates))
	for id := range aggregates {
		facts.aggregates = append(facts.aggregates, id)
	}
	return facts, nil
}

// purgeEvents deletes the events of the aggregates being deleted, and the
// parked copies of those events. This is the Article I exception.
//
// The sweep by shared transaction the design record describes is bounded
// here to aggregates being deleted, which makes it the same set as deleting
// by aggregate id: an event that shares a transaction with one of the
// account's events but belongs to an aggregate that survives — a person
// shared between two accounts, an invite flow — is never taken.
func (p *AccountPurger) purgeEvents(tx *gorm.DB, facts *accountFacts, report *repositories.PurgeReport) error {
	parked := tx.Migrator().HasTable("parked_events")
	return forEachChunk(facts.aggregates, func(chunk []string) error {
		if parked {
			err := tx.Exec("DELETE FROM parked_events WHERE event_id IN (SELECT id FROM events WHERE aggregate_id IN ?)", chunk).Error
			if err != nil {
				return fmt.Errorf("account erasure: delete parked events: %w", err)
			}
		}
		res := tx.Table("events").Where("aggregate_id IN ?", chunk).Delete(map[string]any{})
		if res.Error != nil {
			return fmt.Errorf("account erasure: delete events: %w", res.Error)
		}
		report.Events += int(res.RowsAffected)
		return nil
	})
}

// purgeResourceIndexes removes what is keyed by the account's resource URNs:
// event references, permissions, triples, and the lexical index.
func purgeResourceIndexes(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	search := tx.Migrator().HasTable("resource_search")
	return forEachChunk(facts.resourceURNs, func(chunk []string) error {
		if err := tx.Exec("DELETE FROM event_references WHERE resource_urn IN ?", chunk).Error; err != nil {
			return fmt.Errorf("account erasure: delete event references: %w", err)
		}
		if err := tx.Exec("DELETE FROM resource_permissions WHERE resource_id IN ?", chunk).Error; err != nil {
			return fmt.Errorf("account erasure: delete resource permissions: %w", err)
		}
		if err := tx.Exec("DELETE FROM triples WHERE subject IN ?", chunk).Error; err != nil {
			return fmt.Errorf("account erasure: delete triples: %w", err)
		}
		if search {
			if err := tx.Exec("DELETE FROM resource_search WHERE id IN ?", chunk).Error; err != nil {
				return fmt.Errorf("account erasure: delete lexical index rows: %w", err)
			}
		}
		return nil
	})
}

// purgeProjections deletes the account's rows from every projection table,
// including the ancestor tables a subclassed type also projects into. Every
// installed type is visited rather than only the types the account used, so
// a row a projector wrote under a type the account never named directly is
// still found.
func (p *AccountPurger) purgeProjections(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	var slugs []string
	if err := tx.Unscoped().Model(&models.ResourceType{}).Pluck("slug", &slugs).Error; err != nil {
		return fmt.Errorf("account erasure: list resource types: %w", err)
	}
	for _, slug := range slugs {
		table := p.projMgr.TableName(slug)
		if !tx.Migrator().HasTable(table) || !tx.Migrator().HasColumn(table, "account_id") {
			continue
		}
		if err := tx.Table(table).Where("account_id = ?", facts.accountID).Delete(map[string]any{}).Error; err != nil {
			return fmt.Errorf("account erasure: delete from %s: %w", table, err)
		}
	}
	return nil
}

func purgeResources(tx *gorm.DB, facts *accountFacts, report *repositories.PurgeReport) error {
	res := tx.Unscoped().Where("account_id = ?", facts.accountID).Delete(&models.Resource{})
	if res.Error != nil {
		return fmt.Errorf("account erasure: delete resources: %w", res.Error)
	}
	report.Resources = int(res.RowsAffected)
	return nil
}

func purgeSettings(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	if err := tx.Where("account_id = ?", facts.accountID).Delete(&models.BehaviorSettings{}).Error; err != nil {
		return fmt.Errorf("account erasure: delete behavior settings: %w", err)
	}
	if err := tx.Where("account_id = ?", facts.accountID).Delete(&models.FeatureGrant{}).Error; err != nil {
		return fmt.Errorf("account erasure: delete feature grants: %w", err)
	}
	err := tx.Where("scope_type = ? AND scope_id = ?", repositories.FeatureScopeAccount, facts.accountID).
		Delete(&models.FeatureSetting{}).Error
	if err != nil {
		return fmt.Errorf("account erasure: delete feature settings: %w", err)
	}
	return nil
}

// purgeConnectorAccess deletes the authorization codes and refresh tokens
// that grant access to the account or belong to a deleted agent, then the
// registrations those named — but a registration only when no code or token
// from any account still names it. A client registers before anyone signs
// in, so the row names no account of its own; it is shared for as long as
// anybody's token points at it.
func purgeConnectorAccess(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	for _, table := range []string{"oauth_authorization_codes", "oauth_refresh_tokens"} {
		if !tx.Migrator().HasTable(table) {
			return nil
		}
	}
	clients := map[string]bool{}
	collect := func(table string, where string, args ...any) error {
		var ids []string
		if err := tx.Table(table).Where(where, args...).Distinct().Pluck("client_id", &ids).Error; err != nil {
			return fmt.Errorf("account erasure: list clients named by %s: %w", table, err)
		}
		for _, id := range ids {
			clients[id] = true
		}
		return tx.Table(table).Where(where, args...).Delete(map[string]any{}).Error
	}
	for _, table := range []string{"oauth_authorization_codes", "oauth_refresh_tokens"} {
		if err := collect(table, "account_id = ?", facts.accountID); err != nil {
			return fmt.Errorf("account erasure: delete %s of the account: %w", table, err)
		}
		if err := forEachChunk(facts.deletedAgents, func(chunk []string) error {
			return collect(table, "agent_id IN ?", chunk)
		}); err != nil {
			return fmt.Errorf("account erasure: delete %s of deleted agents: %w", table, err)
		}
	}
	for clientID := range clients {
		var remaining int64
		err := tx.Raw("SELECT (SELECT COUNT(*) FROM oauth_authorization_codes WHERE client_id = ?) + "+
			"(SELECT COUNT(*) FROM oauth_refresh_tokens WHERE client_id = ?)", clientID, clientID).Scan(&remaining).Error
		if err != nil {
			return fmt.Errorf("account erasure: count what still names client %q: %w", clientID, err)
		}
		if remaining > 0 {
			continue
		}
		if err := tx.Table("oauth_clients").Where("client_id = ?", clientID).Delete(map[string]any{}).Error; err != nil {
			return fmt.Errorf("account erasure: delete client %q: %w", clientID, err)
		}
	}
	return nil
}

// purgeAuthorization removes the casbin grouping policies whose domain is the
// account, and reports them: the running process's enforcer keeps its own
// copy of every grouping it has loaded, and only the caller can reach that
// (wm-wrnzb). The table is the casbin gorm adapter's; it is read by name so
// this package takes no dependency on the adapter, and skipped when an
// instance never created it.
func purgeAuthorization(tx *gorm.DB, facts *accountFacts, report *repositories.PurgeReport) error {
	if !tx.Migrator().HasTable("casbin_rule") {
		return nil
	}
	var rows []struct {
		V0 string
		V1 string
	}
	if err := tx.Table("casbin_rule").Select("v0, v1").Where("ptype = 'g' AND v2 = ?", facts.accountID).
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("account erasure: list grouping policies: %w", err)
	}
	for _, row := range rows {
		report.Groupings = append(report.Groupings, repositories.AccountGrouping{AgentID: row.V0, RoleID: row.V1})
	}
	if err := tx.Exec("DELETE FROM casbin_rule WHERE ptype = 'g' AND v2 = ?", facts.accountID).Error; err != nil {
		return fmt.Errorf("account erasure: delete grouping policies: %w", err)
	}
	return nil
}

// purgeIdentity removes the invitations gathered above, the account's
// memberships and the sessions of the members who go with it, then those
// members' credentials and agent rows. A member who belongs to another account keeps their agent, their
// credentials and their sessions: a session of theirs still scoped to this
// account is refused from now on with the code that says the access was
// taken away, which is what tells their app to sign them in again.
func purgeIdentity(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	if err := forEachChunk(facts.invites, func(chunk []string) error {
		return tx.Table("invites").Where("id IN ?", chunk).Delete(map[string]any{}).Error
	}); err != nil {
		return fmt.Errorf("account erasure: delete invites: %w", err)
	}
	if err := forEachChunk(facts.deletedAgents, func(chunk []string) error {
		return tx.Table("auth_sessions").Where("agent_id IN ?", chunk).Delete(map[string]any{}).Error
	}); err != nil {
		return fmt.Errorf("account erasure: delete sessions of deleted agents: %w", err)
	}
	if err := tx.Table("account_members").Where("account_id = ?", facts.accountID).Delete(map[string]any{}).Error; err != nil {
		return fmt.Errorf("account erasure: delete memberships: %w", err)
	}
	if err := forEachChunk(facts.credentials, func(chunk []string) error {
		return tx.Table("password_credentials").Where("credential_id IN ?", chunk).Delete(map[string]any{}).Error
	}); err != nil {
		return fmt.Errorf("account erasure: delete password credentials: %w", err)
	}
	if err := forEachChunk(facts.deletedAgents, func(chunk []string) error {
		if err := tx.Table("credentials").Where("agent_id IN ?", chunk).Delete(map[string]any{}).Error; err != nil {
			return err
		}
		return tx.Table("agents").Where("id IN ?", chunk).Delete(map[string]any{}).Error
	}); err != nil {
		return fmt.Errorf("account erasure: delete agents: %w", err)
	}
	return nil
}

func purgeAccountRow(tx *gorm.DB, facts *accountFacts, _ *repositories.PurgeReport) error {
	if err := tx.Where("account_id = ?", facts.accountID).Delete(&models.AccountErasure{}).Error; err != nil {
		return fmt.Errorf("account erasure: delete the erasure lock: %w", err)
	}
	if err := tx.Table("accounts").Where("id = ?", facts.accountID).Delete(map[string]any{}).Error; err != nil {
		return fmt.Errorf("account erasure: delete the account row: %w", err)
	}
	return nil
}

func (p *AccountPurger) Remains(ctx context.Context, accountID string) (bool, error) {
	if accountID == "" {
		return false, nil
	}
	return remains(p.db.WithContext(ctx), accountID)
}

// remains reports whether any row still names the account: the account row
// itself, an event by payload or aggregate, a resource, a membership, an
// invite. It is the predicate the purge and the erasure share, so what one
// calls left over the other sweeps.
func remains(db *gorm.DB, accountID string) (bool, error) {
	checks := []struct {
		table string
		where string
	}{
		{"accounts", "id = ?"},
		{"resources", "account_id = ?"},
		{"account_members", "account_id = ?"},
		{"invites", "account_id = ?"},
	}
	for _, check := range checks {
		var n int64
		if err := db.Table(check.table).Where(check.where, accountID).Count(&n).Error; err != nil {
			return false, fmt.Errorf("account erasure: count %s naming %q: %w", check.table, accountID, err)
		}
		if n > 0 {
			return true, nil
		}
	}
	var n int64
	err := db.Table("events").Where("aggregate_id = ? OR "+payloadAccountClause(db), accountID, accountID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("account erasure: count events naming %q: %w", accountID, err)
	}
	return n > 0, nil
}

func resourceURNsOf(db *gorm.DB, accountID string) ([]string, error) {
	var urns []string
	// Unscoped: a soft-deleted resource is still the account's, and its
	// triples and references are still keyed by its URN.
	if err := db.Unscoped().Model(&models.Resource{}).Where("account_id = ?", accountID).
		Pluck("id", &urns).Error; err != nil {
		return nil, fmt.Errorf("account erasure: list resources of %q: %w", accountID, err)
	}
	return urns, nil
}

// payloadAccountClause is the WHERE clause that reads AccountID out of an
// event payload, per dialect: PostgreSQL stores the payload as jsonb and
// SQLite as JSON text.
func payloadAccountClause(db *gorm.DB) string {
	if db.Name() == "postgres" {
		return "payload ->> 'AccountID' = ?"
	}
	return "json_extract(payload, '$.AccountID') = ?"
}

// pluckByChunk reads column from table for every row whose keyColumn is in
// keys, keys bound purgeChunk at a time.
func pluckByChunk(tx *gorm.DB, table, column, keyColumn string, keys []string) ([]string, error) {
	var out []string
	err := forEachChunk(keys, func(chunk []string) error {
		var found []string
		if err := tx.Table(table).Where(keyColumn+" IN ?", chunk).Pluck(column, &found).Error; err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	return out, err
}

// forEachChunk calls fn with successive slices of at most purgeChunk ids, and
// not at all for an empty list.
func forEachChunk(ids []string, fn func(chunk []string) error) error {
	for start := 0; start < len(ids); start += purgeChunk {
		end := min(start+purgeChunk, len(ids))
		if err := fn(ids[start:end]); err != nil {
			return err
		}
	}
	return nil
}
