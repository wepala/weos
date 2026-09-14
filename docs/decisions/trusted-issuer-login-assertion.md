---
title: "ADR: Trusted-Issuer Login Assertion"
parent: Architecture Decision Records
layout: default
nav_order: 2
---

# ADR: Trusted-Issuer Login Assertion (`POST /auth/assert`)

**Status:** Proposed (revised 2026-09-12 after design premortem; amended 2026-09-12 after the story `wm-63gg0.1` review: clock leeway, audience uniqueness, key-list throttle and backoff, `keys-unreachable`; amended 2026-09-12 after the story `wm-63gg0.2` review: what owner binding never links to, how emails compare, the 409, what binding logs, a 2-second first backoff; amended 2026-09-12 after the PR 563 Copilot review: the route refuses to mount under core's public `SESSION_SECRET`, and any trusted-issuer setting makes the API require a sign-in; amended 2026-09-13 for bead `wm-x0l4m`: the door may post the assertion server-side, and the door's own provider key `door` is accepted, still needs a verified email and never proves an owner)
**Date:** 2026-09-12
**Ticket:** bead `wm-63gg0` (mirror: wepala/mini-me-weos#530)
**Base:** `v3` (the integration branch the `v3.0.1-beta.*` tags are cut from; `main` is the old line)

## Context and Problem Statement

The mini-me fleet is gaining a front door: one public host that signs people in with
Google or Apple and reverse-proxies each person to their own private WeOS instance.
The door — not the instance — completes the OAuth flow, because only the door can
decide which instance a newly-verified person should reach. The instance still owns
its accounts, sessions and data, so it needs a way to accept "this person is verified,
log them in" from a service it trusts, without talking to Google or Apple itself.

Core's auth today knows exactly two ways in: its own Google/Apple OAuth flow
(`internal/oauth`, ending in pericarp's `FindOrCreateAgent`, which resolves identity by
`(provider, sub)` only) and password auth (`/auth/register`, `/auth/password-login`,
mounted only when `PASSWORD_AUTH_ENABLED` — the mount-or-not precedent in
`MountPasswordAuth`). Neither fits: the OAuth flow requires the instance to be the OAuth
client, and the password path would force the door to hold a secret per person.

## Decision Drivers

- The door must be able to log a person into an instance it fronts, first login included,
  and again every time the instance session expires.
- The instance must be able to refuse anything not signed by its one configured issuer.
- No credential material stored per person anywhere new.
- Key rotation must not require touching every instance, and must never lock the fleet out.
- A single-user instance must never mint a second, empty account for its one owner.
- Framework-shaped: any fleet product (WeHungry next) reuses it unchanged.

## Considered Options

1. **Signed login assertion verified against the issuer's JWKS** — a new
   `POST /auth/assert` accepting a short-lived JWT from a configured trusted issuer.
2. **Trusted proxy header** (`X-Forwarded-User`) — the instance trusts an identity
   header set by the proxy.
3. **Stored per-person secret** — the door registers each person with a random
   password and replays it through `/auth/password-login`.
4. **Full OIDC provider on the door** — the instance runs its existing OAuth flow
   against the door as the IdP.

## Decision Outcome

Chosen option: **1 — signed login assertion**, because it is unforgeable off-path
(unlike 2, where any misconfigured hop can inject the header), stores nothing per
person (unlike 3 — rejected explicitly by the owner on 2026-09-12), and is a fraction
of the surface of 4 while carrying the same guarantee for this topology (the door
already fronts every session).

### Contract

**Mounting.** `POST /auth/assert` exists only when all three of `TRUSTED_ISSUER`,
`TRUSTED_ISSUER_JWKS_URL` and `TRUSTED_ISSUER_AUDIENCE` are configured and
`SESSION_SECRET` is the instance's own (see "Session secret" below), following the
`MountPasswordAuth` precedent (mount-or-not; a missing route is a plain 404, never a
mounted handler that refuses, so middleware ordering cannot turn it into a 401). When
exactly one or two of the three are set, boot logs one warning naming the missing keys
and mounts nothing.

**Request.** Body `{"assertion": "<JWT>"}`. The browser may make this request itself,
through the proxy, or the door may post the assertion server-side and relay the answer —
the status, the body and every `Set-Cookie` — on the host that serves the instance; either
way the instance's `Set-Cookie` lands in the browser exactly as it does for
`/auth/password-login`.

- **A request from another site is refused before the assertion is read.** An assertion
  signs in whoever posts it. A page on another site that holds a valid assertion for this
  audience could otherwise post it from a victim's browser and sign that browser in as
  someone else (login CSRF). A browser marks what it sends, so:
  - when the request carries `Sec-Fetch-Site`, it must be `same-origin`;
  - when it carries `Origin`, that must be exactly one origin, and it must be the trusted
    issuer's origin (the door serves the instance on its own origin) or the instance's
    public origin (`BASE_URL`, or the address serve derives when it is unset).

  Otherwise the answer is **403** with the code `cross-site`. The body is not read, so the
  assertion's `jti` is not spent, and the warning names the reason and at most the
  request's normalized origin. A request with neither header comes from a client that is
  not a browser, which no other site can drive, and is not affected.

**Verification.** ES256 signature against the issuer's JWKS; `iss` equals
`TRUSTED_ISSUER`, where trailing slashes on either side do not count; `aud` is exactly one
value and equals `TRUSTED_ISSUER_AUDIENCE`.

- **One issuer value.** Boot trims spaces and trailing slashes from `TRUSTED_ISSUER` once,
  when it reads the setting. That one value is what `iss` is compared with and the base of
  the door's sign-in address (see "Renewal"). A trailing slash does not change the address,
  so `https://door.example/` and `https://door.example` are the same issuer, whichever side
  writes the slash.
- **An issuer the route can use.** `TRUSTED_ISSUER` must be an absolute `https` URL (plain
  `http` only to a loopback host, as for the key list), with a host, and with no query,
  fragment or user information. Otherwise boot logs one warning, mounts nothing and offers
  no `issuer` provider, the same mount-or-not shape as a partial set. The warning does not
  repeat the value.

- **The audience is this instance's own id.** `TRUSTED_ISSUER_AUDIENCE` is unique to one
  instance and is never shared with another. Instances that share an audience all accept
  the same assertion, and `jti` memory cannot stop that, because it is per instance.
- **Clock leeway applies to `exp` and to a future `iat`.** An assertion is refused as
  `expired` when `exp` is more than **30 seconds** in the past. It is refused as `window`
  when `iat` is more than 30 seconds in the future.
- **The 60-second lifetime is strict.** `exp − iat ≤ 60s`, with **no** leeway: both values
  come from the issuer's one clock, so skew does not apply to their difference. The issuer
  mints assertions that live at most 60 seconds. One minted to live 61 seconds is refused
  as `window`.

Single-use `jti` remembered for 5 minutes in an **in-memory** store (documented as
reset on restart — acceptable because assertions expire in 60 s); required claims `sub`,
`email`, `provider`, `email_verified == true`; optional `name`. `provider` must be one of
the keys `application.OAuthProviderKeys()` lists, verbatim: core's registry keys (`google`,
`apple`, …) and `door`. `door` names an identity that the door owns itself: a person who
signed up to the door with an email and a password. No registry entry holds it, so it
reaches an instance only in an assertion. It still needs `email_verified == true`, as every
key does, and a `door` credential never proves an owner (see "Which credentials prove
ownership").

- **Upgrading an instance that already has people.** An instance that holds people before
  the door signs anyone in — password accounts, invited members, people from its own
  Google or Apple sign-in — gets a second way in for each of them when the door starts
  to assert for it. Owner binding links a new identity by email only on an allowlisted
  instance, and only to a credential that proves ownership. So, before an existing
  instance takes door sign-ins:
  - Set `OAUTH_ALLOWED_EMAILS`. Without it, nothing is linked by email: each existing
    person whose first door identity the instance has not seen gets a second, empty
    person.
  - Where the operator created every password account on the instance, also set
    `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS=true`. Without it, a password credential proves
    nothing, and a door sign-in for its email is refused `unproven-owner`.
  - An invited member always needs an operator. An `invite` credential never proves an
    owner, with or without that setting, so the member's first door sign-in is refused
    `unproven-owner` until an operator clears it (see "Clearing `ambiguous-owner` and
    `unproven-owner`").

Every refusal is a 401 whose
body and log line carry a machine-readable reason: `signature`, `kid-miss`,
`keys-unreachable`, `iss`, `aud`, `expired`, `window`, `jti-replay`, `claims`,
`allowlist`. An accepted assertion can still be refused when owner binding cannot tell
whose identity it is: that answer is **409**, not 401, with the code `ambiguous-owner` or
`unproven-owner` (see "Owner binding"). A request from another site is refused before its
assertion is read: that answer is **403** with the code `cross-site` (see "Request"). A
refusal's body and log line never carry the assertion. The `kid` in its
header is chosen by the caller, so it never reaches a refusal's text and reaches a log
line only as its first 16 characters, in printable ASCII. A request body over 16 KiB is
answered 413 before any of it is read; that is not a refusal and carries no reason.

**JWKS.** Cached 10 minutes. An unknown `kid` triggers a refetch; a refetch that
still lacks the `kid` fails **only that request** and never overwrites or extends the
cached set. The issuer is obliged to publish a new key before signing with it.

- **One miss read per 30 seconds.** Refetches caused by an unknown `kid` are limited to
  one per 30 seconds for the whole instance. Inside that interval, a `kid` that neither
  the cached set nor the last successful miss read holds is refused as `kid-miss` with no
  read. The 10-minute refresh ignores this limit.
- **One read at a time.** A request that needs a key while a read is running waits for
  that read and acts on what it found, so a burst of sign-ins with a newly published key
  costs one read and is accepted whole. A waiting request leaves when its own request
  ends. That is not a failed read: it is not logged as `keys-unreachable`, and it does
  not start or lengthen the backoff below. A read runs to its own 5-second timeout even
  when the request that started it ends, so a client that went away is never recorded
  as an unreachable key list.
- **The last successful miss read is kept.** A complete refetch caused by an unknown `kid`
  is kept beside the cached set, and a `kid` it holds is accepted until the cached set is
  next replaced. It never overwrites or extends the cached set. Without it, a refetch
  caused by an invented `kid`, which already holds a newly rotated key, would leave that
  rotation refused for 30 seconds, and an invented `kid` every 30 seconds would keep it
  refused until the cache aged out.
- **A failed read backs off.** A read that cannot complete — the key list is unreachable,
  answers other than 200, cannot be decoded, or redirects (redirects are never followed) —
  or that holds no usable key starts a backoff in which no read is made. The first
  failure backs off **2 seconds**. Each further failure in a row doubles it — 4, 8, 16 —
  up to **30 seconds**, and a complete read ends the run, so the next failure backs off
  2 seconds again. An instance that wakes with nothing cached and misses one read (egress
  not up yet, the door mid-deploy) signs people in again 2 seconds later, while an issuer
  that stays down is read at most once every 30 seconds. Cached keys, fresh or aged, stay
  in use with no time limit. Anything else is refused with no read: `keys-unreachable`
  when the read could not complete, `kid-miss` when the key list answered with no usable
  key. The failure is logged once per backoff, with the backoff's length.
  `keys-unreachable` tells the door that publishing a key will not help; reaching the key
  list will.

**Allowlist.** `OAUTH_ALLOWED_EMAILS`, when set, is enforced as the OAuth callback
enforces it; empty stays open. The `email` claim is the person's door-account email —
the address the person signed up to the door with — never a provider relay address, so
an Apple "Hide My Email" login still passes an owner's allowlist.

**Owner binding.** On success, resolve the agent by `(provider, sub)` as
`FindOrCreateAgent` does. If none exists **and** `OAUTH_ALLOWED_EMAILS` is set (the
fleet's single-user shape), look at the credentials whose email equals the claim's email:

- When exactly one active person holds a credential that **proves ownership** (see
  "Which credentials prove ownership" below), the new `(provider, sub)` is linked to that
  person instead of a second agent being created.
- When no credential holds the email, create, as today.
- When credentials hold the email but none of them proves ownership, refuse with
  `unproven-owner` (below).

With no allowlist, create, as today. A person the assertion gives no name is named after
the email's local part.

- **How emails compare.** Both emails have spaces trimmed from each end and ASCII
  capitals lower-cased, and nothing else is folded. The service and the database query
  apply the same rule. So `Dana.Whitfield@HarborLegal.example` matches
  `dana.whitfield@harborlegal.example`, but `É` does not match `é`, and U+212A KELVIN
  SIGN does not match `k`: a Unicode fold would let a different address match an
  owner's. An address that differs from the owner's only in a non-ASCII capital creates
  a second person instead, and the warning under "What binding logs" reports it. The
  allowlist compares an assertion's email under the same rule, on both its entries and
  the email, so an address that differs from an entry only in a non-ASCII capital is
  refused as `allowlist` rather than admitted and then kept apart from its owner.
  `OAUTH_ALLOWED_EMAILS` entries are still lower-cased when they are read, as the OAuth
  callback needs, so write an entry the way the door sends the email.
- **Two people holding the email.** When more than one active person holds a credential
  for the email that proves ownership, the sign-in is answered **409** with the code
  `ambiguous-owner`. Nothing is linked and nobody is created, because choosing one of
  them would sign a person in to someone else's data.
- **Credentials that hold the email but prove nothing.** When one or more credentials hold
  the email and none of them proves ownership, the sign-in is answered **409** with the
  code `unproven-owner`. Nothing is linked and nobody is created. Linking could hand the
  identity to whoever wrote that email. Creating could leave the owner in a second, empty
  account, which the owner reports as lost data. An operator decides (see "Clearing
  `ambiguous-owner` and `unproven-owner`").
- **Races.** Sign-ins for one identity, and on an allowlisted instance sign-ins for one
  email, are serialized in process, so two first sign-ins that arrive together leave one
  person. The locks are per process. Replicas that share a database still race, and the
  store's unique `(provider, provider_user_id)` index is what stops a second credential
  there. A link saves the credential row first and records `Credential.Created` only
  after the row is saved, so a link that loses that race records no event.
- **A link whose event cannot be recorded is taken back.** The row and its event are not
  written in one transaction: pericarp's UnitOfWork carries events only, and its event
  store joins an outer transaction only from inside a subscription batch. So when the
  event store fails after the row is saved, the sign-in fails and the row is deleted,
  and one error line names `owner_agent_id` and `credential_id`. A row left without its
  event would work until a projection rebuild silently dropped the link. If the row
  cannot be deleted either, a second error line, starting `repair:`, names both ids:
  delete that row from `credentials`, then let the person sign in again. The person's
  next sign-in links again.

**Which credentials prove ownership.** The list is explicit: a credential proves who owns
its email only when all three of these are true.

- **It is active, and its person exists and is active** (pericarp's `Active` flags). A
  sign-in method or a person that someone turned off must not come back through the door.
- **It is a `google` or `apple` credential**, whose provider verified the address before
  the credential was written.
- **Or it is a `password` credential, and the operator set
  `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS=true`** (default `false`). Nothing verifies a
  password credential's email. Open registration checks neither the email nor the
  allowlist, and a credential registered while `PASSWORD_REGISTRATION_ENABLED` was on
  stays after it is turned off, so the current value of that setting says nothing about
  who made the accounts. The opt-in is the operator saying that they made every password
  account on the instance. Set it only where registration was never open.

Every other credential proves nothing. Binding neither links to it nor counts it toward
`ambiguous-owner`, and an email held only by such credentials is refused as
`unproven-owner`:

- **`invite`.** Any signed-in person can invite an address from their own personal
  account and accept that invite with no session. That leaves an active person holding an
  active invite credential for an email that nobody verified.
- **`netsuite`.** NetSuite reports the email that its account administrator set, with no
  verification flag.
- **`door`.** The door proves once, at sign-up, that a person controls the mailbox. Google
  and Apple also stand behind the account's recovery over time. A `door` identity that the
  instance has not seen can still be linked to a person whose credential proves ownership,
  but a `door` credential itself never proves one (mini-me front-door decision 3C).
  The other way round does not link. The issuer sends one provider key and one `sub` for
  each person it asserts. So a person who signed up to the door with a password and later
  signs in through the door with Google reaches the instance as a `google` identity that
  it has not seen. When only that person's `door` credential holds the email:
  - on an allowlisted instance, the sign-in is refused **409** `unproven-owner`;
  - on an instance with no `OAUTH_ALLOWED_EMAILS`, where nothing links by email, it
    creates a second, empty person.

  Whether a `door` credential and a Google or Apple identity for the same email should be
  one person is an **open decision** (bead `wm-vvi6t`). This record says what core does
  today and does not choose.
- **Any provider this list does not name**, including a development provider and one
  that a downstream binary adds.

**What binding logs.** Each link, each person that a sign-in creates, and each
`ambiguous-owner` or `unproven-owner` refusal writes one structured log line. A link is
logged at info and a refusal at error. An `unproven-owner` line names every person
holding the email in `agent_ids`, and the kinds of credential they hold in
`matched_providers`. A created person is logged at info, or at warn when the instance has
an allowlist and another active person already exists: an instance with named owners
rarely gains a second person on purpose, so that person is most likely an owner whom
binding missed. Every line names the `provider` and the `agent_ids` it is about (the
warning adds `other_agent_ids`), with a `sub_hash` and an `email_hash`. It never carries
the subject or the email. A hash is the first 16 hexadecimal characters of the value's
SHA-256, and the email is folded as described under "How emails compare" before it is
hashed. To find the lines
for an address, compute
`printf '%s' 'ops@harborlegal.example' | shasum -a 256 | cut -c1-16` and search for it.

**Where binding goes wrong.**

- *A recycled email links to the person who held it before.* A credential keeps the
  email it was created with, and a returning sign-in does not update it. Suppose an
  organization gives a departed person's address to someone new, and the allowlist still
  names it. The new holder's first door sign-in, with an identity the instance has never
  seen, is then linked to the previous holder and reaches that person's data. The link's
  log line names the person and the new identity. Before an address goes to someone
  else, remove it from the allowlist or turn off the person who held it.
- *An email that no credential holds creates a second person.* Binding compares the email
  the door sends with the emails that credentials here already hold, so the door must
  send the door-account email. Nothing matches, and a second, empty person is created,
  when the door account itself was made with an Apple "Hide My Email" relay address, when
  the person's Google email has changed since their credential was written, or when the
  operator made the account under a different address. The warning above is the signal.
  An allowlist entry that lets such a person in must equal the email on the owner's
  existing credential.

**Clearing `ambiguous-owner` and `unproven-owner`.** The refusal's error line lists in
`agent_ids` the people who hold the email: for `ambiguous-owner` every active person
holding a credential that proves ownership, for `unproven-owner` every person holding any
credential for it. Decide which of them is the owner.

- For `ambiguous-owner`, turn off every other one: the person, or each of that person's
  credentials that holds the email (pericarp's `Agent.Deactivate` and
  `Credential.Deactivate`, which record `Agent.Deactivated` and `Credential.Deactivated`).
  Inactive people and credentials are not counted, so the next sign-in links to the one
  person left.
- For `unproven-owner`, make the owner provable or take the other credentials away:
  - when the owner or the owner's google or apple credential was turned off by mistake,
    turn it back on;
  - when the owner's only credential is a password account the operator made, and every
    password account on the instance is the operator's, set
    `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS=true`;
  - otherwise remove the credentials that hold the email. Turning them off is not
    enough: an inactive credential still holds the email, so the sign-in is still
    refused. With no credential for the email left, the next sign-in creates the person.

Core has no command for this yet; bead `wm-2bx2g` adds one.

**Response.** The `/auth/password-login` shape — `{agent, account, token, expires_at}`
plus the JWT session cookie — with one added boolean, `new_account`, true when this call
created the agent. (The OAuth callback signals the same fact as a `?new_account=1`
redirect query; a JSON caller has no redirect, hence the field.)

**Renewal.** When a session expires on an instance whose only sign-in is a trusted
issuer, `/api/auth/providers` answers `[]` today and the SPA shows buttons that do
nothing. In fleet mode the instance publishes `TRUSTED_ISSUER` as its provider entry
(`{"name":"issuer","login_url":"<TRUSTED_ISSUER>/door/start"}`) so the SPA's existing
providers list sends the person back to the door, which re-asserts. Owned by story 3.

**Session secret.** A fleet instance must run with a per-instance `SESSION_SECRET`,
and the route refuses to mount without one. Core's default value is public, so a
session signed with it can be forged, and `serve.go` also sends cookies without
`Secure` while that default is in use (it keys `SecureCookies` off it). When all three
`TRUSTED_ISSUER*` settings are set but `SESSION_SECRET` is that default, or empty, boot
logs one **error** naming `SESSION_SECRET`, mounts nothing and offers no `issuer`
provider — the same mount-or-not shape as a partial set, on the same one condition for
the route and the provider entry. A partial set is reported first, because it stops the
route whatever the secret is. An instance with no `TRUSTED_ISSUER*` setting logs nothing
about the secret, so local development on the default is unchanged. The pool module sets
one per instance (E2).

**Sign-in required.** A trusted issuer is a real sign-in, so it takes the API out of
development mode exactly as an OAuth provider or password sign-in does: the protected and
account routes require a session, `/api/auth/me` answers for that session and not for the
seeded dev user, and the feature listing, invite acceptance and MCP routes take the
session and bearer checks they take under OAuth. Development mode answers every caller as
the seeded dev user, so an instance whose only sign-in is the door and that fell back to
it would give that account to anyone who reached it. The switch happens as soon as any
one `TRUSTED_ISSUER*` setting is set, whether or not the route mounts: a partial set, an
unusable key-list address or the public `SESSION_SECRET` leaves an instance that requires
a sign-in and, with no OAuth provider or password sign-in configured, admits nobody. The
boot line that names the problem says so. It never leaves the API open.

**Rolling out.** Add the three `TRUSTED_ISSUER*` settings to an instance only when the
door is already serving that instance. The first setting locks every API route behind a
sign-in, whether or not the route mounts, and an instance whose only sign-in is the door
answers 401 everywhere until an assertion from the door signs someone in. Every boot line
about a trusted issuer says so in its `consequence` field: the info line for a mounted
route ("the API is locked … until an assertion from the trusted issuer, or another
configured sign-in, signs someone in"), and the warning or error for one that did not
mount. `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS` is not one of the three settings and locks
nothing.

A door-only instance also runs the authorization server that gives MCP connectors their
bearer tokens, and it honors `JWT_SIGNING_KEY` as an OAuth or password instance does. Set
`JWT_SIGNING_KEY` to a PEM-encoded RSA key that belongs to the instance. Without it, the
instance signs with a new key at every start, so every restart or idle-stop wake makes
every connector authorize again.

### Consequences

- Good: one door key rotation (a new key beside the old in the JWKS) covers the fleet,
  and a bad rotation fails one login at a time, never a cache TTL of logins.
- Good: an instance outside the fleet (no `TRUSTED_ISSUER*`) is byte-identical to today.
- Good: an owner can use Google one day and Apple the next and stay one account.
- Bad: the instance makes an outbound HTTPS call to the JWKS URL; a fleet instance
  therefore needs egress to the door's host, or the key delivered by env instead —
  the config keys deliberately leave room for `TRUSTED_ISSUER_JWKS` (inline key) later.
- Bad: an instance started on demand must be healthy before the assertion is minted;
  that is a door obligation (mint after `/api/health`, and mint a fresh assertion — a
  new `jti` — on every retry), recorded on the door epic.

## More Information

Downstream consumer: the mini-me Money front door (bead `wm-eb4mv`), which signs
assertions with a Secret Manager key and publishes `/door/jwks.json`. The pin bump that
puts this endpoint on the fleet (story `wm-63gg0.4`) lands in `wepala/mini-me-weos` and
waits on a published `v3` tag; the door must not send traffic to instances until that
pin is deployed.
