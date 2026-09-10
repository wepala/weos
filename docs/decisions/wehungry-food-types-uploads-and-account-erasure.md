---
title: "ADR: Food Types, Account Upload Folders and Account Erasure"
parent: Architecture Decision Records
layout: default
nav_order: 10
---

# ADR: What Core Carries for WeHungry — the Food Types, a Folder per Account, and Erasure of an Account

**Status:** Accepted (plan gate `wm-mol-5061`, 2026-09-10) — built on three stacked branches, not yet merged
**Revised:** 2026-09-10 — brought up to date with the approver's answers to four findings (`wm-4nc8w` revised the design; `wm-8m547`, `wm-1lbdz` and `wm-tqi1q` let it stand) and with what the stories built: story 1 at `5cd7ec96`, story 2 at `0888ac28`, story 3 at `f94969e3`. Two answers are not yet in the code at those commits: the person-neutral descriptions and the golden comparison without descriptions (`wm-4nc8w`), and the deletion of surviving members' session rows (`wm-1lbdz`; scenario 17 already expects it). This is a revision of the same decision, not a superseding record.
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

The three story sections describe what was built — story 1 at `5cd7ec96`, story 2 at `0888ac28`, story 3 at `f94969e3` — and name the two approved answers the code does not carry yet.

### Story `wm-kb6sg.1` — the ten food types live in `meal-planning`

- Add `IngestVocab = HouseVocabBase + "ingest#"` beside `MealPlanningVocab` in `pkg/jsonld/vocab.go`. It is the import pipeline's namespace and names no preset.
- Move the ten constructors into `application/presets/mealplanning/preset.go`. They build their contexts from `"https://schema.org/"`, `jsonld.MealPlanningVocab` and `jsonld.IngestVocab`, so each resolved `@context` string is byte-for-byte the one mini-me stores today. Slugs, names, contexts and schemas move unchanged. `Register` lists 24 types.
- **Descriptions do not move unchanged** (finding `wm-4nc8w`, revised by the approver). mini-me's descriptions of meal-log, restaurant and staple name Akeem ("One meal Akeem ate …"), and core ships them to every install, WeHungry included, wherever a type's description shows (the admin UI, MCP `resource_type_list`). Core ships person-neutral descriptions for those three. This is safe for the live twin: `ReconcilePresetSchemas` never compares or writes a type's `Name` or `Description`, and `InstallPreset(update=false)` skips a type that already exists, so only a fresh install gets the new wording, and the live twin records no event. At `5cd7ec96` the three descriptions still name Akeem; the change is owed to story 1.
- A unit test (`food_types_test.go`) pins the ten resolved contexts and schemas against a golden copy taken from mini-me at `fbf90ba6` (`testdata/mini_me_food_types.golden.json`, which records its source repo, commit, path and blob). The comparison is byte-exact, not JSON-equal: a context with its keys in another order fails. Core cannot import mini-me (a `main` package that requires private presets), so the golden file is the byte check. The golden comparison covers context and schema, not description. At `5cd7ec96` it still compares description too; that change goes with the new descriptions.
- Behaviors, handlers and the four schema patches stay in mini-me. `AutoInstall` stays `false` in core; mini-me keeps turning it on.
- The twins stay separate slugs. The preset's own description tells a new product which half of each pair to write: scheduled-meal, meal-occurrence and shopping-list-item — the halves that carry the preset's behaviors — and names planned-meal, meal-log and grocery-list-item as mini-me's unmerged twins of them (`wm-e8tgx`, commit `2d7ee9ba`). mini-me keeps writing its own names until the P1 merge.
- The preset's sidebar config hides the derived-state types `grocery-list-item`, `grocery-amendment` and `purchase-line` (`Sidebar.HiddenSlugs`, following the preset's practice of hiding child and derived types), and `Sidebar.MenuGroups` puts both halves of each twin pair under one parent (meal-plan, shopping-list), so they sit side by side. Core never applies a preset's sidebar config on install (`wm-8ynzz`), so neither shows on any install today; see Costs and Risks.
- The three private-slug references stay as strings. That is a deliberate deviation from Cross-Preset Link Definitions, justified by byte identity. It costs nothing in a core-only binary: `ensureProjection` returns early when a parent type is not installed (`application/event_handlers.go:152-160`), and the edge activates when an overlay installs finance and commerce. A scenario in `food_types.feature` writes a meal-log whose `orderId` is set where no `order` type is installed, and reads it back through the API and the flat projection (`0f1151db`). No `go.mod` changes, so Article XIII holds.
- Add `isBasedOn`, `seller`, `servesCuisine` and `telephone` — all published schema.org properties — to `policedVocabularies["https://schema.org/"]` (`application/presets/published_vocabulary.go`), so the #535 sweep passes over the moved types. House namespaces are not policed by that sweep, so the minted `mp:` and `ingest#` terms need no waiver there.
- **The #520 house-domain regression allows one named, reused vocabulary** (finding `wm-8m547`, the design stands). #520's outline "The house prefix of each minting preset resolves on weos.io" required every house IRI a preset resolves to sit under that preset's own namespace. `purchase.contentHash` resolves to `https://weos.io/vocab/ingest#contentHash`, so the meal-planning row failed. The approver kept `contentHash` on `ingest#`, so that no live twin changes. Commit `5cd7ec96` gave the outline a `reuses` column: meal-planning names `https://weos.io/vocab/ingest#`, and every other row names `none`. A reused vocabulary must sit under `https://weos.io/vocab/`, end in `#`, differ from the preset's own namespace, and be resolved by one of the preset's installed types, so the allowance cannot outlive the term that needs it. Any house IRI under a namespace the row does not name still fails.
- mini-me, in its bump (`wm-y3mim`): deletes the ten constructors (the `food` preset stays, for its handlers and behavior), re-points its audit rows at preset `meal-planning`, and moves its twin-namespace source grep onto its own files. It never registers its copies beside core's.

