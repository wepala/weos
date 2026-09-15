---
title: "ADR: Trusted-Issuer Login Assertion"
parent: Architecture Decision Records
layout: default
nav_order: 2
---

# ADR: Trusted-Issuer Login Assertion (`POST /auth/assert`)

**Status:** Proposed (revised 2026-09-12 after design premortem; amended 2026-09-12 after the story `wm-63gg0.1` review: clock leeway, audience uniqueness, key-list throttle and backoff, `keys-unreachable`; amended 2026-09-12 after the story `wm-63gg0.2` review: what owner binding never links to, how emails compare, the 409, what binding logs, a 2-second first backoff; amended 2026-09-12 after the PR 563 Copilot review: the route refuses to mount under core's public `SESSION_SECRET`, and any trusted-issuer setting makes the API require a sign-in; amended 2026-09-13 for bead `wm-x0l4m`: the door may post the assertion server-side, and the door's own provider key `door` is accepted, still needs a verified email and never proves an owner; amended 2026-09-14 after the `wm-x0l4m` review: an upgrade note for an instance that already has people, what an unseen identity whose email only a `door` credential holds meets today, and the `issuer` providers entry lists the provider keys an assertion may name; amended 2026-09-14 for bead `wm-6lx6z`, on decision `wm-vvi6t`: owner binding runs with or without `OAUTH_ALLOWED_EMAILS`, and a `door` credential proves who owns its email for a `google` or `apple` identity, so a door password identity and a Google or Apple identity with the same email are one person, in either order; amended 2026-09-14 after the `wm-6lx6z` review: a second `door` subject is refused while an active `door` credential of an active person holds the email, whatever else that person holds; a `door` credential is the issuer's word on the email, and an issuer writes one only for an address it controls or has proved; the `issuer` providers entry says `joins_door_and_google_apple_by_email`; the upgrade note says what a new identity meets for an owner an older core split into two people, and gives a query that finds them; amended 2026-09-15 for bead `wm-lnimb` and its review: a sign-in that asks for a native session gets a refresh token and two expiries, `POST /api/auth/refresh` renews it with a 30-second grace window for a repeated renewal, a native sign-out ends one session unless it asks for every one, and native refresh token rows are purged 7 days past expiry)
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
- **A new provider key reaches every trusting instance first.** Every instance that trusts
  the issuer is upgraded to a core that accepts a provider key before the issuer sends
  that key. An older core refuses the key as `claims`, the reason a missing `sub` also
  gets, so a refusal cannot tell the issuer that the instance is too old. The issuer reads
  the keys an instance accepts from the `issuer` entry of `GET /api/auth/providers`, in
  `accepted_provider_keys` (see "Renewal"). An `issuer` entry without that field comes
  from a core that does not accept `door`.

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
key does. A `door` credential proves who owns its email for a `google` or `apple` identity,
and for no other (see "Which credentials prove ownership").

- **Upgrading an instance that already has people.** An instance that holds people before
  the door signs anyone in — password accounts, invited members, people from its own
  Google or Apple sign-in — gets a second way in for each of them when the door starts
  to assert for it. Owner binding links a new identity by email to a credential that
  proves ownership, whether or not `OAUTH_ALLOWED_EMAILS` is set. So, before an existing
  instance takes door sign-ins:
  - Decide who may come in. `OAUTH_ALLOWED_EMAILS` admits only the people it names;
    without it, the instance admits anyone the door vouches for. It does not decide
    whether an identity is linked: a person whose Google or Apple credential holds the
    email is reached either way.
  - Where the operator created every password account on the instance, also set
    `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS=true`. Without it, a password credential proves
    nothing, and a door sign-in for its email is refused `unproven-owner`, with or without
    an allowlist.
  - An invited member always needs an operator. An `invite` credential never proves an
    owner, with or without that setting, so the member's first door sign-in is refused
    `unproven-owner` until an operator clears it (see "Clearing `ambiguous-owner` and
    `unproven-owner`").
  - Offer a second sign-in method only to an instance that says it joins them. A core with
    the `wm-6lx6z` amendment puts `"joins_door_and_google_apple_by_email": true` on the
    `issuer` entry of `GET /api/auth/providers` (see "Renewal"). An older core makes a
    second, empty person when a person who signed up with a door password later signs in
    with Google or Apple, or the other way round, and the two stay apart after the
    instance is upgraded. So an issuer offers a person a second sign-in method to an
    instance only when that field is present.
  - Look for an owner who is already two people. A core older than the `wm-6lx6z`
    amendment linked nothing on an instance with no allowlist, so an owner who signed in
    there with two methods became two people. Each person keeps working by its own
    identity. A new identity for that email meets one of these, depending on which two
    people exist:
    - One person holds a `door` credential and the other a `google` or `apple` one. A
      third Google or Apple identity is refused `ambiguous-owner`, because both
      credentials prove the email for it. A NetSuite identity is linked to the person who
      holds Google or Apple, because a `door` credential proves nothing for it. A new
      `door` identity follows the two-door rule: it is refused `unproven-owner`, because a
      `door` credential already holds the email (see "Which credentials prove ownership").
    - Both people hold `google` or `apple` credentials. Every new identity, whatever its
      provider, is refused `ambiguous-owner`.

    To find these owners, run this read-only query against the instance's database. It
    lists every email that two or more active people hold through active `google`, `apple`
    or `door` credentials, with one row for each such credential:

    ```sql
    SELECT LOWER(TRIM(c.email)) AS email, c.agent_id, c.provider
    FROM credentials c
    JOIN agents a ON a.id = c.agent_id
    WHERE c.active
      AND a.status = 'active'
      AND c.provider IN ('google', 'apple', 'door')
      AND LOWER(TRIM(c.email)) IN (
        SELECT LOWER(TRIM(c2.email))
        FROM credentials c2
        JOIN agents a2 ON a2.id = c2.agent_id
        WHERE c2.active
          AND a2.status = 'active'
          AND c2.provider IN ('google', 'apple', 'door')
          AND TRIM(c2.email) <> ''
        GROUP BY LOWER(TRIM(c2.email))
        HAVING COUNT(DISTINCT c2.agent_id) > 1
      )
    ORDER BY email, c.agent_id, c.provider;
    ```

    It runs as written on SQLite and on Postgres. On Postgres, `LOWER` also folds non-ASCII
    capitals, so the query can list a pair that binding keeps apart (see "How emails
    compare"). Core cannot merge two people. An operator decides which person is the owner
    and turns the other off (see "Clearing `ambiguous-owner` and `unproven-owner`").
    Turning a person off hides that person's data: nothing moves to the owner, and nobody
    can reach it by signing in.

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
`FindOrCreateAgent` does. If none exists, look at the credentials whose email equals the
claim's email, whether or not `OAUTH_ALLOWED_EMAILS` is set:

- When exactly one active person holds a credential that **proves ownership** for the
  arriving identity (see "Which credentials prove ownership" below), the new
  `(provider, sub)` is linked to that person instead of a second agent being created.
- When no credential holds the email, create, as today.
- When credentials hold the email but none of them proves ownership, refuse with
  `unproven-owner` (below).

A person the assertion gives no name is named after the email's local part.

- **The allowlist does not decide binding.** Binding first ran only on an allowlisted
  instance, because it then linked to a credential of any kind, and on an instance that
  admits anyone an email alone says nothing about who owns it. It now links only to a
  credential whose email was proved (see "Which credentials prove ownership"), and that
  proof holds whether or not the operator named the owners. The instances behind the
  mini-me front door run with no allowlist, and without binding there an owner who used a
  second sign-in method became a second, empty person (decision `wm-vvi6t`). The allowlist
  still decides who is admitted, before binding runs. Binding is reached only through
  `POST /auth/assert`, so it never applies to the instance's own OAuth sign-in or to an
  invite.

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
- **Races.** Sign-ins for one identity, and sign-ins for one email, are serialized in
  process, so two first sign-ins that arrive together leave one person. The locks are per
  process. Replicas that share a database still race, and the
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
its email only when it is active and its kind proves the email for the arriving identity.

- **It is active, and its person exists and is active** (pericarp's `Active` flags). A
  sign-in method or a person that someone turned off must not come back through the door.
- **It is a `google` or `apple` credential**, whose provider verified the address before
  the credential was written. It proves the email for every arriving identity.
- **Or it is a `door` credential, and the arriving identity is `google` or `apple`.** A
  `door` credential means the issuer vouches for its email (decision `wm-vvi6t`). It is
  not a record that a code was sent. The mini-me door proves the address with a code sent
  to the mailbox when a person signs up, but its operator also writes people directly,
  with no code, on two paths: demo people (`writeDemoPerson`, used by
  `tools/door-register.sh demo`), and owner people (`ownerPerson`, used by
  `register --identity`, which pairs an owner with the owner's own Google identity).
  Demo addresses are on a domain that receives no mail, so nobody can hold a Google or
  Apple account for them. **An issuer must only write `door` credentials for addresses it
  controls or has proved.** A
  door password identity and a Google or Apple identity with the same email are therefore
  one person, in either order. The issuer sends one provider key and one `sub` for each
  person it asserts, so a person who signed up to the door with a password and later signs
  in through the door with Google reaches the instance as a `google` identity it has not
  seen, and that identity is linked to the person whose `door` credential holds the email.
  A `door` identity the instance has not seen is linked, the same way, to the person whose
  `google` or `apple` credential holds its email, unless a `door` credential already holds
  that email (see the `door` entry below).
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
- **`door`, for an arriving identity that is not `google` or `apple`.** A second `door`
  identity for an email comes only from an operator re-creating the person at the door,
  and it is never joined to the first. While an active `door` credential of an active
  person holds the email, a `door` identity the instance has not seen is refused **409**
  `unproven-owner`, with or without an allowlist, whatever other credentials that person
  holds: a `google` or `apple` credential beside the `door` one proves the email, but not
  that the two door subjects are one person. The mini-me front-door record keeps its
  re-creation rule on this. The refusal ends when the earlier `door` credential, or its
  person, is turned off; the new identity then links as any other identity does.
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
  organization gives a departed person's address to someone new, and the instance still
  admits it: it has no allowlist, or the allowlist still names it. The new holder's first
  door sign-in, with an identity the instance has never seen, is then linked to the
  previous holder and reaches that person's data. The link's log line names the person
  and the new identity. Before an address goes to someone else, turn off the person who
  held it, or remove the address from the allowlist where there is one. On an instance
  with no allowlist, turning the person off is the only way.
- *An email that no credential holds creates a second person.* Binding compares the email
  the door sends with the emails that credentials here already hold, so the door must
  send the door-account email. Nothing matches, and a second, empty person is created,
  when the door account itself was made with an Apple "Hide My Email" relay address, when
  the person's Google email has changed since their credential was written, or when the
  operator made the account under a different address. On an allowlisted instance, the
  warning above is the signal; on an instance with no allowlist, the created person's info
  line is. An allowlist entry that lets such a person in must equal the email on the
  owner's existing credential.

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
  - when the owner or the owner's credential that proves ownership (`google`, `apple`, or
    `door` for a Google or Apple sign-in) was turned off by mistake, turn it back on;
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

*Amended 2026-09-15 for bead `wm-lnimb` and its review (`wm-nybvk`, `wm-ehtnq`): the
native session.* An app in a native shell holds no cookie, only the token, and the token
lasts one hour. So a sign-in may ask for a **native session** with `"session":"native"` in
its request body, on `/auth/assert`, `/auth/password-login` and `/auth/register`. That
answer adds three fields beside `token`:

- `refresh_token`: renews the session at `POST /api/auth/refresh` (see "Renewal").
- `refresh_token_expires_at`: when the refresh token stops renewing, 30 days after it was
  issued. Every renewal hands back a new one with a full 30 days.
- `token_expires_at`: when `token` expires, one hour after it was issued.

Any other `session` value, or none, is a browser's sign-in, and its answer is exactly the
shape above with none of the three fields: a browser renews through its cookie session,
and must not hold a long-lived credential that page script can read. An instance that
cannot renew answers a native sign-in without them too. A native session's access token
carries a `native_session` claim naming its refresh token family, so a sign-out with only
that token can end that session. The door relays this answer to the app, so it must pass
the three fields through unchanged (door bead `wm-4suse`).

**Renewal.** When a session expires on an instance whose only sign-in is a trusted
issuer, `/api/auth/providers` answers `[]` today and the SPA shows buttons that do
nothing. In fleet mode the instance publishes `TRUSTED_ISSUER` as its provider entry
(`{"name":"issuer","login_url":"<TRUSTED_ISSUER>/door/start","accepted_provider_keys":["apple","door","google","netsuite"],"joins_door_and_google_apple_by_email":true}`)
so the SPA's existing providers list sends the person back to the door, which re-asserts.
Owned by story 3. `accepted_provider_keys` lists, sorted, the provider keys an assertion may
name on this instance (`application.OAuthProviderKeys()`), so the issuer can check an
instance before it sends a person there (see "Request"). The field was added beside the
others; no existing field changed. `joins_door_and_google_apple_by_email` is always `true`
where it is present: it says that this core joins a `door` identity and a `google` or
`apple` identity with the same email into one person (see "Which credentials prove
ownership"). An older core leaves it out, and an issuer offers a second sign-in method to an
instance only when it is present (see the upgrade note under "Request"). It too was added
beside the others, and no existing field changed. A registry provider's entry carries none
of `login_url`, `accepted_provider_keys` and `joins_door_and_google_apple_by_email`.

*Amended 2026-09-15 for bead `wm-lnimb` and its review (`wm-utb5c`, `wm-tu180`,
`wm-ehtnq`, `wm-3dgs0`, `wm-sa7wv`, `wm-5rziu`): renewing and ending a native session.*
A native session (see "Response") renews without the door.

`POST /api/auth/refresh` takes `{"refresh_token":"..."}`: JSON, at most 16 KiB. The refresh
token is the only credential it reads; it needs no cookie and no bearer token, and ignores
either when a request carries one. Every answer carries `Cache-Control: no-store`.

- **200** `{"data":{"account":{"id","name"},"token","token_expires_at","refresh_token","refresh_token_expires_at"}}`.
  An owner or admin of an account whose deletion did not finish also gets
  `"erasure_pending":true` and `"code":"account_erasure_pending"`, and the token serves only
  `DELETE /api/account`. The presented refresh token is spent, and the new one replaces it
  in the same family. The new token carries no `token_use` mark.
- **400** `invalid_request` when the body has no refresh token. **413** when the body is too
  large.
- **401** with a `code`:
  - `invalid_refresh_token`: unknown, expired, spent, revoked, a connector's refresh token,
    or an account that is gone.
  - `account_access_revoked`: the person was removed from the account. The refresh token is
    revoked too.
  - `account_deactivated`: the account is suspended. The refresh token is not revoked.
  - `account_erasure_pending`: the account's deletion did not finish, and the person is not
    an owner or admin.
- **503** with `Retry-After: 5` when the store cannot be read, or a rotation failed and
  nothing was spent. A failed rotation never answers 500.

A native refresh token is refused at `/oauth/token`, and a connector's is refused here.

**Reuse and the grace window.** A spent refresh token presented again revokes its whole
family, because someone else holds a copy. There is one exception: an exact repeat within
**30 seconds** of the rotation, while the successor that rotation made is still live. That
repeat is a renewal whose answer was lost, or several renewals at once. It gets the same
successor and a new access token. A repeat after the window, a repeat whose successor was
already spent, and any token of a revoked family get 401 and no successor. The successor
is derived: an HMAC-SHA256 of the spent row's id and the presented token, under a key
derived from `JWT_SIGNING_KEY`. So the store keeps only hashes, and names the successor on
the spent row by row id (`successor_id`, `rotated_at`). Without a signing key the key is
random per process, and a repeat that reaches another process is reuse.

**Sign-out.** `POST /api/auth/logout` still clears the cookie and the browser session. A
native app also ends its session there, with its refresh token in the body or its access
token as the bearer. An expired access token this instance signed still names its
session.

- By default only that one session, its refresh token family, ends. A spent, revoked or
  expired refresh token ends only its own family.
- `{"everywhere":true}` ends every native session of the person, and only with a live
  credential: a refresh token that still renews, or an access token that still validates.
  Either one counts only while a renewal for that person in its account would go ahead
  now. A removed member's or a suspended account's credential ends its own session, and
  the answer carries `sign_out_everywhere_refused`.
- A sign-out and a rotation of the same family can overlap. The sign-out reports success
  only after a fresh read of the family finds no live token, so a successor committed
  mid-sign-out is revoked too.
- A rotation spends a refresh token only if it is still unexpired when the rotation
  holds the row. A renewal that waited past the expiry is refused.
- A sign-out that presents a credential adds `app_session` to pericarp's
  `{"status":"logged out"}`: `ended`, `ended_everywhere`, `not_identified` or `not_ended`.
  When it did not do all it was asked, it also adds `code`: `app_session_not_identified`,
  `sign_out_everywhere_refused` or `app_session_not_ended`.
- A cookie-only sign-out answers as before.
- A store failure never blocks the browser's sign-out: the answer is 200 with
  `app_session_not_ended`, and the app signs out again.

**Storage.** Native refresh tokens share the OAuth refresh token table under the reserved
client id `weos-native`, hashed at rest. A row more than **7 days** past its expiry is
purged, at most once an hour per process, when a native refresh token is issued or
rotated. Until then a spent row stays, so presenting it is still caught as reuse.

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
- Good: an owner can use Google one day, Apple the next and the door's password after that,
  and stay one account, on an instance with or without an allowlist.
- Bad: the issuer's word on an email joins accounts across sign-in methods (decision
  `wm-vvi6t`), and a sign-in method is only as safe as the issuer's rule for writing
  `door` credentials (see "Which credentials prove ownership"). A door
  password opens a person whose account Google or Apple made, without their recovery or
  second factor, and a link stays made if the rule is taken back later.
- Bad: on an instance with no allowlist, an email held only by credentials that prove
  nothing (an invite, or a password without the opt-in) is now refused `unproven-owner`
  instead of creating a person, so whoever can write such a credential for an address can
  make that address's first door sign-in wait for an operator.

  *Amended 2026-09-15 (finding `wm-yg7va`, bead `wm-am5ly`):* this side effect stands, with
  three safeguards so an operator can see and fix the refusal. (1) When the route mounts
  on an instance with no `OAUTH_ALLOWED_EMAILS` and `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS`
  is off, boot logs one warning that names `TRUSTED_ISSUER_LINK_PASSWORD_OWNERS`. It is
  read from configuration alone, so it also appears on an instance that holds no password
  or invite credential. (2) The 409 answer for `unproven-owner` and for `ambiguous-owner`
  tells the person to contact the operator of the instance; the codes do not change.
  (3) An acceptance scenario in `tests/e2e/features/trusted_issuer_account.feature`
  stages an email that only a password holds, on an instance with no allowlist, and
  checks the refusal and its text.
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
