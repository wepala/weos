---
title: "ADR: Food Types, Account Upload Folders and Account Erasure"
parent: Architecture Decision Records
layout: default
nav_order: 10
---

# ADR: What Core Carries for WeHungry — the Food Types, a Folder per Account, and Erasure of an Account

**Status:** Proposed
**Date:** 2026-09-10
**Tracking:** beads `wm-kb6sg` (stories `wm-kb6sg.1`, `wm-kb6sg.2`, `wm-kb6sg.3`); epic issue [wepala/wehungry#1](https://github.com/wepala/wehungry/issues/1)

## Context and Problem Statement

A WeHungry account has to hold its own food data on a shared WeOS instance. Three parts of core stand in the way. This section describes the code as it is on `v3` at `ebbd918`, and mini-me-weos at `fbf90ba6` (the tip of its `main`).

### 1. The food vocabulary is split across two repositories

- Core's `meal-planning` preset (`application/presets/mealplanning/preset.go`) registers 14 types — recipe, how-to-step, ingredient, recipe-ingredient, nutrition-information, cookbook, meal-plan, scheduled-meal, meal-occurrence, pantry, food-item, shopping-list, shopping-list-item, restricted-diet — with `AutoInstall` false.
- mini-me registers a separate `food` preset in `cmd/mini-me/food_preset.go` (`registerFood`) with ten more: taste-profile, meal-log, restaurant, planned-meal, grocery-amendment, grocery-list-item, staple, purchase, purchase-line, item-kind. Story wepala/mini-me-weos#408 already points them at core's vocabulary. Each `@context` is built by string concatenation from `schemaNS = "https://schema.org/"`, `mealPlanningNS = jsonld.MealPlanningVocab`, and one literal, `weosIngestNS = "https://weos.io/vocab/ingest#"` (`cmd/mini-me/preset.go:22`), which has no counterpart constant in core's `pkg/jsonld/vocab.go`.
- The same `food` preset also owns nine HTTP handlers (receipt import, `/food/*`, a debug seed), a `recipe` behavior, and four in-place schema patches to core types (`addRecipeEstimatedCost`, `addRecipeIngredientFields`, `addFoodItemLow`, `addPantryTopUpSeed`).
- Three of the ten are twins of a core type: they share the class but keep their own slug and table. meal-log / meal-occurrence share `mp:MealOccurrence`, planned-meal / scheduled-meal share `schema:Schedule`, grocery-list-item / shopping-list-item share `mp:ShoppingListItem`.
- Three of the ten name types core does not ship. restaurant declares `"rdfs:subClassOf":"agent"` (finance), purchase declares `"rdfs:subClassOf":"agreement"` (commerce), and meal-log's `orderId` carries `x-resource-type: order` (commerce). finance and commerce come from `github.com/wepala/weos-private-presets` (mini-me `cmd/mini-me/main.go:19-30`).
- A stored resource type is keyed by slug alone (`infrastructure/models/resource_type.go`, `Slug` has a unique index). Nothing records which preset installed it. Boot reconcile (`ReconcilePresetSchemas`, `application/resource_type_service.go`) merges additive changes per preset name, and emits no event where code and store agree.
- mini-me's namespace audit (`cmd/mini-me/preset_food_namespace_audit_test.go`) finds the types by calling `registerFood`, and by reading `food_preset.go` as source text.
- WeHungry cannot reuse any of it yet: its `go.mod` (`module myapp`) requires pericarp but not `wepala/weos`.

### 2. Uploads have no owner

- `services.UploadParams` (`domain/services/file_service.go:26`) carries `Filename`, `ContentType` and `ID`. No account.
- `UploadHandler.Upload` (`api/handlers/upload_handler.go:100`) builds the params from the multipart form alone.
- GCS (`infrastructure/storage/gcs/gcs.go:58`) and S3 (`infrastructure/storage/s3/s3.go:72`) write the key `uploads/<id>-<name>`. Local (`infrastructure/storage/local/local.go:64-65`) writes `<Storage.LocalPath>/<id>-<name>` and returns the URL `/api/uploads/files/<id>-<name>`.
- The provider (`infrastructure/storage/provider/provider.go`) wraps a cloud primary with local as a secondary; the composite returns the secondary's app-hosted URL.
- `GET /api/uploads/files/*` (`internal/cli/serve.go:548`) is in the `protected` group, so it needs a sign-in. It is a plain `http.FileServer` over the local path, and it does not compare the file with the caller's account. Any signed-in person who has a URL reads the file.
- `FileService` has `Upload` only. No backend can list or delete what it wrote.
- mini-me's snapshot (`cmd/mini-me/snapshot_handler.go:303-310`) promises that a packed document keeps the stored name its `/api/uploads/files/<stored name>` reference resolves to.

### 3. Nothing can remove an account

No route, command or service deletes an account. What exists is a lock. pericarp's `ValidateSession` refuses a session whose account is inactive (`ErrSessionAccountDeactivated`) or whose membership is gone (`ErrSessionAccountRevoked`), and sign-in refuses an inactive account; `tests/e2e/features/account_scoped_sessions.feature` pins both. That is a soft flag. The data stays.

An account's data lives in these places today:

| Store | Where | How it is tied to the account |
|---|---|---|
| Event log | pericarp `events` (`aggregate_id`, `transaction_id`, `position`, `payload`) | No account column. Every resource event carries `AccountID` in its payload (`domain/entities/resource_events.go`). pericarp's auth service also takes the event store, so auth aggregates can write events under their own ids. |
| Resource rows | `resources`, every projection table and its ancestor tables | `account_id` column (`infrastructure/database/gorm/projection_manager.go:816`). `ResourceRepository.Delete` removes `resources` rows outright (`resource_repository.go:1569`). |
| SQL triples | `triples` | The subject is the resource URN. No account column. |
| References, permissions | `event_references` (event id, resource URN), `resource_permissions` (resource id, agent id) | By URN or agent. |
| Knowledge graph | Per-account: `<OXIGRAPH_ACCOUNT_STORE_PATH>/<accountID>`, marked by `.weos-account-graph` (`infrastructure/graph/stores.go`). Single-tenant: one store shared by every account. | Directory, or subject URN. |
| Background projections | Checkpointed subscriber groups such as `oxigraph` and `display-values` (`application/peripheral_groups.go`); parked events | They read the log by position after the write commits. |
| Settings and grants | `behavior_settings`, `feature_grants` (`account_id`); `feature_settings` (`scope_id`) | Column. |
| Identity | pericarp `accounts`, `account_members`, `agents`, `credentials`, `password_credentials`, `auth_sessions` (`account_id`), `invites`; casbin grouping policies `(agent, role, account)` | Column or policy domain. |
| Connector access | `oauth_authorization_codes`, `oauth_refresh_tokens` (`agent_id`, `account_id`) | Column. |
| Connector registration | `oauth_clients` (`internal/oauth/models.go:21`) | **None.** A client registers (RFC 7591) before anyone signs in, so the row names no agent and no account. |
| Uploads | The bucket and the local path | Nothing today (part 2). |
| Browser | Gorilla cookie session and JWT cookie (`PasswordAuthHandler.Logout` clears both) | Cookie. |

pericarp's auth repositories expose `Save` and finders; only `PasswordCredentialRepository` has `Delete`. `BearerOrSession` (`api/middleware/bearer_or_session.go:50-70`) builds the identity from the token's claims and does no account lookup of its own.

Apple App Store guideline 5.1.1(v) and Google Play's User Data policy require an app that creates accounts to let the person delete the account and its data from inside the app. WeHungry cannot be listed until core can do that.

**The question:** where do these three capabilities live in core, and what shape do they take, so that the food vocabulary has one home, no account can read another account's files, and a deletion leaves nothing behind?

## Decision Drivers

Quality-attribute scenarios, each as source → stimulus → artifact → environment → response → measure:

1. **Isolation.** A signed-in person in account B → requests the URL of a file account A uploaded → the upload read route → normal operation, any backend configuration → the route answers 404, the same answer as for a file that does not exist → 0 bytes of A's file served.
2. **Erasure completeness.** A person → confirms deletion of their account → every store in the table above → normal operation, per-account or single-tenant graph → everything tied to the account is removed and the response signs them out → a query of each store by the account id, its resource URNs and its deleted agents returns 0 rows; a fresh sign-in with the same credential lands in a new, empty account.
3. **Erasure under failure.** The process → crashes part-way through a deletion → the deletion → any step → the account cannot be used, and running the deletion again finishes it → 0 data left after the re-run; 0 requests served in the account between the crash and the re-run.
4. **Vocabulary identity.** mini-me → upgrades to the core tag that carries the types → the stored taste-profile … item-kind types on the live twin → first boot → reconcile finds no delta → 0 `ResourceTypeUpdated` events, 0 re-projections, and mini-me's namespace audit passes against the moved definitions.
5. **No regression for existing files.** An existing twin → reads a document uploaded before this change → the read route → after the upgrade → the file is still served at its old URL → 0 broken references.

Constraints from the constitution: Article I (events are immutable; every change goes through a unit of work), Article II (the domain declares the ports), Article IV (every moved property states a defined term), Article VIII (the response envelope), Article XIII (core requires no private module).

Constraints from the epic: the account comes from the session, never from a header; deletion is a hard delete, never a flag; every moved `@context` keeps its string form; the three twin pairs are not merged.

## Considered Options

### Food types

- **A1. Move the ten into the `meal-planning` preset with byte-identical contexts.**
  Good: one preset and one vocabulary; a core-only binary lists 24 types; stored types are keyed by slug, so moving the definition changes no data.
  Bad: core's preset then names three slugs that only the private presets ship; the published-vocabulary sweep meets schema.org names no core type used before.
- **A2. Move them into a new core `food` preset.**
  Good: meal-planning's 14 types stay untouched.
  Bad: two presets for one domain — the split this epic exists to close. The definition of done asks for 24 meal-planning types.
- **A3. Move them, and replace the private-slug references with link definitions ([Cross-Preset Link Definitions]({% link decisions/cross-preset-link-definitions.md %})).**
  Good: follows that record; no core schema names a private slug.
  Bad: `rdfs:subClassOf` sits inside the `@context`, so removing it breaks byte identity and changes the class hierarchy in the graph. A link can express a reference, not a subclass edge. It turns a vocabulary move into a re-projection.

### Upload ownership

- **B1. The account id in `UploadParams`, one key builder shared by every backend, ownership checked on read from the path.**
  Good: small; the key is the ownership record, so deleting an account's files is a prefix delete.
  Bad: the URL shape changes for new uploads.
- **B2. An upload metadata table (id → account), checked on read.**
  Good: URLs stay as they are.
  Bad: a new table and a second write on every upload, and the bucket still has no folder to delete. The epic asks for the folder.
- **B3. Signed, expiring URLs served straight from the bucket.**
  Good: no bytes served by the app.
  Bad: nothing in core signs URLs, local storage has no equivalent, and a signed URL is readable by whoever holds it. It answers a different question.

### Deletion

- **C1. Deactivate the account.** Rejected outright: it is the soft flag the epic forbids and both store policies reject.
- **C2. Crypto-shredding — encrypt each account's event payloads with its own key, then destroy the key.**
  Good: the log stays append-only.
  Bad: no encryption layer exists; every existing event would be rewritten anyway; projection rows, triples, the graph and the bucket hold plaintext and still need a purge. An innovation token spent for no gain.
- **C3. An `Account.Deleted` event, which every projector answers by purging its own state.**
  Good: with the grain for projections.
  Bad: background groups lag, so "signed out and gone" is only eventually true; the event rows still need a direct delete; the deletion event itself names the account it erased.
- **C4. One synchronous erasure service — lock the account, drain background projections, delete the external stores, then delete every SQL row in one transaction.**
  Good: complete, ordered and re-runnable; reuses the deactivation lock pericarp already enforces.
  Bad: deletes events directly, against Article I, through a delete path pericarp does not offer.

## Decision Outcome

Chosen: **A1, B1 and C4.** A1 and B1 are the with-the-grain options. C4 is the only deletion option that meets drivers 2 and 3 without building an encryption layer.

### Story `wm-kb6sg.1` — the ten food types live in `meal-planning`

- Add `IngestVocab = HouseVocabBase + "ingest#"` beside `MealPlanningVocab` in `pkg/jsonld/vocab.go`.
- Move the ten constructors into `application/presets/mealplanning/preset.go`. They build their contexts from `"https://schema.org/"`, `jsonld.MealPlanningVocab` and `jsonld.IngestVocab`, so each resolved `@context` string is byte-for-byte the one mini-me stores today. Slugs, names, descriptions and schemas move unchanged. `Register` lists 24 types.
- A unit test pins the ten resolved contexts and schemas against a golden copy taken from mini-me at `fbf90ba6`. Core cannot import mini-me (a `main` package that requires private presets), so the golden file is the byte check.
- Behaviors, handlers and the four schema patches stay in mini-me. `AutoInstall` stays `false` in core; mini-me keeps turning it on.
- The twins stay separate slugs. The derived-state types `grocery-list-item`, `grocery-amendment` and `purchase-line` join `Sidebar.HiddenSlugs`, following the preset's existing practice of hiding child and derived types.
- The three private-slug references stay as strings. That is a deliberate deviation from Cross-Preset Link Definitions, justified by byte identity. It costs nothing in a core-only binary: `ensureProjection` returns early when a parent type is not installed (`application/event_handlers.go:152-160`), and the edge activates when an overlay installs finance and commerce. No `go.mod` changes, so Article XIII holds.
- Add `isBasedOn`, `seller`, `servesCuisine` and `telephone` — all published schema.org properties — to `policedVocabularies["https://schema.org/"]` (`application/presets/published_vocabulary.go:134`), so the #535 sweep passes over the moved types. House namespaces are not policed (`published_vocabulary.go:128-130`), so the minted `mp:` and `ingest#` terms need no waiver.
- mini-me, in its bump: deletes the ten constructors (the `food` preset stays, for its handlers and behavior), re-points its audit rows at preset `meal-planning`, and moves its twin-namespace source grep onto its own files. It never registers its copies beside core's.

### Story `wm-kb6sg.2` — every upload lands in its account's folder

- `services.UploadParams` gains `AccountID string`.
- `UploadHandler.Upload` sets it from `auth.AgentFromCtx(ctx).ActiveAccountID` — the identity the session or bearer middleware already resolved — and refuses the upload when it is empty. No header, form field or query parameter is read for it.
- `infrastructure/storage/helpers.go` gains `ObjectKey(accountID, id, safeName)`, which returns `accounts/<accountID>/uploads/<id>-<safeName>`, and `ValidateAccountID`, which applies the `[A-Za-z0-9_-]` rule `infrastructure/graph/stores.go` already uses. GCS and S3 use the key as it is. Local writes `<Storage.LocalPath>/accounts/<accountID>/uploads/<id>-<name>` and returns `/api/uploads/files/accounts/<accountID>/uploads/<id>-<name>`. The composite passes the account through unchanged.
- The read route stops being a bare `http.FileServer`. For a path under `accounts/`, it serves the file only when the second segment equals the caller's active account, and answers 404 otherwise — the same answer as for a missing file, so a probe learns nothing. The existing security headers stay.
- Files uploaded before this change keep serving at their flat URLs (driver 5). They cannot be attributed to an account, so they fall outside the isolation guarantee and outside erasure. Moving them and rewriting their references is follow-up work, not this story.

### Story `wm-kb6sg.3` — a person can delete their account

**Routes** (protected group, envelope per Article VIII):

- `DELETE /api/account` with body `{"confirmation":"DELETE"}`; anything else is 400. The caller must hold the admin role in the active account, checked the way `FeatureAdminService.requireAccountAdmin` checks it (`application/feature_admin_service.go:294`). A request made while an impersonation session is active is refused, so an administrator cannot erase someone else's account through this route. On success the route answers 200 and clears the session cookie and the JWT cookie the way `PasswordAuthHandler.Logout` does. When the active account no longer exists, the handler answers 404.
- `GET /api/account/export` returns the active account's `recipe` resources as one JSON-LD document (`@context` and `@graph`), written the way `api/handlers/resource_handler.go:321-340` already writes `application/ld+json`. It filters on `account_id` explicitly. Resource listing is gated per resource, not per account (wepala/weos#474), so reusing the list gate would export another account's recipes to a person who belongs to both.
- `weos account delete <account-id> --confirm` calls the same service, so an operator can finish a deletion the person can no longer reach (driver 3).

**Service.** `application.AccountErasureService` owns the sequence. The domain declares the ports (Article II) and infrastructure implements them:

- `repositories.AccountDataPurger` — GORM; enumerates and deletes the SQL state.
- `repositories.KnowledgeGraphStores.DropAccount(ctx, accountID, subjects)` — per-account: close the open store and remove its directory, only when the marker file is present; single-tenant: remove each subject.
- `services.FileService.DeleteAccountFolder(ctx, accountID)` — GCS: list objects by prefix and delete each; S3: `ListObjectsV2` and `DeleteObjects` in batches of 1000; local: `RemoveAll` of the account folder; composite: every backend, errors joined, never best-effort.

**Sequence.** Every step is idempotent, and the account row goes last, so a crash leaves a locked account that a re-run finishes:

1. **Lock.** Set the account inactive through pericarp's existing account save. `ValidateSession` refuses every session in it from the next request on.
2. **Drain.** Read the event store's `HeadPosition`, then wait — bounded; a timeout fails the request and keeps the lock — until every subscriber group's checkpoint reaches it, so no background group projects the account's events after the purge.
3. **Enumerate.** Resource URNs from `resources.account_id`. Event ids whose payload `AccountID` is the account (JSON extraction per dialect: `->>` on PostgreSQL, `json_extract` on SQLite), together with every event that shares their `transaction_id`. The account's members, and which of them have no other membership. Those agents' credentials, sessions and invites, and the ids of their auth aggregates.
4. **External stores.** Delete the bucket folder, then drop the graph. Neither is transactional, so both come before the SQL commit: if one fails, the SQL state that drives enumeration is still there for the re-run.
5. **SQL, one transaction, in chunks** (SQLite caps bound parameters):
   events and their parked copies; `event_references`; `resource_permissions`; every projection and ancestor table row and `resources` row for the account; `triples` by subject; `behavior_settings`, `feature_grants` and `feature_settings` for the account; `oauth_authorization_codes` and `oauth_refresh_tokens` for the account or its deleted agents; an `oauth_clients` row only when no code or token from any account still names it; casbin grouping policies whose domain is the account; `invites`, `auth_sessions`, `account_members`; `password_credentials`, `credentials` and `agents` for agents left with no account; and last, the `accounts` row.
   With the credential gone, the next sign-in takes `FindOrCreateAgent`'s create path and gets a new, empty account.
6. **Sign out** the caller.

**The Article I exception.** Deleting event rows breaks Article I, and this record is where that is stated. Erasure is a legal requirement that outranks the immutability rule. It is confined to `AccountDataPurger` and is never reachable from a unit of work. Story `wm-kb6sg.3`'s pull request names the article in its body, as Governance requires. Because this is a permanent, repeatable case rather than a one-off, the same pull request amends Article I to name account erasure as its one exception (a MINOR bump, to 1.2.0).

## Consequences

### Good

- One vocabulary, one preset: a core-only binary lists 24 `meal-planning` types, and mini-me and WeHungry read the same definitions.
- An upload's key is its ownership record. Isolation is a string comparison on read, and erasing an account's files is a prefix delete.
- Erasure reuses what exists: pericarp's deactivation lock for the window while it runs, the event store's head position for the drain, and the marker file that already stops a graph `Truncate` from deleting a directory it did not create.
- A crash cannot leave a usable, half-deleted account, and the operator command finishes the job.

### Neutral

- Stored types are keyed by slug, so the move emits no event on an existing twin — as long as the bytes match. The golden test is what keeps that true.
- The three twin pairs now sit in one preset. That makes the unmerged state visible; merging them stays P1.
- New upload URLs contain `accounts/<id>/uploads/`, which shows the caller their own account id. It is not a secret.
- `oauth_clients` rows cannot be deleted per account, because nothing ties a client to one. What is deleted per account is every authorization code and refresh token, which is what grants access. The epic's "OAuth clients" is met as "the account's connector access".

### Costs and Risks

- **Article I amendment.** A constitution change rides with story 3. Until it merges, the story is a named violation.
- **Private slugs in a public preset.** `agent`, `agreement` and `order` appear in core but are defined only in `weos-private-presets`. A core-only install gets restaurant, purchase and meal-log without the parent edge or the order reference's target. That is the documented behavior, and readers will ask why.
- **Four allow-list additions** to the schema.org sweep. Each is a real schema.org property; review should check that rather than wave it through.
- **The mini-me bump is two changes in lockstep.** Registering core's and mini-me's copies side by side would put one slug in two presets. The bump deletes mini-me's copies and edits its audit in the same commit.
- **Legacy flat uploads stay readable by any signed-in person**, and survive erasure. A fresh WeHungry instance has none; the live mini-me twin has some. A follow-up bead should move them and rewrite the references.
- **mini-me's snapshot layout.** The archive promises the stored name, and the stored name now contains `accounts/<id>/uploads/`. Pack walks directories recursively, so the file is packed, but restore has to put it back at the same relative path. Verify this in the mini-me bump.
- **Idempotency meets sign-out.** After the first call the session is gone, so a replay with the old cookie is refused by the auth middleware (401) before the handler can answer 404. The 404 is observable only by a caller whose credential still authenticates — a concurrent request, or a bearer token. The acceptance contract must say which one it asserts.
- **Bearer tokens outlive the deletion.** `BearerOrSession` does no account lookup, so an access token issued before the deletion authenticates until it expires, and a write through it would recreate rows under the deleted account id — and a per-account graph directory, because `ForAccount` opens stores lazily. Story 3 adds an account-active check to the bearer path: the one lookup the session path already makes.
- **The drain can stall.** A subscriber group that parks or keeps failing never reaches head. The bounded wait turns that into a failed deletion with the lock held — visible, and re-runnable — rather than a purge that a late projection undoes.
- **Enumeration by payload depends on every resource event carrying `AccountID`.** Today it does (`resource_events.go:45-48` says so and why). A future event type that omits it would leave rows behind. The erasure acceptance test queries every table by account, not through the enumeration, so an omission fails the test.
- **Large accounts.** One transaction over a large account runs long on SQLite. The deletes are chunked; the lock makes a long transaction safe, not fast.

## Further Reading

- [Cross-Preset Link Definitions]({% link decisions/cross-preset-link-definitions.md %}) — the rule A1 deviates from, and why A3 was not taken.
- `tests/e2e/features/account_scoped_sessions.feature` — the deactivation and revocation refusals the lock relies on.
- `tests/e2e/features/per_account_knowledge_graph.feature` — the per-account graph layout the drop relies on.