### Story `wm-kb6sg.2` — every upload lands in its account's folder

- `services.UploadParams` gains `AccountID string`.
- `UploadHandler.Upload` sets it from `auth.AgentFromCtx(ctx).ActiveAccountID` — the identity the session or bearer middleware already resolved. No header, form field or query parameter is read for it. A request with no identity answers 401; a signed-in caller with no active account answers 403 `no active account`, because a 401 would send the app back to a sign-in that cannot give it an account.
- `infrastructure/storage/helpers.go` gains `ObjectKey(accountID, id, safeName)`, which returns `accounts/<accountID>/uploads/<id>-<safeName>`, and `ValidateAccountID`, which applies the `[A-Za-z0-9_-]` rule `infrastructure/graph/stores.go` already uses. GCS and S3 use the key as it is. Local writes `<Storage.LocalPath>/accounts/<accountID>/uploads/<id>-<name>` and returns `/api/uploads/files/accounts/<accountID>/uploads/<id>-<name>`.
- The composite passes the account through unchanged. When it has secondaries and every one of them fails, the upload fails; it no longer hands out the primary's bucket URL, where no account check runs (`wm-5zb4p`). The object already written to the bucket stays there, recorded only by an error log line.
- The read route stops being a bare `http.FileServer`. `handlers.ServeUploadedFiles(routePrefix, localPath, logger)` decides on the decoded, cleaned path, so an encoded `../` cannot pass the check and then resolve elsewhere. It serves two shapes only: a flat `<name>` at the root, and `accounts/<accountID>/uploads/<name>` where `<accountID>` equals the caller's active account ("accounts" and "uploads" match exactly, not case-folded). An account file is opened through an `os.Root` at that account's uploads folder, so a symlink that leads out of the folder is refused. Every other request answers 404 with the body `{"error":"file not found"}` — the same answer as a missing file, so a probe learns nothing. The security headers stay, and the 200 adds `Cache-Control: private, no-store`, because one URL is 200 for its account and 404 for every other.
- The route serves the local replica only. The bucket copy is never read back, so `Storage.LocalPath` has to survive an instance roll until read-back lands (`wm-5fu7t`).
- Files uploaded before this change keep serving at their flat URLs (driver 5). They cannot be attributed to an account, so they fall outside the isolation guarantee and outside erasure. The approver accepted that at the plan gate (`wm-usr9u`); moving them and rewriting their references is `wm-snzgr`.

### Story `wm-kb6sg.3` — a person can delete their account

**Routes** (envelope per Article VIII):

