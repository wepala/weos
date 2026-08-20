@wip @epic-480 @story-484
Feature: A disabled MCP tool never reaches the LLM
  As someone whose LLM talks to a WeOS instance over MCP
  I want the tools for a capability I do not have to be absent from the list, and refused
  if called anyway
  So that the model cannot offer me something the instance will not do, and cannot be
  talked into calling it

  #481 built resolution, #482 gave an operator the switch, #483 gave an admin the grant.
  All three ended at an answer nobody acts on: a boolean, correct and cheap, with no call
  site consuming it. This story is the first consumer, and it is the one the epic was
  written for — an agent turn is where thirty evaluations happen, and the MCP tool surface
  is where "off" has to mean something a model can see.

  So nothing below re-derives resolution. Which layer beats which, that an explicit off is
  final downward, that a store failure answers off, that an undeclared key answers the
  caller's default and is logged once, that a change reaches sessions already open without
  signing anybody out, that a window is read at resolution rather than swept by a job — all
  of that is feature_flag_resolution.feature and feature_flag_grants.feature and stays
  there. What is new here is what the MCP surface does with the answer: what the tool list
  contains, what a call does when the list and the caller disagree, and that the in-app
  agent and a third-party client are shown the same set.

  Two gates, not one, and they are not equally important. A tool missing from `tools/list`
  is a convenience for the model: it cannot propose what it cannot see, so it cannot
  hallucinate a call, and the user is never offered a capability that will fail. A tool
  that refuses when called is the gate. Every scenario that hides a tool therefore also
  calls it, because an implementation that filters the listing and forgets the handler has
  built a UI affordance and called it a control, and the difference is invisible right up
  until a client calls a tool by name from a list it cached an hour ago.

  Which brings up the one thing this contract deliberately does not require: a
  `tools/list_changed` notification. The MCP protocol has one, the Go SDK can send one, and
  it is the obvious thing to reach for — and it cannot be the mechanism here, for a reason
  that is structural rather than a matter of effort. Grant expiry is lazy by design: #483
  settled that a validity window is read at resolution and that nothing fires when
  `validThrough` passes, because a row that has run out is simply not counted. So at the
  moment a window closes, there is no event to hang a notification on, and there never will
  be without a scheduler this epic explicitly does not want. A contract that demanded a
  notification whenever the resolved tool list changed would be unsatisfiable on the very
  case it most needs to cover. The scenarios below therefore pin the property that holds in
  every case: **a stale tool list is never authorization**. A client that still has the tool
  and calls it is refused, whether the change came from an operator's flip a second ago or
  from a window that closed on its own. A client that lists again gets the truth, with no
  reconnect and no restart. Whether an implementation additionally sends a notification on
  the changes it *can* see is left open — it would be a courtesy that saves a model one
  wasted call, and if it is added it must not become the thing anything relies on.

  The stdio transport has no caller, and that is a limit worth writing down rather than
  discovering. `weos mcp` is one local process with no session, no bearer token and no
  active account; `auth.AgentFromCtx` is nil there and resolution reaches the instance
  layer and stops. So on stdio, gating is instance-wide and can be nothing else: an
  operator's instance-level off hides and refuses the tool, and an account override or a
  personal grant makes no difference at all. Two scenarios below say so out loud, because
  stdio is mini-me's primary surface and somebody will otherwise assume the per-user
  behaviour these scenarios prove over HTTP applies there too. It does not, and cannot,
  until the local transport carries an identity — which is a different story than this one.

  Where the gate is declared is the same place the tool is: at its `mcp.AddTool` call site,
  beside the name, the schema and the annotations, because a tool declared in one file and
  gated in a registry somewhere else is two things that will drift. A tool that names no
  feature is ungated and nothing about it changes — no lookup, no resolver, no new failure
  mode — which is what the first scenarios below pin, against tools that ship today.

  A key nobody declared is not a store failure and the two must not be conflated. #481
  already settled the general rule and it is the epic's rule: an undeclared key is registry
  drift and answers the caller's default, logged once; a read that fails is closed. Applied
  here, that means a typo in a gate leaves the tool where it was and says so in the log,
  while a database that cannot be read takes the gated tools away and leaves the ungated
  ones alone. The alternative — hiding every tool whose gate key does not resolve — turns
  one mistyped constant into a silent capability outage across an instance, with a tool
  surface that looks deliberate.

  The cost assertion is the epic's own: a turn that calls thirty tools resolves once. It is
  pinned here rather than in #481 because this is the surface where the number thirty comes
  from, and because the per-call gate is exactly where a plausible implementation reaches
  for the resolver again instead of the set it already has.

  Two tools carry gates in these scenarios. `episodic_recall` is a real shipped tool gated
  at its own call site by the `episodic-recall` feature the other three contracts already
  declare, so the mechanism is exercised through the code path it actually ships in.
  `ledger_export` is registered by the harness through the configurer seam downstream
  binaries use, and it mutates: it exists so the contract can exercise a gate whose feature
  is declared off, and prove a refused call writes nothing, without this story having to
  decide which shipped tool ought to be dark by default.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And these tools declare the feature that gates them:
      | tool            | gated by        | mutates |
      | episodic_recall | episodic-recall | no      |
      | ledger_export   | ledger-export   | yes     |
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- A tool that names no feature is untouched ---

  Scenario: Tools that name no feature are listed and callable however the features stand
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" is connected over MCP
    When they list the server's tools
    Then the listing includes "resource_get"
    And the listing includes "person_list"
    And calling "resource_get" succeeds

  Scenario: A gated tool that is on is listed and called exactly as it was before it had a gate
    Given "ops@harborlegal.example" is connected over MCP
    When they list the server's tools
    Then the listing includes "episodic_recall"
    And "episodic_recall" is advertised with the annotation it declares
    And "episodic_recall" is advertised with the input schema it declares
    And calling "episodic_recall" succeeds

  Scenario: A gate naming a key nobody declared leaves the tool where it was, and says so once
    Given the tool "ledger_export" is gated by the undeclared feature "shpping-labels"
    And "ops@harborlegal.example" is connected over MCP
    When they list the server's tools 20 times
    Then the listing includes "ledger_export"
    And calling "ledger_export" succeeds
    And the instance logged the undeclared feature key once
    And the log names the key "shpping-labels"

  # --- What the model is shown ---

  Scenario Outline: A gated tool is listed only when its feature is on for the caller
    Given <precondition>
    And "ops@harborlegal.example" is connected over MCP
    When they list the server's tools
    Then the listing <presence> "<tool>"

    Examples:
      | precondition                                                               | tool            | presence |
      | nothing has been overridden or granted on this instance                    | episodic_recall | includes |
      | nothing has been overridden or granted on this instance                    | ledger_export   | omits    |
      | the operator has turned the feature "episodic-recall" off for the instance | episodic_recall | omits    |
      | "Harbor Legal" has turned the feature "ledger-export" on                   | ledger_export   | includes |

  Scenario: Two people on one instance are shown two different tool lists
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are connected over MCP, each in their own session
    When each of them lists the server's tools
    Then "counsel@harborlegal.example" is shown "ledger_export"
    And "ops@harborlegal.example" is not shown "ledger_export"
    And both of them are shown "resource_get"

  Scenario: One person acting in two accounts is shown two different tool lists
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" also belongs to "Cedar Realty"
    And "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" is connected over MCP acting in "Harbor Legal"
    And they have listed the server's tools and been shown "ledger_export"
    When they act in "Cedar Realty" and list the server's tools again
    Then the listing omits "ledger_export"
    And the listing includes "resource_get"

  Scenario: The list and the call agree, in both directions
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And the operator has turned the feature "episodic-recall" off for the instance
    And "counsel@harborlegal.example" is connected over MCP
    When they list the server's tools
    And they call every gated tool the instance declares
    Then every tool the listing showed them was callable
    And every gated tool the listing withheld was refused

  # --- The call is the gate ---

  Scenario: Calling a tool whose feature is off is refused, and nothing it would have done was done
    Given "ops@harborlegal.example" is connected over MCP
    When they call "ledger_export"
    Then the call is refused with an error the client can read
    And the refusal names the tool "ledger_export"
    And the refusal says the capability is not enabled for them
    And no ledger export was recorded
    And the refusal is not a partial result

  Scenario: A tool list fetched before the change is not authorization to call what is on it
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is connected over MCP
    And they have listed the server's tools and been shown "ledger_export"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" calls "ledger_export" from the list they already hold
    Then the call is refused
    And "counsel@harborlegal.example" is still connected
    And they were not signed out

  Scenario: Listing again after a change returns the truth, with no reconnect and no restart
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is connected over MCP
    And they have listed the server's tools and been shown "ledger_export"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" lists the server's tools again in the session they already held
    Then the listing omits "ledger_export"
    And the MCP session was not reconnected
    And the instance was not restarted

  Scenario: A grant that runs out mid-session refuses the call, though nothing announced it
    Given the instance is configured with a maximum cache age of 10 minutes
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" holds a grant of "ledger-export" valid until 2 seconds from now
    And "counsel@harborlegal.example" is connected over MCP
    And they have listed the server's tools and been shown "ledger_export"
    When they call "ledger_export" straight away
    And they call "ledger_export" again once that moment has passed
    And they list the server's tools again
    Then the call straight away succeeded
    And the call after that moment is refused
    And the listing now omits "ledger_export"
    And nothing invalidated the session in between
    And the maximum cache age had not run out
    And "counsel@harborlegal.example" is still connected

  # --- Failing closed ---

  Scenario: When the store cannot be read the gated tools go, and the rest stay
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is connected over MCP
    When they list the server's tools
    Then the listing omits "episodic_recall"
    And the listing includes "resource_get"
    And calling "episodic_recall" is refused
    And calling "resource_get" succeeds
    And the instance logged the failure

  Scenario: A store failure is not remembered as an answer
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is connected over MCP
    And they have listed the server's tools and not been shown "episodic_recall"
    When the store can be read again
    And they list the server's tools again in the session they already held
    Then the listing includes "episodic_recall"
    And calling "episodic_recall" succeeds

  # --- What the gate costs ---

  Scenario: A turn that lists once and calls thirty tools resolves once
    Given "ops@harborlegal.example" is connected over MCP
    When they list the server's tools
    And they make 30 tool calls in that session
    Then every call was answered
    And feature state was read from the database only while the listing was answered

  # --- The in-app agent is shown the same set ---

  Scenario: The in-app agent's tools for a turn are the ones the same person sees over MCP
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And the operator has turned the feature "episodic-recall" off for the instance
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When their in-app agent opens a toolset for a turn
    And they list the server's tools over MCP in the same session
    Then the toolset holds exactly the tools the MCP listing showed
    And the toolset holds "ledger_export"
    And the toolset does not hold "episodic_recall"

  Scenario: Two people talking to the in-app agent get two different toolsets
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    When each of them takes a turn with the in-app agent
    Then "counsel@harborlegal.example" was offered "ledger_export"
    And "ops@harborlegal.example" was not offered "ledger_export"

  Scenario: A tool turned on after boot is usable by the in-app agent without a restart
    Given the instance booted with the feature "ledger-export" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When the operator turns the feature "ledger-export" on for the instance
    And "ops@harborlegal.example" takes a turn with the in-app agent
    Then the toolset holds "ledger_export"
    And the agent's call to "ledger_export" succeeds
    And the instance was not restarted

  # --- The local stdio transport has no caller ---

  Scenario Outline: On stdio the instance layer alone decides which gated tools exist
    Given <precondition>
    And an MCP client attached to the store over the local stdio transport
    When it lists the server's tools
    Then the listing <presence> "<tool>"
    And calling "<tool>" <outcome>

    Examples:
      | precondition                                                               | tool            | presence | outcome     |
      | nothing has been overridden or granted on this instance                    | episodic_recall | includes | succeeds    |
      | the operator has turned the feature "episodic-recall" off for the instance | episodic_recall | omits    | is refused  |
      | the operator has turned the feature "ledger-export" on for the instance    | ledger_export   | includes | succeeds    |

  Scenario: On stdio an account override and a personal grant reach nothing
    Given "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    And an MCP client attached to the store over the local stdio transport
    When it lists the server's tools
    Then the listing omits "ledger_export"
    And calling "ledger_export" is refused
    And no account override or grant was read to answer either
