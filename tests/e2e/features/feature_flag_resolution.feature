@epic-480 @story-481
Feature: A feature is declared once and resolved through OpenFeature
  As an operator running a WeOS instance
  I want one answer to "is this capability on for this caller?"
  So that unfinished work ships dark and paid capabilities reach only the right people

  A feature is declared in code, or by a preset, and the declaration is the whole of what
  the instance knows about it: a key, a name and description an operator can read, the
  value it takes when nothing else has spoken, whether an account admin may change it,
  and whether it may be granted to one person. Every call site asks the same question
  through the OpenFeature client, under the "feature." key prefix, so that a remote flag
  source can be layered in later without a single call site changing.

  Three layers can speak, in this order: the instance, then the account the caller is
  acting in, then a grant the caller holds directly or through their role. The rule these
  scenarios pin is that an explicit "off" at any layer is final for every layer below it.
  An operator who turns a feature off for the instance has turned it off for everyone,
  and no account override and no personal grant reaches past that; an account that turns
  it off has turned it off for its members, whatever they were granted. Anything a layer
  says nothing about passes through untouched, so a feature nobody has touched answers
  its declared default, and a feature declared off can still be turned on by the account
  or by a grant. That last point is the one worth stating out loud, because "first off
  wins" read strictly would make every feature declared off unreachable forever, which
  would leave the declaration's own `default` field with only one usable value. The
  scenarios below therefore separate the declared default from an operator's explicit
  instance-level off, and assert both.

  This story ships no command, no endpoint and no admin screen — those are #482, #483 and
  #486. The only surface it has is the resolver itself. So these scenarios evaluate
  through the OpenFeature client the way a call site will, and they stage the stored state
  behind it — an instance-level off, an account override, a grant — by reaching the store
  directly from the step definitions, as the account-suspension scenarios in
  account_scoped_sessions.feature already do for the same reason. When #482 gives an
  operator a way to make those changes, the steps change and the scenarios do not.

  The caller is whoever the request context says it is. Nothing here reads a token claim,
  and nothing here should start to: the resolved set lives server-side precisely so that
  it can be invalidated, which a claim baked into a token cannot be. A scenario that
  proved a claim was ignored would have to invent a call site that reads one, so the
  property is pinned from the other side instead — every answer below follows the caller
  and the account on the context, and two people holding different grants inside one
  account get two different answers.

  Failure is closed. A feature declared on answers off when the store behind it cannot be
  read, because a resolver that answers "on" on the way to a database error hands out the
  capability at exactly the moment nobody can see why. A key nobody declared is a
  different thing: it is a deploy that drifted from its registry, so the caller gets the
  default it supplied and the instance says so in its log, once, rather than once per
  evaluation on a hot path.

  Resolution sits in front of MCP tool listings and agent turns, where thirty calls in one
  turn is ordinary, so the resolved set is computed once per session and read from memory
  afterwards. One rule governs what happens next, and it has no exceptions: a change
  reaches sessions that are already open. An instance-level flip, an account override and
  a grant taken away all land the same way, and a grant made to a role lands on every
  member who holds it, not only on whoever the change was made through.

  Nothing in that rule signs anybody out. Clearing what a session had resolved and ending
  that session are different acts with different costs — the first costs the next
  evaluation one database read and the person stays signed in, the second forces them to
  log in again and belongs to a separate feature. No flag change ever forces a re-login,
  and the scenarios say so out loud, because a forced logout is the shape this is most
  likely to be built as by accident.

  The rule is also the same on every deployment. One process serving one instance and a
  fleet of replicas must answer identically; what differs is only the machinery behind the
  invalidation, which no scenario here can see and none should name. That is deliberate:
  behaviour that varied with the deployment shape would hide a replica-only bug from every
  single-process instance it was developed on. Because that machinery can lag or lose a
  message, the staleness that remains is bounded rather than open-ended — a resolved set is
  re-resolved once the configured maximum cache age has passed, whether an invalidation
  arrived or not. The scenario for that configures a short age and waits; on a real
  instance the age is long, because there the invalidation is exact.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- What a declaration is, and where declarations come from ---

  Scenario: A feature declared in code is registered before anything is installed
    When the registered features are listed
    Then the listing includes the feature "episodic-recall"
    And that feature reports the display name "Episodic recall"
    And that feature reports a description an operator can read
    And that feature reports it defaults on
    And that feature reports an account admin may change it
    And that feature reports it may be granted to one person

  Scenario: A feature that no account may change and no grant may reach says so in its declaration
    When the registered features are listed
    Then the listing includes the feature "audit-trail"
    And that feature reports an account admin may not change it
    And that feature reports it may not be granted to one person

  Scenario: Installing a preset adds the features the preset declares
    Given a preset "billing-demo" that declares the feature "invoice-export", defaulting off
    When the "billing-demo" preset is installed
    And the registered features are listed
    Then the listing includes the feature "invoice-export"
    And the listing still includes the feature "episodic-recall"

  Scenario Outline: A feature nobody has touched answers the default it was declared with
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And nothing has been overridden or granted on this instance
    When they evaluate "<key>" with default <supplied>
    Then the feature answers <answer>

    Examples:
      | key             | supplied | answer |
      | episodic-recall | off      | on     |
      | ledger-export   | on       | off    |

  # --- The three layers, and which one wins ---

  Scenario: An account turns on a feature that was declared off
    Given "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "ledger-export" with default off
    Then the feature answers on

  Scenario: An account's off beats a grant held by one of its members
    Given "Harbor Legal" has turned the feature "episodic-recall" off
    And "ops@harborlegal.example" has been granted the feature "episodic-recall"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" with default on
    Then the feature answers off

  Scenario: An instance-level off beats an account that turned the feature on
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "Harbor Legal" has turned the feature "episodic-recall" on
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" with default on
    Then the feature answers off

  Scenario: An instance-level off beats a grant made to one person
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" has been granted the feature "episodic-recall"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" with default on
    Then the feature answers off

  Scenario: An account's setting does not reach into another account
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "Harbor Legal" has turned the feature "episodic-recall" off
    And both of them are signed in
    When each of them evaluates "episodic-recall" with default on
    Then "ops@harborlegal.example" is answered off
    And "broker@cedarrealty.example" is answered on

  Scenario: A grant turns a feature on for the person who holds it and for nobody else
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    When each of them evaluates "ledger-export" with default off
    Then "ops@harborlegal.example" is answered on
    And "counsel@harborlegal.example" is answered off

  Scenario: A grant carried by someone's role resolves the same as one made to them directly
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And the role "admin" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "ledger-export" with default off
    Then the feature answers on

  Scenario: An account override stored against a feature no account may change is ignored
    Given "Harbor Legal" has a stored override turning the feature "audit-trail" off
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "audit-trail" with default on
    Then the feature answers on

  Scenario: A grant stored against a feature that may not be granted is ignored
    Given the feature "audit-trail" is off for the instance
    And "ops@harborlegal.example" holds a stored grant for the feature "audit-trail"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "audit-trail" with default on
    Then the feature answers off

  # --- The caller comes from the request context ---

  Scenario: Two people in one account are answered from their own grants, not from whoever asked last
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    And both of them are signed in to "Harbor Legal"
    When "ops@harborlegal.example" evaluates "ledger-export" with default off
    And "counsel@harborlegal.example" evaluates "ledger-export" with default off
    And "ops@harborlegal.example" evaluates "ledger-export" with default off again
    Then "counsel@harborlegal.example" was answered off
    And "ops@harborlegal.example" was answered on both times

  Scenario: An evaluation with nobody on the context answers the instance-level value
    Given "Harbor Legal" has turned the feature "episodic-recall" off
    When a caller with no identity on the context evaluates "episodic-recall" with default on
    Then the feature answers on
    And no account override or grant was read for that evaluation

  # --- Keys the resolver does not know ---

  Scenario Outline: A key nobody declared answers the default the caller supplied
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate the undeclared feature "shipping-labels" with default <supplied>
    Then the feature answers <supplied>
    And the evaluation reason is "DEFAULT"

    Examples:
      | supplied |
      | on       |
      | off      |

  Scenario: A key outside the feature namespace passes straight through
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate the flag key "entitlement.sea_math_2027" with default on
    Then the flag answers on
    And the evaluation reason is "DEFAULT"

  Scenario: A key nobody declared is logged once however often it is evaluated
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate the undeclared feature "shipping-labels" 20 times with default off
    Then the instance logged the undeclared feature key once
    And the log names the key "shipping-labels"

  # --- Failing closed ---

  Scenario: A feature declared on answers off when the store behind it cannot be read
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" with default on
    Then the feature answers off
    And the evaluation reason is "ERROR"
    And the instance logged the failure

  Scenario: A store failure is not remembered as an answer
    Given the store holding account overrides and grants cannot be read
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they evaluated "episodic-recall" with default on and were answered off
    When the store can be read again
    And they evaluate "episodic-recall" with default on in the same session
    Then the feature answers on
    And they were not signed out by the failure

  Scenario Outline: An evaluation that asks for something other than a boolean answers the caller's own default
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" as <kind> with default <supplied>
    Then the answer is the <supplied> they supplied
    And the evaluation reason is "DEFAULT"

    Examples:
      | kind       | supplied |
      | a string   | trial    |
      | an integer | 7        |
      | a number   | 1.5      |
      | an object  | tier=pro |

  # --- Resolved once per session, read from memory afterwards ---

  Scenario: Evaluating the same feature many times in a session reads the database once
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" 30 times in that session
    Then the feature answers on every time
    And feature state was read from the database only while the first evaluation was answered

  Scenario: The first evaluation resolves every feature, not only the one it was asked about
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "episodic-recall" and then "ledger-export" in the same session
    Then feature state was read from the database only while the first evaluation was answered

  Scenario: Each session gets its own resolved set
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" has been granted the feature "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "ledger-export" and been answered on
    When "counsel@harborlegal.example" signs in and evaluates "ledger-export" with default off
    Then "counsel@harborlegal.example" is answered off

  Scenario: Both sessions one person holds are reached, not only the one that asked first
    Given "ops@harborlegal.example" has been granted the feature "ledger-export"
    And they are signed in on two devices, each with its own session
    And both sessions have already evaluated "ledger-export" and been answered on
    When the grant is taken away from "ops@harborlegal.example"
    And they evaluate "ledger-export" again on each device
    Then both sessions are answered off
    And neither session was signed out

  # --- One rule: a change reaches sessions that are already open ---

  Scenario: A grant taken away is gone from the session the person is already in
    Given "ops@harborlegal.example" has been granted the feature "ledger-export"
    And they are signed in and have been answered on for "ledger-export"
    When the grant is taken away from "ops@harborlegal.example"
    And they evaluate "ledger-export" again in the same session
    Then the feature answers off
    And they are still signed in
    And the request they make next is served

  Scenario: A grant made to a role reaches every member who holds it, in the sessions they already have
    Given "counsel@harborlegal.example" and "clerk@harborlegal.example" both belong to "Harbor Legal" with the role "admin"
    And "temp@harborlegal.example" belongs to "Harbor Legal" without that role
    And all three of them are signed in and have been answered off for "ledger-export"
    When the role "admin" is granted the feature "ledger-export"
    And each of them evaluates "ledger-export" again in the session they already held
    Then "counsel@harborlegal.example" is answered on
    And "clerk@harborlegal.example" is answered on
    And "temp@harborlegal.example" is answered off
    And none of them was signed out

  Scenario: A grant taken away from a role reaches every member who held it
    Given the role "admin" has been granted the feature "ledger-export"
    And "counsel@harborlegal.example" and "clerk@harborlegal.example" both hold the role "admin" in "Harbor Legal"
    And both of them are signed in and have been answered on for "ledger-export"
    When the grant is taken away from the role "admin"
    And each of them evaluates "ledger-export" again in the session they already held
    Then both of them are answered off

  Scenario: An operator turning a feature off for the account reaches sessions already open
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When "Harbor Legal" turns the feature "episodic-recall" off
    And they evaluate "episodic-recall" again in the same session
    Then the feature answers off

  Scenario: An operator turning a feature off for the instance reaches sessions already open
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And both of them are signed in to "Harbor Legal"
    And both of them have already evaluated "episodic-recall" and been answered on
    When the operator turns the feature "episodic-recall" off for the instance
    And each of them evaluates "episodic-recall" again in the session they already held
    Then both of them are answered off

  Scenario: An operator's change to one account does not re-resolve another account's sessions
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And both owners are signed in and have been answered on for "episodic-recall"
    When "Harbor Legal" turns the feature "episodic-recall" off
    And each of them evaluates "episodic-recall" again in the session they already held
    Then "ops@harborlegal.example" is answered off
    And "broker@cedarrealty.example" is answered on
    And "broker@cedarrealty.example" read no feature state from the database to be answered

  # --- The staleness that is left is bounded ---

  Scenario: A change that no invalidation announced is picked up once the maximum cache age passes
    Given the instance is configured with a maximum cache age of 2 seconds
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When "Harbor Legal" turns the feature "episodic-recall" off and no invalidation reaches the session
    And they evaluate "episodic-recall" again straight away
    And they evaluate "episodic-recall" again after the maximum cache age has passed
    Then the evaluation straight away is answered on
    And the evaluation after the maximum cache age is answered off
    And they were signed out by neither evaluation

  Scenario: A session that outlives the maximum cache age with nothing changed keeps answering the same way
    Given the instance is configured with a maximum cache age of 2 seconds
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When nothing is changed
    And they evaluate "episodic-recall" again after the maximum cache age has passed
    Then the feature answers on
    And feature state was read from the database once more, to re-resolve the set