- `DELETE /api/account` with the body `{"confirm":"DELETE"}` exactly, sent as `application/json` (media-type parameters allowed). The body is one JSON object with one field, named `confirm` in that spelling and case, whose value is the string `DELETE`, and nothing after it. Anything else — `confirmation`, `Confirm`, an extra field, a trailing space, a second document, another content type, no body — answers 400 and changes nothing (`wm-cqqxf`, `wm-q7knw`). The field is `confirm`, not `confirmation`: the story's wording won at the plan gate.
- The caller must hold the owner or admin role in the active account (`apimw.IsOwnerOrAdmin`, which reads pericarp's `FindMemberRole`); an ordinary member answers 403. Any owner or admin may delete a shared account (`wm-tqi1q`). A request that carries an active impersonation cookie answers 403, so an administrator cannot erase someone else's account through this route.
- The route has its own group behind `apimw.SessionAuthForErasure`, with no `Impersonation` middleware. It makes the checks pericarp's `RequireAuth` makes and answers the same codes, with one admission `RequireAuth` cannot make: a session scoped to an account whose erasure is unfinished is let through, so the person can run the deletion again. A session that names no account is admitted into a locked account the person owns or administers (`apimw.LockedAccountFor`), which is the session a Google or Apple sign-in makes (`wm-or9a5`). Nothing else admits a locked account's session.
- Answers: 200 `{"data":{"account_id","members_lost"}}`, where `members_lost` counts every member the account had, the caller included, and the response clears the session cookie and the `pericarp_token` JWT cookie the way `PasswordAuthHandler.Logout` does. 404 when the active account no longer exists, or when nothing of it remains to purge — the second of two deletions (`wm-4cysr`). 409 with code `account_erasure_in_progress` when this process is already erasing the account. 500 with code `account_erasure_unfinished` when a step failed; the account stays locked and the deletion can be run again. `ErrorEnvelope` gains an optional `code` field, written by `respondErrorCode`.
- Before the deletion, `GET /api/auth/me` validates the session its cookie names and reports an optional `member_count` for the active account, so the app can say who else loses it. A count that cannot be read is left out, not reported as zero.
- `GET /api/account/export` (protected group) returns the active account's `recipe` resources as one JSON-LD document at the top level, not in the envelope: `application/ld+json`, `Content-Disposition: attachment; filename="recipes.jsonld"`, `@type` `weos:AccountExport`, a `weos:exportScope` object that states the document holds recipes and nothing else (includes, excludes, a note), and an `@graph` of each recipe's nodes under the type's merged context (`wm-os4la`). It filters on `accountId` explicitly, and skips a row from any other account as a second check. Resource listing is gated per resource, not per account (wepala/weos#474), so reusing the list gate would export another account's recipes to a person who belongs to both.
- `weos account delete <account-id> --confirm` calls the same service, so an operator can finish a deletion the person can no longer reach (driver 3). Without `--confirm` it refuses before it opens the store. `--drain-timeout` overrides the drain bound; `--skip-drain` purges without the drain, for a checkpoint row nothing advances. The app never sets it.

**The lock outside the route.** A locked account serves nothing but the deletion, and every refusal it produces says why:

- `apimw.ErasureGuard` wraps `RequireAuth` on the protected group. It holds back only a 401 whose code is `account_deactivated`, asks the lock repository once, and rewrites the code to `account_erasure_pending` for a locked account. Every other response passes through untouched, so a healthy request pays nothing (`wm-tsugz`); a lock that cannot be read answers 503. Because it judges the refusal, a bearer header beside the cookie changes nothing there (`wm-6umqn`). The MCP group passes `DeferToBearer()`.
- A password sign-in that resolves no active account is scoped to a locked account the person owns or administers, answers `erasure_pending` with code `account_erasure_pending`, and issues no JWT (`wm-421nl`).
- `apimw.Impersonation` lands in the person's first active account, and refuses 401 with `account_erasure_pending` or `account_deactivated` when every account the person has is inactive (`wm-iiasy`).
- `BearerOrSession` looks up the token's account on every bearer request — the one lookup the session path already made. A gone account answers 401 `invalid_token` with no code; a suspended one adds `account_deactivated`; a locked one adds `account_erasure_pending`; a failed read answers 503. A token issued before the deletion therefore stops authenticating, and a write through it cannot recreate rows or a graph directory under the deleted account's id.

**Service.** `application.AccountErasureService` owns the sequence. The domain declares the ports (Article II) and infrastructure implements them:

- `repositories.AccountErasureLocks` (`Lock`, `IsLocked`) — GORM, table `account_erasures` (account id, requested by, started at). The row is what tells an account part-way through an erasure from one an operator suspended: both are inactive in pericarp's terms. `Lock` on an account already locked keeps the first request's record.
- `repositories.AccountDataPurger` (`Enumerate`, `Purge`, `Remains`) — GORM, `infrastructure/database/gorm/account_purger.go`; enumerates and deletes the SQL state. `Purge` answers `ErrNothingToPurge` for a gone account row that nothing names.
- `repositories.KnowledgeGraphStores.DropAccount(ctx, accountID, subjects)` — per-account: close the open store and remove its directory, only when the marker file is present. Single-tenant: for each resource URN, remove the blank nodes reachable through blank nodes only, up to eight levels deep and deepest first; then every triple with the URN as its object; then the URN's own triples (`wm-fo2f9`). A subject that is not a plain IRI is refused.
- `services.FileService.DeleteAccountFolder(ctx, accountID)` — GCS: list objects by prefix and delete them 16 at a time; S3: `ListObjectsV2` and `DeleteObjects` in batches of 1000, four batches at a time; local: `RemoveAll` of the account folder; composite: every backend, errors joined, never best-effort. A folder that does not exist is not an error.
- `AccountRoleRevoker` — optional; the casbin checker, so the erasure can revoke from the running enforcer what the purge deleted from its table.

**Sequence.** Every step is idempotent, and the account row goes last, so a crash leaves a locked account that a re-run finishes:

0. **Claim and detach.** An in-process set refuses a second run for the same account (`ErrErasureInProgress`). The run is detached from the request's context (`context.WithoutCancel`) and given its own deadline, `ACCOUNT_ERASURE_TIMEOUT_SECONDS` (default 900, 15 minutes), so a client that hangs up does not abort the bucket walk (`wm-mpj0l`).
1. **Lock.** Write the `account_erasures` row, then set the account inactive through pericarp's account save. The row goes first, so every refusal the inactive account produces can say the deletion is unfinished. `ValidateSession` refuses every session in it from the next request on.
2. **Drain.** Read the event store's `HeadPosition` once, after the lock, then poll the `subscriber_checkpoints` rows until every row reaches it — bounded by `ACCOUNT_ERASURE_DRAIN_TIMEOUT_SECONDS` (default 30); a timeout fails the request and keeps the lock. A row is set aside, with a warning, only when three things hold together: it is not a group this process runs (the worker manager's groups when `Worker.RunInProcess` is set, none otherwise); it has gone unwritten longer than `ACCOUNT_ERASURE_DRAIN_STALE_AFTER_SECONDS` (default 600); and it still does not move after a two-second grace. A group that is turned off, renamed or retired leaves its row where it stopped, and without this rule every deletion on the instance times out on it (`wm-gyfdi`). A row this process runs is waited for however old it is, and so is a fresh row from another process.
3. **Enumerate.** Resource URNs from `resources.account_id`, and the member count. Inside the purge transaction the purger gathers the aggregates whose events go: the account; its resources; every aggregate whose event payload `AccountID` names the account (`->>` on PostgreSQL, `json_extract` on SQLite); and the members with no other membership, with their credentials, password credentials and sessions. Events are deleted by aggregate id within that set only. An event of a surviving aggregate that shares a `transaction_id` with one of the account's events is left alone. Invitations that go are the account's own, and any from another account that names a deleted person by agent id or by email (`wm-i2oni`).
4. **External stores.** Delete the bucket folder, then drop the graph. Neither is transactional, so both come before the SQL commit: if one fails, the SQL state that drives enumeration is still there for the re-run.
5. **SQL, one transaction, in chunks** (SQLite caps bound parameters), in this order:
   events and their parked copies; `event_references`, `resource_permissions`, `triples` by subject, and the `resource_search` lexical index; the account's rows in every installed type's projection and ancestor tables; `resources`; `behavior_settings`, `feature_grants` and `feature_settings` for the account; `oauth_authorization_codes` and `oauth_refresh_tokens` for the account or its deleted agents, then an `oauth_clients` row only when no code or token from any account still names it; casbin grouping rows (`ptype = 'g'`) whose domain is the account, read first and reported; `invites`; `auth_sessions`; `account_members`; `password_credentials`, `credentials` and `agents` for agents left with no account; and last, the `account_erasures` row and the `accounts` row.
   `auth_sessions` loses every row scoped to the account, including the rows of members who keep another account, and every session of an agent that goes with the account (`wm-1lbdz`). At `f94969e3` the purger deletes only the sessions of agents that go; the rows of surviving members are owed to story 3.
   With the credential gone, the next sign-in takes `FindOrCreateAgent`'s create path and gets a new, empty account.
