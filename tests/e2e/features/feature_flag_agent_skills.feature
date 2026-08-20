@epic-480 @story-485
Feature: A disabled agent skill is never offered to the in-app agent
  As someone talking to a WeOS app's agent
  I want the skills for a capability I do not have to be absent from the agent's routing,
  and refused if named anyway
  So that the agent never walks me into something the instance will not do for me

  #484 gated the tools. This story gates the layer above them: the skills the coordinator
  routes to. The two are the same mechanism pointed at a different surface, and this
  contract is written to keep them the same mechanism — the gate a skill names is answered
  by the same `application.ToolFeatureGate`, over the same OpenFeature client, out of the
  same per-session resolved set. Where this contract diverges from #484 it says so out
  loud, and there is exactly one place it does.

  So nothing below re-derives resolution. Which layer beats which, that an explicit off is
  final downward, that a store failure answers off, that a change reaches sessions already
  open without signing anybody out, that a validity window is read at resolution rather
  than swept by a job — all of that is feature_flag_resolution.feature and
  feature_flag_grants.feature and stays there. What is new here is what the SKILL surface
  does with the answer: which sub-agents the coordinator is given for a turn, what the
  direct-invocation door does when the caller names a skill their features do not reach,
  and what a turn costs.

  Two gates again, and again they are not equally important. Filtering the orchestrator
  graph is what stops `transfer_to_agent` reaching a skill: the coordinator cannot transfer
  to a sub-agent it was never given, so a hidden skill cannot be routed to, cannot be
  described to the user, and cannot be talked about as though it existed. Refusing direct
  invocation — the `?skill=<name>` door #419 shipped — is the control. A client that names a
  skill it saw last week, or a model that emits `transfer_to_agent` for a name it invented,
  must not reach further than a caller whose features say no. Every scenario below that
  hides a skill therefore also names it directly, because an implementation that filters
  the graph and forgets the direct path has built an affordance and called it a control,
  and the difference stays invisible until somebody types the name.

  Where the filter runs matters, and the obvious place is wrong. `SkillRegistry` caches one
  set of definitions for the whole instance, loaded with the caller's identity deliberately
  stripped (`auth.ContextWithAgent(ctx, nil)`), because a skill is an installed capability
  and not per-user data. Filtering inside that cache would resolve one caller's features and
  then serve the result to everybody until the next skill event — the second person to take
  a turn would get the first person's answer. The filter therefore belongs on the way out,
  where the definitions are handed to the turn being built: `Orchestrator.buildRoot` and
  `buildSkillRoot` already run once per turn against the caller's context, which is exactly
  the seam the criterion's "per conversation, not once at boot" is asking for. Two scenarios
  below prove the property from the outside rather than naming the seam: two people in one
  process get two different graphs, and a feature turned on after boot is routable without a
  restart.

  Cost is the reason this can be done per turn at all, and it is pinned as a measurement
  rather than an intention, the way #484 pinned "a turn that calls thirty tools resolves
  once". Building a graph of twenty gated skills must read feature state no more times than
  building a graph of none — the filter asks the resolved set that the session already
  holds, and the set is resolved once. A second turn in the same session reads nothing at
  all. An implementation that evaluates per skill still passes every gating scenario above
  and fails these two, which is the point of writing them.

  Now the one deliberate difference, and it is the question this contract most wants ruled
  on.

  #485's fifth criterion says a skill gated on a feature nobody registered is treated as
  OFF and the mismatch logged, so that a typo cannot silently expose a skill. #484 settled
  the opposite for tools: an undeclared key leaves the tool exactly where it was and logs
  once, because closing every gate whose key does not resolve turns one mistyped constant
  into a silent capability outage across an instance, with a surface that looks deliberate.
  Both readings are defensible and they cannot both be the rule, because both surfaces call
  the same gate function and that function has one default.

  This contract pins #484's rule: **a skill gated on an undeclared key stays offered, and
  the drift is logged once, naming the skill and the key.** Three reasons, in the order they
  carry weight.

  First, the failure mode the criterion is guarding against is quieter for skills than for
  tools, not louder. A tool closed by drift still answers a caller who calls it — the
  refusal is a result the model reads and the user sees. A skill closed by drift produces no
  event at all: the coordinator simply never routes there, the user is told the agent cannot
  help, and nothing anywhere says why. Closing on drift makes the typo invisible on exactly
  the surface where invisibility is worst.

  Second, the risk of leaving a drifted skill offered is bounded by #484 and is smaller than
  it looks. A skill's power is its tools, and every gated tool is gated at its own call site
  against the same caller — the toolset the skill runs with is opened per turn from the
  caller's context. So a skill exposed by drift can be transferred to, and then cannot
  execute a single capability the caller's features do not already reach. What leaks is a
  description and an instruction block, not a capability. A scenario below asserts exactly
  that, because the reasoning is only sound if it is true.

  Third, one gate function with one default is worth protecting. #486 adds a third consumer
  and there will be more; a second gate whose default is the other way round is a coin flip
  at every new call site, and the two will drift the first time somebody copies the wrong
  one.

  Against all that stands the honest counter-argument, which is that a skill's gate key is
  authored in DATA — an agent-skill resource created through the API, MCP, or a seeding
  flow — while a tool's gate key is a constant in code that a compiler and a reviewer both
  see. The population that can typo a skill gate is much wider than the population that can
  typo a tool gate. If that asymmetry outweighs the three reasons above, the rule flips, and
  flipping it is mechanical: exactly two scenarios change, "A gate naming a key nobody
  declared leaves the skill where it was, and says so once" and "A skill exposed by drift
  still cannot run a gated tool", and the fix is to close the gate and drop the second. This
  contract does not hedge between them; it pins one and marks the seam.

  Either way the mismatch is loud. The instance logs it once per key with the skill's name
  beside it, so "why is that skill still there" and "why did that skill vanish" both have an
  answer without a debugger — which is the half of the criterion neither reading disputes.

  What is out of scope is worth stating because it is the next thing somebody will build.
  Gating decides what the AGENT is offered. It does not decide who can see, list or edit the
  agent-skill resource: `resource_list` for type "agent-skill" returns what it returns, an
  admin can still open a gated-off skill and fix its instructions, and the admin surface's
  own gating is #486. A gate that also hid the resource would make a disabled capability
  unmaintainable by the person whose job is to enable it.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the "agents" preset is installed
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And these agent skills are installed, each naming the feature that gates it:
      | skill                       | gated by        |
      | knowledge_graph_researcher  |                 |
      | episode_summarizer          | episodic-recall |
      | ledger_reporter             | ledger-export   |
    And the tool "ledger_export" is gated by the feature "ledger-export"
    And the in-app agent is configured with a scripted model
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- A skill that names no feature is untouched ---

  Scenario: A skill that names no feature is offered and invocable however the features stand
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    Then the coordinator was offered the skill "knowledge_graph_researcher"
    And invoking the skill "knowledge_graph_researcher" directly succeeds
    And no feature was evaluated for that skill

  Scenario: A gated skill that is on is offered and runs exactly as it did before it had a gate
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    Then the coordinator was offered the skill "episode_summarizer"
    And "episode_summarizer" is offered with the description it declares
    And "episode_summarizer" runs with the tools its allowlist declares
    And invoking the skill "episode_summarizer" directly succeeds

  # --- What the coordinator is offered ---

  Scenario Outline: A gated skill is offered as a routing target only when its feature is on for the caller
    Given <precondition>
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    Then the coordinator <presence> the skill "<skill>"

    Examples:
      | precondition                                                               | skill              | presence        |
      | nothing has been overridden or granted on this instance                    | episode_summarizer | was offered     |
      | nothing has been overridden or granted on this instance                    | ledger_reporter    | was not offered |
      | the operator has turned the feature "episodic-recall" off for the instance | episode_summarizer | was not offered |
      | "Harbor Legal" has turned the feature "ledger-export" on                   | ledger_reporter    | was offered     |

  Scenario: Two people on one instance are given two different graphs
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    When each of them takes a turn with the in-app agent
    Then "counsel@harborlegal.example" was offered "ledger_reporter"
    And "ops@harborlegal.example" was not offered "ledger_reporter"
    And both of them were offered "knowledge_graph_researcher"
    And the instance was not restarted between the two turns

  Scenario: One person acting in two accounts is given two different graphs
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" also belongs to "Cedar Realty"
    And "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have taken a turn and been offered "ledger_reporter"
    When they act in "Cedar Realty" and take another turn
    Then the coordinator was not offered the skill "ledger_reporter"
    And the coordinator was offered the skill "knowledge_graph_researcher"

  Scenario: The graph and the direct door agree, in both directions
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And the operator has turned the feature "episodic-recall" off for the instance
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    And they invoke every gated skill the instance has installed
    Then every skill the coordinator was offered was invocable directly
    And every gated skill the coordinator was not offered was refused

  Scenario: A skill turned on after boot is routable without a restart
    Given the instance booted with the feature "ledger-export" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have taken a turn and not been offered "ledger_reporter"
    When the operator turns the feature "ledger-export" on for the instance
    And they take another turn in the same conversation
    Then the coordinator was offered the skill "ledger_reporter"
    And invoking the skill "ledger_reporter" directly succeeds
    And the instance was not restarted

  # --- The direct door is the gate ---

  Scenario: Invoking a skill whose feature is off is refused, and nothing it would have done was done
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they invoke the skill "ledger_reporter" directly
    Then the invocation is refused with an error the client can read
    And the refusal names the skill "ledger_reporter"
    And the refusal says the capability is not enabled for them
    And the skill's instructions never ran
    And no ledger export was recorded
    And the refusal is not a partial reply

  Scenario: A refusal for a gated skill reads differently from a skill that does not exist
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they invoke the skill "ledger_reporter" directly
    And they invoke the skill "ledgr_reporter" directly
    Then the refusal for "ledger_reporter" says the capability is not enabled for them
    And the refusal for "ledgr_reporter" says no such skill exists
    And neither refusal ran a skill

  Scenario: A model that names a hidden skill anyway does not reach it
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And the scripted model transfers to "ledger_reporter" whatever it is offered
    When they take a turn with the in-app agent
    Then the coordinator was not offered the skill "ledger_reporter"
    And the turn did not run "ledger_reporter"
    And the user was given a reply rather than a broken turn

  Scenario: A skill name held from before a change is not authorization to invoke it
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And they have taken a turn and been offered "ledger_reporter"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" invokes the skill "ledger_reporter" directly
    Then the invocation is refused
    And "counsel@harborlegal.example" is still signed in
    And they were not signed out

  Scenario: Taking another turn after a change reflects the truth, with no new conversation and no restart
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And they have taken a turn and been offered "ledger_reporter"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example"
    And "counsel@harborlegal.example" takes another turn in the same conversation
    Then the coordinator was not offered the skill "ledger_reporter"
    And the conversation kept its history
    And the instance was not restarted

  Scenario: A grant that runs out mid-conversation stops the routing, though nothing announced it
    Given the instance is configured with a maximum cache age of 10 minutes
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" holds a grant of "ledger-export" valid until 2 seconds from now
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn straight away
    And they invoke the skill "ledger_reporter" directly once that moment has passed
    And they take another turn in the same conversation
    Then the turn straight away was offered "ledger_reporter"
    And the invocation after that moment is refused
    And the later turn was not offered "ledger_reporter"
    And nothing invalidated the session in between
    And the maximum cache age had not run out
    And "counsel@harborlegal.example" is still signed in

  # --- Failing closed ---

  Scenario: When the store cannot be read the gated skills go, and the rest stay
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    Then the coordinator was not offered the skill "episode_summarizer"
    And the coordinator was offered the skill "knowledge_graph_researcher"
    And invoking the skill "episode_summarizer" directly is refused
    And invoking the skill "knowledge_graph_researcher" directly succeeds
    And the instance logged the failure

  Scenario: A store failure is not remembered as an answer
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have taken a turn and not been offered "episode_summarizer"
    When the store can be read again
    And they take another turn in the same conversation
    Then the coordinator was offered the skill "episode_summarizer"
    And invoking the skill "episode_summarizer" directly succeeds

  # --- A gate key nobody declared (see the preamble: this is the contested rule) ---

  Scenario: A gate naming a key nobody declared leaves the skill where it was, and says so once
    Given the skill "ledger_reporter" is gated by the undeclared feature "ledgr-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take 20 turns with the in-app agent
    Then the coordinator was offered the skill "ledger_reporter"
    And invoking the skill "ledger_reporter" directly succeeds
    And the instance logged the undeclared feature key once
    And the log names the key "ledgr-export"
    And the log names the skill "ledger_reporter"

  Scenario: A skill exposed by drift still cannot run a gated tool
    Given the skill "ledger_reporter" is gated by the undeclared feature "ledgr-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they invoke the skill "ledger_reporter" directly
    And the skill calls the tool "ledger_export"
    Then the skill was reached
    And the tool call is refused because the capability is not enabled
    And no ledger export was recorded

  # --- What the gate costs ---

  Scenario: Building a graph of twenty gated skills resolves once
    Given the instance has 20 skills gated by "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they take a turn with the in-app agent
    Then the turn was answered
    And feature state was read from the database only while that turn was answered
    And the graph was built without reading feature state once per skill

  Scenario: A second turn in the same session reads no feature state at all
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already taken a turn with the in-app agent
    When they take another turn in the same conversation
    And they invoke the skill "episode_summarizer" directly
    Then both turns were answered
    And no feature state was read from the database after the first turn

  Scenario: The filter follows the caller, not the process
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    And "ops@harborlegal.example" has already taken a turn and not been offered "ledger_reporter"
    When "counsel@harborlegal.example" takes a turn with the in-app agent
    Then "counsel@harborlegal.example" was offered "ledger_reporter"
    And the skill registry loaded the definitions once for both of them

  # --- Routing, proved with the scripted agent model ---

  Scenario: The same prompt routes to the skill for a granted user and not for an ungranted one
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    And the scripted model routes "export last quarter's ledger" to "ledger_reporter"
    When each of them sends "export last quarter's ledger" to the in-app agent
    Then "counsel@harborlegal.example" was answered by the skill "ledger_reporter"
    And "ops@harborlegal.example" was not answered by any skill
    And "ops@harborlegal.example" was given a reply rather than a broken turn

  Scenario: The ungranted user's turn never mentions the skill it could not reach
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And the scripted model routes "export last quarter's ledger" to "ledger_reporter"
    When they send "export last quarter's ledger" to the in-app agent
    Then the reply does not name "ledger_reporter"
    And the turn ended with a reply the user can read

  Scenario: A grant made mid-conversation changes where the same prompt routes
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    And the scripted model routes "export last quarter's ledger" to "ledger_reporter"
    And they have sent "export last quarter's ledger" and not been answered by any skill
    When "ops@harborlegal.example" grants "ledger-export" to "counsel@harborlegal.example"
    And "counsel@harborlegal.example" sends "export last quarter's ledger" again in the same conversation
    Then they were answered by the skill "ledger_reporter"
    And they were not signed out
    And the instance was not restarted

  # --- A turn with nobody on the context ---

  Scenario Outline: With no caller on the context the instance layer alone decides which skills exist
    Given <precondition>
    And a turn taken with nobody on the context
    Then the coordinator <presence> the skill "<skill>"
    And invoking "<skill>" directly <outcome>

    Examples:
      | precondition                                                               | skill              | presence        | outcome    |
      | nothing has been overridden or granted on this instance                    | episode_summarizer | was offered     | succeeds   |
      | the operator has turned the feature "episodic-recall" off for the instance | episode_summarizer | was not offered | is refused |
      | the operator has turned the feature "ledger-export" on for the instance    | ledger_reporter    | was offered     | succeeds   |

  Scenario: With no caller on the context an account override and a personal grant reach nothing
    Given "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    And a turn taken with nobody on the context
    Then the coordinator was not offered the skill "ledger_reporter"
    And invoking "ledger_reporter" directly is refused
    And no account override or grant was read to answer either
