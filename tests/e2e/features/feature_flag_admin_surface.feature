@epic-480 @story-486 @task-487
Feature: The API answers what a caller's features are, and refuses what they are not
  As someone using a WeOS admin
  I want one answer for which capabilities I hold, and the server to hold to that answer
  when I go around the UI
  So that what I am shown and what I am allowed are the same thing

  This is the epic's last story and its third consumer. #484 gated the MCP tool surface,
  #485 gated the agent skills above it, and both ended at a surface a model talks to. This
  one ends at a surface a person clicks, which is why it needs something the other two did
  not: an endpoint that tells a browser what the answer is, so a sidebar can be drawn from
  it.

  Nothing below re-derives resolution. Which layer beats which, that an explicit off is
  final downward, that a store failure answers off, that an anonymous caller resolves from
  the instance layer alone, that a validity window is read at resolution rather than swept
  by a job, that a change reaches sessions already open without signing anybody out — all
  of that is feature_flag_resolution.feature and feature_flag_grants.feature and stays
  there. What is new here is the HTTP surface: what the listing endpoint answers and to
  whom, and what a gated route does when a caller reaches it directly.

  This file is one half of the story. The other half — what the admin SPA draws from the
  answer — is web/admin/e2e/features/feature-gated-navigation.feature, in the SPA's own
  playwright-bdd suite, because that suite is the only one that can open a page. The split
  is not cosmetic and it is not negotiable: a scenario about a hidden sidebar entry written
  into this file cannot run, and a scenario about a 403 written into that file proves
  nothing, because that suite scripts its own API responses with page.route. So the two
  files are deliberately joined at the seam — every hiding scenario over there names a
  refusal scenario over here, and this preamble and that one both say so. Read either one
  alone and you are reading half a control.

  # --- The endpoint already exists, and this story does not replace it ---

  #482 shipped GET /api/features. It is registered under the protected group, it answers
  the full status of every declared feature — key, display name, description, resolved
  value, the layer that decided it, and whether an account may manage it or a grant may
  reach it — and three committed scenarios in feature_flag_operator_switch.feature depend
  on that shape: the listing reports where a value came from, it names each feature by its
  display name, and an ordinary member may read it while being refused every write.

  #487 asks for "{key, enabled} per registered feature" and for an unauthenticated caller
  to be answered 200 with instance defaults. Those two asks pull in opposite directions
  against what exists, so this contract rules on them rather than leaving the seam for an
  implementer to guess at.

  The ruling: **one endpoint, the shape it already has, moved so that a caller with no
  session is answered rather than refused.** `key` and `enabled` are two of the fields it
  already returns, so #487's shape criterion is met by a superset and no committed #482
  scenario changes. Shrinking to a bare pair would break all three of those scenarios and
  would take away exactly what the admin's own feature-administration screen needs to draw
  — the source, the display name, and whether the switch in front of the operator is one
  they may throw. A second, thinner endpoint is worse than either: two answers to one
  question, resolved by the same resolver, free to drift, with operators then having to be
  told which one to believe. The epic's anti-pattern list already refuses that shape once,
  for BehaviorMeta; it refuses it here for the same reason.

  Moving it out of the protected group costs two things, and both are pinned below because
  both are the kind of regression that ships quietly.

  The first is impersonation. The protected group applies impersonation middleware; an
  admin acting as somebody else is answered as that somebody else by every route in it.
  A listing that lost that middleware would answer with the real admin's features while
  every other call on the page answered with the impersonated user's — a sidebar drawn for
  one person over data belonging to another, which is worse than either alone. So the
  scenario below asks for the property, not the middleware: while impersonating, the
  listing is the impersonated person's.

  The second is route ordering. serve.go carries a warning at the mcpGroup registration
  saying that the last empty-prefix group under /api owns what an unmatched /api path
  answers, and that adding another one silently changes that answer for every unmounted
  path — including /api/auth/register when registration is off, whose parity is pinned by
  password_registration_flag.feature, in a test that builds its own echo instance and would
  therefore not notice. Making one route anonymous is exactly the change that trips this.
  A scenario below pins the parity directly, from the outside, so it is caught here even
  though the file that cares about it cannot catch it.

  # --- "The server still enforces" had nothing to enforce, and now it does ---

  The story's fourth criterion says hitting a gated route directly by URL is refused by the
  API and not merely hidden by the UI. As of this story starting, that criterion could not
  be met, and it is worth being plain about why rather than writing a scenario against
  something that does not exist. **No REST route on this instance is feature-gated.** #484
  gated MCP tools at their `mcp.AddTool` call sites. #485 gated agent skills as they are
  handed to a turn. Neither touched the HTTP API, and there is no route-level gate to reach
  for. An implementation could satisfy every other criterion of this story — endpoint,
  composable, hidden sidebar — and leave a URL bar that works perfectly well.

  So this story gates HTTP routes. That is a real addition to its scope and it should be
  seen as one. Two routes carry gates below, chosen the way #484 chose its two.

  `agent-chat` gates the in-app agent's REST surface — the three /api/agent/conversations
  routes the admin's chat page calls. It is the right shipped choice because it is the one
  sidebar entry that is a capability rather than an administrative screen: Users and
  Settings are gated by role, and a role is not a flag. It is declared on, for #484's
  reason — the chat ships today and an upgrade that introduced the gate dark would take a
  working page away from every instance. And it does not collide with #485: off means there
  is no assistant at all, on means there is one whose skill graph #485 filters. A scenario
  below pins that the two compose rather than overlap.

  `ledger-export` gates a mutating route the harness mounts through the PresetHTTPHandlers
  seam that presets and downstream binaries already use, exactly as #484's `ledger_export`
  tool is registered through the MCP configurer seam. It exists so the contract can prove a
  refused call writes nothing, against a feature that is declared off, without this story
  having to decide that some second shipped screen ought to be dark by default.

  # --- Hiding is presentation only, so every hide is paired ---

  A scenario that only checks a sidebar would pass against an implementation with no server
  gate at all. So the discipline #484 used for list-and-call and #485 used for graph-and-
  door applies here across the file boundary: every entry the SPA hides has a route this
  file refuses, and the outline below walks the layers proving the listing and the route
  never disagree in either direction. An answer the listing gave a minute ago is not
  authorization either — the stale-set scenarios are the same property #484 pinned for a
  cached tool list, and they matter more here, because a browser holds its answer for as
  long as the tab is open.

  # --- One contested ruling, marked ---

  When the store cannot be read, a gate answers off. That is settled. What a *listing*
  does was not, and today it answers 500. This contract changes it: **the listing answers
  200 with every feature off, carries a message saying the state could not be read, and
  logs the failure.**

  The reason is that a 500 forces the browser to invent a policy, and the two policies it
  can invent are "show everything" and "show nothing", of which one is wrong and the other
  is indistinguishable from the answer the server would have given anyway. Answering
  all-off makes the listing agree with the gates during the outage instead of leaving the
  UI to guess, and the message is what stops an operator staring at an empty sidebar with
  nothing to read.

  The honest counter-argument is that all-off is a lie: the store being unreadable is not
  the same fact as the features being off, and a client cannot tell a real all-off instance
  from a broken one except by reading a message it may not surface. If that outweighs the
  above, the rule flips to a 5xx and exactly two scenarios change — "When the store cannot
  be read the listing answers off rather than failing" and its SPA counterpart. This
  contract does not hedge; it pins one and marks the seam.

  # --- Out of scope ---

  Gating decides what a caller is offered. It does not decide what an operator may
  administer: a feature turned off for somebody is still listed, still visible on the
  feature-administration screen, and still turn-on-able, or the person whose job is to
  enable it cannot reach it. #485 made the same point for skills and a scenario below
  makes it for this surface. Role-based access is not touched and must not be confused
  with this: a member refused a write is refused for their role, whatever their features
  say, and a scenario pins that the two mechanisms answer separately.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | agent-chat      | Assistant       | Talk to this instance's assistant        | on      | yes        | yes       |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And these API routes declare the feature that gates them:
      | route                                          | gated by      | mutates |
      | POST /api/agent/conversations/{id}/messages    | agent-chat    | yes     |
      | GET /api/agent/conversations/{id}              | agent-chat    | no      |
      | POST /api/ledger/exports                       | ledger-export | yes     |
    And these API routes name no feature:
      | route                   |
      | GET /api/persons        |
      | GET /api/resource-types |
      | GET /api/features       |
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- What the listing answers, and to whom ---

  Scenario: A signed-in caller is answered every declared feature, resolved for them
    Given the operator has turned the feature "ledger-export" on for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    Then the answer is served
    And the answer carries every feature the instance declares
    And the answer reports "agent-chat" as on
    And the answer reports "ledger-export" as on
    And each feature carries its key and whether it is enabled
    And each feature carries its display name and the layer that decided it
    And the answer carries no grant belonging to anybody

  Scenario: A caller carrying no session is answered the instance view rather than refused
    Given the operator has turned the feature "agent-chat" off for the instance
    When a request carrying no session asks the API for the feature set
    Then the answer is served
    And the answer reports "agent-chat" as off
    And the answer reports "ledger-export" as off
    And the answer reports "episodic-recall" as on
    And no account override or grant was read to answer it

  Scenario: The anonymous answer cannot report a layer an anonymous caller does not have
    Given "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    When a request carrying no session asks the API for the feature set
    Then the answer reports "ledger-export" as off
    And no feature in the answer names an account override as its layer
    And no feature in the answer names a grant as its layer
    And the answer names no person and no account

  Scenario: Two people signed in to one account are answered differently
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    When each of them asks the API for their feature set
    Then "counsel@harborlegal.example" is answered "ledger-export" on
    And "ops@harborlegal.example" is answered "ledger-export" off
    And both of them are answered "agent-chat" on

  Scenario: One person acting in two accounts is answered differently
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" also belongs to "Cedar Realty"
    And "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" on
    When they act in "Cedar Realty" and ask for their feature set again
    Then they are answered "ledger-export" off
    And they are answered "agent-chat" on
    And they were not signed out

  Scenario: While impersonating, the listing is the impersonated person's
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" off
    When "ops@harborlegal.example" impersonates "counsel@harborlegal.example"
    And they ask for their feature set again
    Then they are answered "ledger-export" on
    And they are answered the same set "counsel@harborlegal.example" is answered
    When they stop impersonating
    And they ask for their feature set again
    Then they are answered "ledger-export" off

  Scenario: Making the listing readable without a session made nothing else readable without one
    When a request carrying no session asks the API for the feature set
    And a request carrying no session asks the API for the person listing
    And a request carrying no session tries to turn the feature "agent-chat" off for the instance
    And a request carrying no session asks the API for a path nobody mounted
    Then the feature set was served
    And the person listing is refused as not authenticated
    And the attempt to change the instance is refused as not authenticated
    And no instance-level setting is stored for "agent-chat"
    And the path nobody mounted answers exactly what an unmounted path answered before this change

  Scenario: An ordinary member reads the listing and is still refused every write
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    And they try to turn the feature "agent-chat" off for the instance
    Then the feature set was served
    And the attempt to change it is refused as forbidden

  # --- The listing and the gate never disagree ---

  Scenario Outline: What the listing says about a feature is what its routes do
    Given <precondition>
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    And they call "<route>"
    Then the answer reports "<feature>" as <state>
    And the call <outcome>

    Examples:
      | precondition                                                            | feature       | state | route                     | outcome    |
      | nothing has been overridden or granted on this instance                 | agent-chat    | on    | POST /api/agent/conversations/{id}/messages  | succeeds   |
      | nothing has been overridden or granted on this instance                 | ledger-export | off   | POST /api/ledger/exports  | is refused |
      | the operator has turned the feature "agent-chat" off for the instance   | agent-chat    | off   | POST /api/agent/conversations/{id}/messages  | is refused |
      | the operator has turned the feature "ledger-export" on for the instance | ledger-export | on    | POST /api/ledger/exports  | succeeds   |
      | "Harbor Legal" has turned the feature "ledger-export" on                | ledger-export | on    | POST /api/ledger/exports  | succeeds   |

  Scenario: The listing and the routes agree, in both directions
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And the operator has turned the feature "agent-chat" off for the instance
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    And they call every gated route the instance mounts
    Then every route whose feature the answer reported on was served
    And every route whose feature the answer reported off was refused

  Scenario: A caller with no session is refused the routes their answer said were off
    Given the operator has turned the feature "agent-chat" off for the instance
    When a request carrying no session asks the API for the feature set
    And a request carrying no session calls "POST /api/agent/conversations/{id}/messages"
    Then the answer reported "agent-chat" as off
    And the call is refused

  # --- The route is the gate ---

  Scenario: A gated route called with the feature off is refused, and nothing it would have done was done
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call "POST /api/ledger/exports"
    Then the call is refused as forbidden
    And the refusal names the feature "ledger-export"
    And the refusal says the capability is not enabled for them
    And no ledger export was recorded
    And the refusal is not a partial result

  Scenario: A refusal for a gated route reads differently from a refusal for a role
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    And the operator has turned the feature "ledger-export" on for the instance
    When they call "POST /api/ledger/exports"
    And they try to turn the feature "agent-chat" off for the instance
    Then the ledger call is served
    And the attempt to change the instance is refused as forbidden
    And the refusal names their role rather than a feature

  Scenario: A role that permits the call does not survive the feature being off
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And the operator has turned the feature "agent-chat" off for the instance
    When they call "POST /api/agent/conversations/{id}/messages"
    Then the call is refused as forbidden
    And the refusal names the feature "agent-chat"
    And the refusal does not say their role is insufficient

  Scenario: Routes that name no feature are untouched however the features stand
    Given the operator has turned the feature "agent-chat" off for the instance
    And the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call "GET /api/persons"
    And they call "GET /api/resource-types"
    Then both calls succeed
    And no feature was evaluated for either route

  Scenario: An answer held from before a change is not authorization to call the route
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" on
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" calls "POST /api/ledger/exports" on the answer they already hold
    Then the call is refused
    And no ledger export was recorded
    And "counsel@harborlegal.example" is still signed in
    And they were not signed out

  Scenario: Asking again after a change returns the truth, with no new session and no restart
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" on
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" asks for their feature set again in the session they already held
    Then they are answered "ledger-export" off
    And they were not signed out
    And the instance was not restarted

  Scenario: A grant that runs out mid-session closes the route, though nothing announced it
    Given the instance is configured with a maximum cache age of 10 minutes
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" holds a grant of "ledger-export" valid until 2 seconds from now
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" on
    When they call "POST /api/ledger/exports" straight away
    And they call "POST /api/ledger/exports" again once that moment has passed
    And they ask for their feature set again
    Then the call straight away succeeded
    And the call after that moment is refused
    And they are now answered "ledger-export" off
    And nothing invalidated the session in between
    And the maximum cache age had not run out
    And "counsel@harborlegal.example" is still signed in

  Scenario: A feature turned on after boot opens its route without a restart
    Given the instance booted with the feature "ledger-export" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "ledger-export" off
    When the operator turns the feature "ledger-export" on for the instance
    And they ask for their feature set again in the session they already held
    And they call "POST /api/ledger/exports"
    Then they are answered "ledger-export" on
    And the call succeeds
    And the instance was not restarted

  # --- Failing closed ---

  Scenario: When the store cannot be read the gated routes close, and the rest stay open
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call "POST /api/agent/conversations/{id}/messages"
    And they call "GET /api/persons"
    Then the call to the gated route is refused
    And the call to the ungated route succeeds
    And the instance logged the failure

  Scenario: When the store cannot be read the listing answers off rather than failing
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    Then the answer is served
    And every feature in the answer is reported off
    And the answer carries a message saying the feature state could not be read
    And the instance logged the failure

  Scenario: A store failure is not remembered as an answer
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have asked for their feature set and been answered "agent-chat" off
    When the store can be read again
    And they ask for their feature set again in the session they already held
    Then they are answered "agent-chat" on
    And calling "POST /api/agent/conversations/{id}/messages" succeeds

  Scenario: A route gated on a key nobody declared stays where it was, and says so once
    Given the route "POST /api/ledger/exports" is gated by the undeclared feature "ledgr-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call "POST /api/ledger/exports" 20 times
    Then every call succeeds
    And the instance logged the undeclared feature key once
    And the log names the key "ledgr-export"
    And the log names the route

  # --- Gating the surface is not gating its administration ---

  Scenario: A feature that is off for somebody is still on the operator's listing, and still turn-on-able
    Given the operator has turned the feature "agent-chat" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set
    And they turn the feature "agent-chat" on for the instance through the API
    Then the answer had carried "agent-chat" reported as off
    And the change was accepted
    And they are now answered "agent-chat" on

  Scenario: A gated-off assistant is not a filtered assistant
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And the operator has turned the feature "agent-chat" off for the instance
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they call "POST /api/agent/conversations/{id}/messages"
    Then the call is refused as forbidden
    And the refusal names the feature "agent-chat"
    And no turn was taken
    And no skill graph was built

  # --- What it costs ---

  Scenario: A page load that reads the listing and then calls five routes resolves once
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And the operator has turned the feature "ledger-export" on for the instance
    When they ask the API for their feature set
    And they make 5 calls to gated routes in that session
    Then every call was answered
    And feature state was read from the database only while the listing was answered

  Scenario: Twenty listings in one session read the database once
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for their feature set 20 times
    Then every answer was the same
    And feature state was read from the database only while the first answer was built