6. **Re-sweep.** The lock stops new requests, not requests already admitted: one admitted a moment before the lock can commit after the head was read, even after the purge's transaction, and leave a `resources` row, its events and, in per-account mode, a graph directory re-created under the deleted id (`wm-mnry2`). After the purge, `Remains` checks for the account row, events by payload or aggregate, resources, memberships and invites; anything found is swept again by steps 3–5, up to two more times, and past that bound the run fails with a message to run it again. A run named for an account whose row is gone but whose rows remain sweeps them with no lock to take.
7. **Revoke in memory.** `api/middleware/authorize_resource.go` calls `AssignAccountRole(agent, role, activeAccount)` on every dynamic-route request, so the casbin table holds grouping rows for every account that served one, and the running enforcer holds a copy of each. After the purge, the service calls `RevokeAccountRole` for every grouping the purge reported (`wm-wrnzb`, commit `80341e4a`). A failed revoke is logged, not a failed deletion: the ids are KSUIDs and are never reused.
8. **Sign out** the caller (the route does this; the service knows nothing of cookies).

**The Article I exception.** Deleting event rows breaks Article I, and this record is where that is stated. Erasure is a legal requirement that outranks the immutability rule. Story `wm-kb6sg.3`'s pull request names the article in its body, as Governance requires. Because this is a permanent, repeatable case rather than a one-off, the same pull request amends Article I (a MINOR bump, to 1.2.0). On story 3's branch the amended article:

- names account erasure as the one exception, confined to `repositories.AccountDataPurger`, implemented once in `infrastructure/database/gorm/account_purger.go`, and reached only through `application.AccountErasureService`, from `DELETE /api/account` and `weos account delete`. It is never reachable from a unit of work, an event handler or a projection, and never a way to correct history. A second delete path for events anywhere else is a violation.
- bounds the exception to the events of aggregates being deleted: the account, its resources, and the auth aggregates of members who belonged to nothing else. That is why step 3 deletes by aggregate id and not by shared transaction.
- states what the article governs: core's own entities and the event store they commit to. pericarp's authentication aggregates — accounts, agents, credentials, sessions, invites — are persisted by pericarp's own repositories and are outside its scope. The lock's two writes (the `account_erasures` row and the deactivation of the `accounts` row) go that way and are not a violation (`wm-eftav`). The `wm-1lbdz` answer deletes session rows, not events, so it does not widen the event sweep the article bounds.

## Consequences

### Good

- One vocabulary, one preset: a core-only binary lists 24 `meal-planning` types, and mini-me and WeHungry read the same definitions.
- An upload's key is its ownership record. Isolation is a string comparison on read, and erasing an account's files is a prefix delete.
- Erasure reuses what exists: pericarp's deactivation for the window while it runs, the event store's head position for the drain, and the marker file that already stops a graph `Truncate` from deleting a directory it did not create. The one addition, the `account_erasures` row, is what lets every refusal say "the deletion is unfinished" instead of "suspended".
- A crash cannot leave a usable, half-deleted account. The person can finish it from the app after a password or provider sign-in, and the operator command finishes it too.
- A bearer token issued before the deletion stops authenticating on its next request, so nothing can recreate the account's rows through it.

### Neutral

