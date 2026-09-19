---
title: "ADR: Door-Initiated Token Revocation"
parent: Architecture Decision Records
layout: default
nav_order: 3
---

# ADR: Door-Initiated Token Revocation (`POST /auth/revoke-tokens`)

**Status:** Proposed (amended 2026-09-19 when the work resumed: the revocation reaches only the holder of the asserted identity and never resolves by email; issuer-key compromise buying revocation is recorded as an accepted consequence; no rate limit, as on `/auth/assert`; a section on where the code corrects what bead `wm-fcpzx` records)
**Date:** 2026-09-16
**Ticket:** bead `wm-fcpzx`, from finding `wm-p3h32`; the door's caller is bead `wm-a8yyh` (story `wm-eb4mv.10`)
**Base:** `v3` (the integration branch the `v3.0.1-beta.*` tags are cut from; `main` is the old line)

## Context and Problem Statement

A person resets their password at the fleet's front door. The door owns the password, but
each instance issues its own credentials, and nothing the door can do ends them. So a
reset — the thing a person does *because* somebody else may hold their device — leaves
that somebody with working access.

Verified in the code on 2026-09-16, before any of this was written:

- A refresh token is not a JWT. It is an opaque random string, stored as its SHA-256 hash
  in `oauth_refresh_tokens` (`internal/oauth/repository.go`, `internal/oauth/models.go`).
  `handleRefreshGrant` resolves it by hash and checks revoked, expiry and client — it
  reads `JWT_SIGNING_KEY` nowhere. **Rotating the signing key is not an eviction**: it
  invalidates issued access tokens, the holder presents the refresh token, and core signs
  a fresh one with the new key. Seconds of interruption.
- A refresh token lives 30 days, and **every rotation grants a fresh 30 days**, so a
  client that keeps renewing never expires.
- Two kinds of refresh token live in that one table: an MCP connector's, under the client
  id it registered, and a native app session's, under the reserved client id
  `weos-native` (`wm-lnimb`).
- The levers that exist reach neither for the door. `POST /api/auth/logout`
  `{"everywhere":true}` ends every *native* session of a person, but only for a caller
  holding that person's own live credential — which is exactly what the door does not
  have, and what the intruder does. Nothing at all ends a connector's tokens from
  outside. `SESSION_SECRET` rotation ends browser sessions for *everybody on the
  instance* at once, and does not touch the bearer path anyway
  (`api/middleware/bearer_or_session.go` short-circuits to the JWT before the cookie).

So: browser sessions have a blunt lever, and **token access has none**. The only ways to
stop a connector today are to edit `oauth_refresh_tokens` by hand or to retire the
instance.

## Decision Drivers

- The door must be able to end one person's token access without that person present.
- It must not sign everyone out. An instance-wide lever is what we already have, and why
  it is not the answer.
- No new credential material per instance to distribute, store or rotate.
- A caller that is not the door must not be able to reach it.
- The answer must not tell a caller whether an address has an account here.

## Considered Options

1. **A revocation assertion from the trusted issuer**, at a route mounted beside
   `/auth/assert`.
2. **A shared secret** (`DOOR_ADMIN_TOKEN`) the door presents.
3. **Extend `/auth/logout` `{"everywhere":true}`** to accept the door's word instead of
   the person's credential.
4. **Nothing in core**: document the manual `UPDATE oauth_refresh_tokens` an operator can
   run.

## Decision Outcome

Chosen option: **1 — a revocation assertion**.

Against 2: a shared secret is a second credential per instance to deliver, store and
rotate, and the fleet has already solved rotation for the door's signing key — a new key
beside the old in the key list covers every instance, and a bad rotation fails one call at
a time. Against 3: `everywhere` is a person's own sign-out, and widening it to a caller
who holds no credential of that person would put a door-sized hole in the route a browser
also uses. Against 4: an operator editing a token table during an account takeover is not
a remedy, and the finding this comes from exists because the runbook named a remedy that
does not work.

### Contract

**Mounting.** `POST /api/auth/revoke-tokens` mounts on exactly the condition
`/auth/assert` mounts on: all three `TRUSTED_ISSUER*` settings, an issuer and key-list
address the verifier can use, and a `SESSION_SECRET` of the instance's own. An instance
that cannot take an assertion cannot take a revocation; an instance that takes sign-ins
must give the door a way to end what those sign-ins handed out. The unmounted path is
never registered, so it answers like a path the server has never had. It logs nothing when
it does not mount — the assertion route has already said what is wrong with the same
settings, in one line.

