@epic-480 @story-482
Feature: An operator turns a feature on or off for the whole instance
  As an operator running a WeOS instance
  I want to see and change what is on, from a shell, from the admin UI, or from an agent
  So that shipping dark work and stopping a broken capability never means a redeploy

  #481 built the answer and nobody could change it. It declared features, folded the three
  layers into one resolution, cached the result per session and invalidated that cache on
  every write — but the only writers were the step definitions, reaching into the store
  directly because no operator-facing way in existed yet. This story is that way in: a
  command an operator types, endpoints the admin UI calls, and MCP tools an agent calls,
  all three sitting on the FeatureService methods #481 already wrote and tested.

  So nothing below re-asserts resolution. Which layer beats which, that an explicit off is
  final downward while an explicit on is not, that an ineligible stored row is ignored,
  that a failed read answers off, that an undeclared key answers the caller's default and
  is logged once, that a change reaches sessions already open without signing anybody out —
  all of that is feature_flag_resolution.feature and stays there. What is new here is only
  what the three surfaces do: what they show, what they store, who they refuse, and that
  the store they wrote reaches whoever asks next. Where a scenario evaluates a feature at
  all, it is evaluating to prove the surface wrote what it claimed to, not to re-derive the
  precedence rules.

  Three surfaces, one write path. The command line opens the store directly the way
  `weos account create` does, so an operator can flip a feature on an instance whose admin
  UI is exactly the thing that is broken. The endpoints exist because the admin UI in #486
  needs them and because an operator working through a browser should not have to shell in.
  The MCP tools exist because an agent turn is a first-class caller on this product and an
  operator talking to their instance should be able to say "turn episodic recall off" and
  have it happen. All three must agree, and the listing scenarios are written to make a
  disagreement fail rather than go unnoticed.

  What the listing has to say is more than on-or-off. A feature reading "off" because
  nobody ever touched it and a feature reading "off" because an operator turned it off last
  Tuesday are different facts with different next actions, and an operator who cannot tell
  them apart will "fix" the first by disabling it and be unable to explain the second. So
  every listing reports the state and the layer that decided it, from the same
  ResolveFeature the resolver uses, and the scenarios assert both halves.

  Reset is the same distinction from the writing end, and it is the one place in this
  subsystem where a plausible implementation does real damage. Reset removes the override;
  it does not write "off". A feature declared on and then reset answers on again, a feature
  declared off and then reset answers off — that much a careless implementation gets right
  by accident. What it gets wrong is that an explicit off is final for every layer below it
  and a declared-off default is not, so an implementation that "resets" by storing false has
  quietly taken the feature away from every account and every grant that could otherwise
  turn it on, and the listing will still read "off" exactly as expected. One scenario below
  therefore resets a feature and then has an account turn it on, because that is the only
  observation that tells the two apart.

  Who counts as an operator is deliberately underspecified here, because the instance has
  no operator role to check. WeOS knows account owners, account admins and members
  (`GetUserRole`, `FindMemberRole`); it knows nothing about a person's standing on the
  instance as a whole. The scenarios pin the two answers every candidate design agrees on —
  an ordinary member is refused, and the owner of the instance's account is not — and say
  nothing about whether the admin of a second, unrelated account may flip instance state for
  everyone. That question needs a decision, not a guess, and pinning a guess here would
  either bless a real multi-tenant hole or block a design that closes it. The command line
  and the local stdio transport are treated as the operator by construction: both already
  have the database, so a permission check against a store they could edit with sqlite3 would
  be theatre, and this is the same call the knowledge-graph stdio exception already makes.

  Account overrides appear here only as far as their surface. #481 proved that an override
  on a non-manageable feature is ignored at resolution; what these scenarios add is that the
  admin who asks for one is told so rather than silently obeyed and overruled, that an
  override lands on the account the caller is signed in to rather than one they named, and
  that it does not leak into another account. Grants get no surface in this story — that is
  #483 — and no scenario below grants anything except where an existing #481 step is the
  cheapest way to stage a precondition.

  The command line is the one surface that is not in the serving process, and that gap is
  visible. A flip typed into a shell writes rows a running instance has already cached, and
  the in-process invalidator inside that instance never hears about it; only the bounded
  maximum cache age guarantees the running instance comes back to the store. The scenario
  for that therefore asserts the promise the story actually makes — no restart, no cache
  flush by hand — and allows the bounded window rather than demanding the flip land on the
  very next request, so it holds whether the invalidation channel is later widened to reach
  other processes or not. On a real instance that age is long, which is precisely why the
  gap is worth stating out loud rather than leaving for someone to discover.

  Logging is asserted as a record an operator can read afterwards, not as a log format.
  "Who did it" has an answer through the endpoints and the MCP tools — the authenticated
  person — and does not through the command line, where there is no identity at all; the
  honest record there names the command line itself, so that a change nobody can attribute
  is at least never mistaken for one somebody made in the UI.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- The command line ---

  Scenario: The command line lists every feature with its state and where that state came from
    Given the operator has run "weos feature disable episodic-recall"
    When the operator runs "weos feature list"
    Then the command exits successfully
    And the listing reports "episodic-recall" as off, from an instance override
    And the listing reports "ledger-export" as off, from the declared default
    And the listing reports "audit-trail" as on, from the declared default
    And the listing names each feature by its display name

  Scenario Outline: An operator flips a feature for the instance from the command line
    When the operator runs "weos feature <command> <key>"
    Then the command exits successfully
    And the listing reports "<key>" as <state>, from an instance override

    Examples:
      | command | key             | state |
      | enable  | ledger-export   | on    |
      | disable | episodic-recall | off   |

  Scenario Outline: Reset drops the override and the feature returns to the default it was declared with
    Given the operator has run "weos feature <command> <key>"
    When the operator runs "weos feature reset <key>"
    Then the command exits successfully
    And the listing reports "<key>" as <state>, from the declared default

    Examples:
      | command | key             | state |
      | enable  | ledger-export   | off   |
      | disable | episodic-recall | on    |

  Scenario: A reset leaves a feature an account can still turn on, where an explicit off would not
    Given the operator has run "weos feature enable ledger-export"
    And the operator has run "weos feature reset ledger-export"
    And "Harbor Legal" has turned the feature "ledger-export" on
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "ledger-export" with default off
    Then the feature answers on

  Scenario: Flipping a feature that is already in that state succeeds and changes nothing
    Given the operator has run "weos feature disable episodic-recall"
    When the operator runs "weos feature disable episodic-recall" again
    Then the command exits successfully
    And the listing reports "episodic-recall" as off, from an instance override
    And the instance holds one stored instance-level setting for "episodic-recall"

  Scenario: A key nobody declared is refused rather than stored
    When the operator runs "weos feature enable shipping-labels"
    Then the command exits with a failure
    And the failure names the key "shipping-labels"
    And no instance-level setting is stored for "shipping-labels"
    And the listing does not report "shipping-labels"

  Scenario: A flip with no database configured refuses rather than flipping some other store
    Given a WeOS instance with no database configured
    When the operator runs "weos feature disable episodic-recall"
    Then the command exits with a failure
    And the failure says no database was specified and names how to supply one
    And the command leaves no database behind in the directory it ran from

  Scenario: A flip made from the command line reaches an instance that is already running
    Given the instance is configured with a maximum cache age of 2 seconds
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When the operator runs "weos feature disable episodic-recall" against the same store
    And they evaluate "episodic-recall" again once the maximum cache age has passed
    Then the feature answers off
    And the instance was not restarted
    And they are still signed in

  Scenario: A flip made from the command line records what changed and that the command line changed it
    When the operator runs "weos feature disable episodic-recall"
    Then the command exits successfully
    And the change was recorded with the key "episodic-recall" and the state it was set to
    And the record names the command line as what made the change
    And the record carries the time the change was made

  # --- The endpoints the admin UI calls ---

  Scenario: An operator turns a feature off through the API and the next request sees it off
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When they turn the feature "episodic-recall" off for the instance through the API
    And they evaluate "episodic-recall" again in the same session
    Then the change was accepted
    And the feature answers off
    And the instance was not restarted

  Scenario: The API lists every feature with its state and where that state came from
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for the feature listing
    Then the listing reports "episodic-recall" as off, from an instance override
    And the listing reports "ledger-export" as off, from the declared default
    And the listing names each feature by its display name

  Scenario: A reset through the API returns the feature to the default it was declared with
    Given the operator has turned the feature "ledger-export" on for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they reset the feature "ledger-export" for the instance through the API
    Then the change was accepted
    And the listing reports "ledger-export" as off, from the declared default

  Scenario: A member may read the listing but may not change the instance
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for the feature listing
    And they try to turn the feature "episodic-recall" off for the instance through the API
    Then the listing was served
    And the attempt to change it is refused as forbidden
    And no instance-level setting is stored for "episodic-recall"

  Scenario: A request carrying no session cannot change the instance
    When a request carrying no session tries to turn the feature "episodic-recall" off for the instance
    Then the request is refused as not authenticated
    And no instance-level setting is stored for "episodic-recall"

  Scenario: A key nobody declared is refused through the API rather than stored
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to turn the feature "shipping-labels" off for the instance through the API
    Then the attempt is refused as not found
    And no instance-level setting is stored for "shipping-labels"

  Scenario: An account admin turns a manageable feature on for the account they are signed in to
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they turn the feature "ledger-export" on for their own account through the API
    And "broker@cedarrealty.example" signs in and evaluates "ledger-export" with default off
    Then the change was accepted
    And "counsel@harborlegal.example" is answered on
    And "broker@cedarrealty.example" is answered off
    And no instance-level setting is stored for "ledger-export"

  Scenario: An account admin may not override a feature no account may change
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they try to turn the feature "audit-trail" off for their own account through the API
    Then the attempt is refused as a bad request
    And the refusal says the feature cannot be changed per account
    And no account-level setting is stored for "audit-trail"

  Scenario: A flip through the API names who made it and when
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they turn the feature "episodic-recall" off for the instance through the API
    Then the change was accepted
    And the change was recorded with the key "episodic-recall" and the state it was set to
    And the record names "ops@harborlegal.example" as who made it
    And the record carries the time the change was made

  # --- The MCP tools an agent calls ---

  Scenario: The MCP listing reports the same state and source as the API
    Given the operator has turned the feature "episodic-recall" off for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call the MCP tool "feature_list"
    Then the listing reports "episodic-recall" as off, from an instance override
    And the listing reports "ledger-export" as off, from the declared default
    And what the tool reported matches what the API lists

  Scenario: A flip through MCP reaches the next request like any other
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have already evaluated "episodic-recall" and been answered on
    When they call the MCP tool "feature_set" to turn "episodic-recall" off for the instance
    And they evaluate "episodic-recall" again in the same session
    Then the feature answers off
    And the instance was not restarted

  Scenario: A reset through MCP returns the feature to the default it was declared with
    Given the operator has turned the feature "ledger-export" on for the instance
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they reset the feature "ledger-export" for the instance through MCP
    Then the change was accepted
    And the listing reports "ledger-export" as off, from the declared default

  Scenario: A member calling the MCP tool is refused, as they are at the API
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    When they call the MCP tool "feature_set" to turn "episodic-recall" off for the instance
    Then the call is refused as forbidden
    And no instance-level setting is stored for "episodic-recall"

  Scenario: A caller on the local stdio transport changes instance state like the command line
    Given an MCP client attached to the store over the local stdio transport
    When it calls "feature_set" to turn "episodic-recall" off for the instance
    Then the call succeeds
    And the listing reports "episodic-recall" as off, from an instance override