- Stored types are keyed by slug, so the move emits no event on an existing twin — as long as the context and schema bytes match. The golden test is what keeps that true. Descriptions are outside that promise, because reconcile never compares or writes them.
- The three twin pairs now sit in one preset. That makes the unmerged state visible; merging them stays P1. Until then the preset's description names the half a new product writes.
- New upload URLs contain `accounts/<id>/uploads/`, which shows the caller their own account id. It is not a secret.
- `oauth_clients` rows cannot be deleted per account, because nothing ties a client to one. What is deleted per account is every authorization code and refresh token, which is what grants access. The epic's "OAuth clients" is met as "the account's connector access".
- A second deletion with the cookie the first one cleared answers 401 with no code, because the auth middleware refuses a session with no row before the handler runs. The route's 404 for a gone account is observable only by a caller whose credential still authenticates, and a unit test of the handler pins it.

### Answers given after approval

The approver answered four findings after the plan gate. Each answer is in the sections above; this is the record of what was decided and why.

- **`wm-4nc8w` — revised.** "My name really shouldn't be in the description." Core ships person-neutral descriptions for meal-log, restaurant and staple, and the golden byte comparison covers context and schema, not description. Only fresh installs see the new wording; the live twin records no event.
- **`wm-8m547` — stands.** "Don't want to change any live twin for this right now." `purchase.contentHash` stays at `https://weos.io/vocab/ingest#contentHash`, byte identity with mini-me holds, and the #520 regression contract now allows a named, reused weos.io vocabulary (commit `5cd7ec96`).
- **`wm-1lbdz` — stands.** "Better to honor the delete request." Every one of the account's `auth_sessions` rows is deleted, the rows of surviving members included. A surviving member's session in the deleted account is therefore refused as any session that no longer exists is — 401, no code — not with pericarp's `account_access_revoked`, and their next sign-in lands in the account they keep. Scenario 17 of `account_deletion.feature` was amended to expect that (commit `f94969e3`).
- **`wm-tqi1q` — stands.** "Better to honor the delete request regardless." Any owner or admin may delete a shared account, and it takes the account from every member; a member who belonged to nothing else loses their login with it. The member count on `GET /api/auth/me` stays optional, and the deletion's answer reports `members_lost` after the fact. No required member count and no owner-only rule were added.

### Costs and Risks