**Request.** Body `{"assertion":"<JWT>"}`, at most 16 KiB. The door calls it server-side.

**Authorization is the login assertion's, with one claim more.** Same issuer, same
published key list, same audience, same provider keys, same allowlist, same single-use
`jti`, same 60-second life, same clock leeway — one implementation
(`internal/trustedissuer`), not a second copy. The assertion must carry
`"purpose":"revoke-tokens"`.

- An assertion carrying no `purpose` is a **login** assertion. That keeps the login
  contract exactly as it was: the claim was added after the route, and an issuer minting
  what the contract has always described is unchanged.
- So a login assertion presented here is refused `claims`, and a revocation assertion
  presented at `/auth/assert` is refused `claims`. One captured on its way to either route
  is useless at the other.
- The purpose is checked **before the `jti` is spent**, like every other claim: an issuer
  that minted the wrong kind corrects it and presents the same `jti`.
- The two routes hold **separate verifiers**, so neither can spend the other's assertion
  out of a shared replay memory.

**Cross-site.** The same guard as `/auth/assert`, and now one copy of it
(`crossSiteRequest`): a browser that marks the request as coming from another site, or
carries an `Origin` that is neither the instance's nor the issuer's, is refused **403
`cross-site`** before the body is read, so the `jti` is not spent. A server-side call
carries neither header and passes — which is how the door calls it.

**Whom it reaches.** Exactly one person, or nobody: the person holding a credential for
the asserted `(provider, subject)`. It creates nobody and links nothing, and it **never
resolves by email**.

The door asserts the identity the reset password signs in with — provider `door`, its own
subject for the person. That identity alone reaches every token the reset is about. A
token issued under the door's password was issued through a door sign-in, and every door
sign-in leaves a `door` credential on the person it signed in: owner binding links it to
the person who already owns the email, or the sign-in creates a person holding it. So the
person holding that credential holds every refresh token the old password ever handed out
here — and, because owner binding joins a door identity and a Google or Apple identity
with the same email into one person, every other token that person holds too. An identity
this instance has never seen means the old password never signed anybody in here, and
there is nothing to revoke.

**Why not also by email** (the first draft of this ADR did, and was changed before merge).
The fallback — every active person whose credential proves the asserted email, and every
candidate where a sign-in would refuse `ambiguous-owner` — reached further than the reset
needs. On an instance with no `OAUTH_ALLOWED_EMAILS` it let the issuer name any email and
end the tokens of a person it had never signed in, bounded only by the issuer's honesty.
The case it was for — the instance that had people before it had a door, holding tokens
from a Google or password sign-in — is not the reset's business: a door password never
issued those tokens, and resetting it does not make them any less the owner's. The door
knows each person's door identity, because it registers them, so the narrower reach costs
it nothing. It also narrows what a stolen issuer key buys (see "Consequences"): the thief
must know each person's opaque door subject, not just an address.

**What it revokes.** Every refresh token of the person reached, in every family, client
and account — each connector's and each native app session's
(`RevokeAllForAgent`). The person's own honest clients must authorize or sign in again:
after a password reset, nothing distinguishes the intruder's client from the owner's, and
that is the point.

**Answer.** **204**, always, for an accepted assertion — whether the instance knows the
person, whether they held any token, and whether this call or an earlier one revoked it.
The door learns only that the person now holds no refresh token here; anything else would
make the route an oracle for whether an address has an account on this instance. It is
idempotent for the same reason it is silent.

- **401** with the refusal's reason as the code, exactly as `/auth/assert` answers one.
  The assertion never reaches a log line.
- **403** `cross-site`; **413** for a body over the limit.
- **503** with `Retry-After` when the store could not be written. The tokens may still
  renew, so the door asks again. A 204 there would report an eviction that did not happen.

**What it does not do.**

- **It does not end browser sessions.** Those are signed cookies with no store to revoke;
  `SESSION_SECRET` rotation is still the only lever, and it is instance-wide. This route
  exists because the *token* half had no lever at all.
- **It does not shorten an access token.** Those are stateless and last their hour, so
  revocation stops renewal, not the current hour.

## Consequences

- Good: a password reset at the door can finally end token access, for one person, with no
  new secret anywhere and no operator at a database.
- Good: the fleet's existing key rotation covers this route the moment it covers the
  sign-in.
- Bad: the person's own connectors and app sessions must be reconnected after a reset.
  That is the intended meaning of the reset, but it is a cost the door should say out loud
  to the person.
- Bad: an intruder's access survives for up to one hour on the access token already
  issued. Ending that needs a revocation check on the bearer path, which is a token
  version or a deny list, and is not in this change.
- Bad: the browser half of the same question is still open — a reset does not end a
  session already open in a browser.
- Bad, accepted: the `purpose` claim is the only thing that separates the two capabilities
  an issuer key holds, so **a stolen issuer signing key now also buys revocation** — an
  availability attack, repeatedly ending the token access of the people it can name, on
  every instance that trusts the key. Before this change the same key bought
  impersonation only. Confidentiality is unchanged, and a key that can sign a person in
  can already do worse than sign them out. No guard is cheap enough to be worth it: a
  second key per purpose doubles the key list and its rotation, and a per-instance
  secret is option 2, rejected above. Two things bound it. Reach by identity only means
  the thief must know each person's opaque `door` subject; an email is not enough. And
  the remedy is the one a key compromise already needs — remove the key from the
  published list, which ends both capabilities at once.
- Bad, accepted: **no rate limit on the route**, matching `/auth/assert`, which has none
  either (core has no per-route limiter; the only throttle on the assertion path is the
  key-list fetch backoff). Only a caller holding a valid, single-use, 60-second assertion
  signed by the issuer gets past 401, so the one caller that can drive the route hard is
  the issuer itself. A limiter for both assertion routes is a separate decision.

## Where the Code Corrects Bead `wm-fcpzx`

The bead's verification was done against `feature/wm-63gg0-auth-assert` (`534ca13b`).
`v3` has since moved on, and the bead is left as written, so the true picture is stated
here. Its line numbers are stale, and four things differ:

- **The gap is wider than the bead says.** There are two kinds of refresh token in
  `oauth_refresh_tokens`, not one: a connector's, and a native app session's under the
  reserved client id `weos-native` (`wm-lnimb`, PR 569). A native sign-in through the door
  hands the app a refresh token that `POST /api/auth/refresh` renews, so a reset left the
  intruder's *app session* renewing as well as their connector.
- **A per-person revoke already existed, but per client.** `RevokeForAgent(ctx, agentID,
  clientID)` cannot express "every client", which a reset needs, so this change adds
  `RevokeAllForAgent` beside it, reusing its loop that repeats until no token of the set is
  active (so a concurrent rotation cannot slip one through).
- **"No operator lever exists today" needs one qualification.** `POST /api/auth/logout`
  `{"everywhere":true}` does end every native session of a person — but only for a caller
  holding that person's own live credential, and never a connector's tokens. The person has
  a lever; the door has none. The bead's conclusion stands; its wording overstates the
  absence.
- **`door` is now an accepted provider key, and owner binding runs with or without
  `OAUTH_ALLOWED_EMAILS`** (`wm-x0l4m`, `wm-6lx6z`). That is what makes "a door sign-in
  always leaves a `door` credential on the person it signed in" true, which is what lets
  the revocation reach by identity alone.

Re-verified on `v3` and still true: a refresh token is an opaque string stored as a SHA-256
hash; `handleRefreshGrant` reads `JWT_SIGNING_KEY` nowhere, so rotating that key is not an
eviction; `Create` defaults expiry to 30 days and every rotation grants a fresh 30;
`SESSION_SECRET` rotation does not reach the bearer path.

## More Information

Downstream consumer: the mini-me Money front door (`wm-eb4mv`), which calls this at the
end of a successful password reset (bead `wm-a8yyh`, story `wm-eb4mv.10`) with an
assertion it signs under the same key it signs login assertions with, naming the person's
`door` identity (provider `door`, the door's own subject for them). It needs a `v3` tag before the door can pin
it.

See [Trusted-Issuer Login Assertion]({% link decisions/trusted-issuer-login-assertion.md %})
for the assertion this reuses.