- **Article I amendment.** A constitution change rides with story 3. Until it merges, the story is a named violation.
- **Private slugs in a public preset.** `agent`, `agreement` and `order` appear in core but are defined only in `weos-private-presets`. A core-only install gets restaurant, purchase and meal-log without the parent edge or the order reference's target. That is the documented behavior, and readers will ask why.
- **Four allow-list additions** to the schema.org sweep. Each is a real schema.org property; review should check that rather than wave it through.
- **The #520 allowance is a named exception to the house-domain rule.** A second preset that wants to reuse a weos.io vocabulary has to add it to its own row. That is deliberate: the reuse stays visible and cannot outlive the term that needs it.
- **meal-log's flat `status` returns the lifecycle status** (wepala/weos#539). `status` is a reserved projection column, so the flat REST row for a meal logged as `cooked` answers `active`, while the graph holds `mp:status` correctly. mini-me recorded this limitation; the note now sits on `mealLogType` in `preset.go` (`wm-9aksy`, commit `b027cb6d`), and every core install inherits the limitation with the type. It goes away when #539 is fixed.
- **Preset sidebar config has no visible effect** (`wm-8ynzz`). `PresetSidebarConfig` is declared per preset and unit-tested, but nothing applies it on install: the admin sidebar reads stored sidebar settings, which installing a preset never writes. The hidden derived types and the twin-pair grouping above show on no install until that bead applies preset sidebar defaults (without overwriting an operator's edits) or removes the dead config.
- **The mini-me bump is two changes in lockstep** (`wm-y3mim`). Registering core's and mini-me's copies side by side would put one slug in two presets. The bump deletes mini-me's copies and edits its audit in the same commit.
- **Legacy flat uploads stay readable by any signed-in person**, and survive erasure. A fresh WeHungry instance has none; the live mini-me twin has some. The approver accepted this at the plan gate; `wm-snzgr` moves them and rewrites the references. The flat branch of the read route still opens through `http.Dir`, so a symlink planted at the top level of the upload directory is followed. No product code writes symlinks.
- **Only the local replica is readable.** The read route never reads the bucket, so a VM whose local disk is replaced 404s every photo until `wm-5fu7t` streams the object from the primary when the local file is absent. Until then a failed local write fails the upload and leaves an orphaned bucket object, found only by its error log line; `wm-5fu7t` should delete or reuse it.
- **mini-me's snapshot layout.** The archive promises the stored name, and the stored name now contains `accounts/<id>/uploads/`. Pack walks directories recursively, so the file is packed, but restore has to put it back at the same relative path. Verify this in the mini-me bump.
- **The drain's staleness rule trades a wait for a guess.** A separate worker process that has been down for longer than `ACCOUNT_ERASURE_DRAIN_STALE_AFTER_SECONDS` is treated as frozen and not waited for, so its projection can later write the account's events into its own store. `--skip-drain` goes further and skips the wait entirely; it is for an operator who knows no projection is running. A group that parks or keeps failing still fails the deletion with the lock held — visible, and re-runnable.
- **A request in flight when the lock lands can commit after the purge.** The re-sweep closes that window for up to two late commits. A run that keeps finding rows fails and asks for a re-run, and a re-run for a gone account sweeps what remains.
- **The in-progress guard is per process.** Two API replicas behind a balancer can each start a run for the same account; both are idempotent, and the loser answers 404 once the winner's purge commits, but the two can contend on Postgres rows. A cross-process guard needs a lease on `account_erasures`. WeHungry deploys one VM today. The route still waits for the whole run (there is no `202 Accepted`), so a client with a short timeout can hang up; the run finishes anyway within its 15-minute deadline, which has to cover a bucket walk of every file the account ever stored.
- **A shared graph keeps some of a deleted account's nodes** (`wm-fo2f9`). A nested node that carries its own `@id` is an IRI and cannot be told from a link to another resource, so it stays; so does nesting deeper than eight levels. A deletion promise needs per-account graph stores (`OXIGRAPH_ACCOUNT_STORE_PATH`), where the whole directory goes. WeHungry runs per-account stores.
- **Casbin rows live in two places.** The purge deletes the account's grouping rows from the table and step 7 revokes the enforcer's copies. A revoke that fails leaves a copy in memory until restart; it authorizes nothing, because the ids are never reused.
- **Deleting a shared account ends it for everyone in it.** One owner or admin can take the account from every member, and a member who belonged to nothing else loses their login with it. The app can show `member_count` before the confirmation, but core does not require it. A surviving member gets a plain 401 on their old session, not a code that says the access was taken away.
- **The bearer path pays one account read per request**, and answers 503 when that read fails.
- **An impersonating administrator now lands only in an active account.** Impersonating a person whose only account is suspended used to land in it, and is now refused 401 `account_deactivated`. It follows from the same rule as the lock.
- **Account deletion does not revoke a Sign in with Apple token** (`wm-hhi0t`, P1). App Store guideline 5.1.1(v) has required revocation on account deletion for apps that use Sign in with Apple since June 2022, and nothing in the sequence tells an identity provider anything. The planned fix is an optional provider-revoker port called between the graph drop and the SQL purge, with the credentials the purge is about to delete. It blocks the epic's close-out and must land before the WeHungry App Store submission.
- **Enumeration by payload depends on every resource event carrying `AccountID`.** Today it does (`resource_events.go:45-48` says so and why). A future event type that omits it would leave rows behind. The erasure acceptance test queries every table by account, not through the enumeration, so an omission fails the test.
- **Large accounts.** One transaction over a large account runs long on SQLite. The deletes are chunked; the lock makes a long transaction safe, not fast.

## Further Reading

- [Cross-Preset Link Definitions]({% link decisions/cross-preset-link-definitions.md %}) — the rule A1 deviates from, and why A3 was not taken.
- `constitution.md`, Article I (1.2.0 on story 3's branch) — the erasure exception and the scope of the article.
- `tests/e2e/features/food_types.feature` and `tests/e2e/features/house_vocabulary_domain.feature` — the moved types, and the #520 outline with its `reuses` column.
- `tests/e2e/features/account_upload_folders.feature` — upload ownership and the read route.
- `tests/e2e/features/account_deletion.feature` — the deletion contract, including what members, locked accounts and connectors meet afterwards.
- `tests/e2e/features/account_scoped_sessions.feature` — the deactivation and revocation refusals the lock relies on.
- `tests/e2e/features/per_account_knowledge_graph.feature` — the per-account graph layout the drop relies on.
