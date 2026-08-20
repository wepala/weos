@epic-480 @story-483
Feature: An admin grants a feature to specific users or roles
  As an admin on a shared instance
  I want to give one person, or everyone holding a role, a capability the rest of the
  instance does not get
  So that private and paid capabilities reach the people who should have them, and can be
  taken back the same day

  #481 taught resolution about grants and #482 deliberately gave them no surface. So today
  the bottom layer of the stack is the only one nobody can reach: a grant can be resolved,
  invalidated and overruled, and the only way to make one is a step definition writing a
  row. This story is the way in — a command an operator types, endpoints the admin UI
  calls, and MCP tools an agent calls, all three on the FeatureService grant methods #481
  already wrote — plus the two things a grant needs that an override never did, a validity
  window and a record of who made it.

  Nothing below re-derives resolution. That a grant turns a feature on for its holder and
  nobody else, that a role grant resolves the same as a direct one, that an account's off
  and an instance's off both beat a grant, that a stored grant on a non-grantable feature
  is ignored, that taking a grant away reaches sessions already open on every device
  without signing anybody out, and that a role grant reaches every member holding the role
  — all of that is feature_flag_resolution.feature and stays there. Where a scenario
  evaluates a feature here it is proving that a surface wrote what it claimed to, or that a
  window opened or closed, not re-deriving precedence.

  A grant is account-scoped, so who may make one is the question #482 already answered for
  account overrides, not a third rule. The account comes off the caller's session and is
  never a parameter: an owner or admin grants inside the account they are signed in to, an
  ordinary member is refused, and a request that names some other account is answered in
  the caller's own. Instance-wide standing does not enter into it. An operator who wants to
  reach past an account's admin has the instance switch, which is a different act with a
  different blast radius.

  The subject of a grant is a person or a role, and both are pericarp's, not ours. A person
  is named at every surface by their email address, because a KSUID is not something anybody
  types correctly, and the stored grant holds the agent id the email resolved to — the
  scenarios assert both halves, so an implementation that stored the email would fail rather
  than work until somebody changed their address. A role is one of the three pericarp knows:
  owner, admin, member. A grant to a role it does not know is refused rather than stored,
  because a mistyped role name stores a row that can never match any member and produces
  exactly the same observable result as no grant at all — a silence nobody would think to
  investigate. A grant to a person who is not a member of the account is refused for the same
  reason. Granting to a real role that simply nobody holds yet is fine and is not the same
  thing: the role exists, and whoever is given it next gets the feature with it.

  Validity windows are the new storage. A grant may carry a `validFrom`, a `validThrough`,
  both, or neither, and a grant outside its window resolves exactly as if the row were not
  there — no cleanup job removes it and no scheduled task flips anything, because the window
  is read at resolution and a row that has run out is simply not counted. An expired grant
  stays in the listing, marked as expired, because an admin asking "why did they lose that?"
  is asking about a row somebody deleted or a window that closed, and only one of those two
  leaves evidence.

  What a window makes hard is that a grant which expires is not a write. Nothing invalidates
  anything at the moment `validThrough` passes, so on the machinery #481 built, a holder with
  a cached resolved set keeps the feature until the maximum cache age runs out — up to
  fifteen minutes on the default configuration. This contract does not accept that: two
  scenarios below configure a maximum cache age of ten minutes and then assert the answer
  flips within seconds of the boundary, in a session already open, with nothing invalidated
  and nothing restarted. They are written that way on purpose. A resolved set that knows the
  earliest boundary among the grants it counted, and treats itself as stale at that instant,
  passes them; a resolved set that only knows how old it is cannot, and would pass only if
  the maximum cache age were tuned down to the accuracy anybody wanted from a window — which
  would throw away the whole point of caching the other three hundred and sixty evaluations.

  Revocation is the other half of the day. It deletes the row rather than storing an off,
  for the same reason a grant has no enabled column: a grant that could say "off" would let
  the bottom layer overrule the two above it. Revocation targets one subject, which is worth
  saying out loud because deleting by feature and account is the shorter query and takes away
  the role grant along with the personal one. And it is recorded at the moment it is made,
  through the same Feature.Changed record #482 asserts for overrides, so that "who took that
  away, and when" has an answer that does not require a debugger. Whether a revocation that
  matched no row is recorded at all is deliberately left open here; the scenario for it
  asserts only that the call succeeds and nothing is stored.

  The command line is the surface with no session, and grants need an account. So it names
  both: the person by email, the account by its id — pericarp offers no lookup by account
  name, and inventing one here would be a second identity feature smuggled into a flags
  story — and the account may be left out only on an instance that has exactly one, the same
  rule and the same refusal when it is ambiguous that #482 settled for the instance switch.
  Note that "exactly one account" is narrower than it sounds: registration mints every new
  person a personal account, so a single-account instance is a single-person instance, which
  is the mini-me shape and the one where nobody should have to look up a KSUID to grant
  themselves something. The local stdio MCP transport gets no such
  latitude: it is treated as the operator for instance-wide state because whoever holds the
  machine already holds the database, but an account-scoped grant has no account to land in
  when nobody is signed in, and guessing one would be the multi-tenant hole the whole
  arrangement exists to avoid. It is refused, and told where to go instead.

  Cost is asserted once, at the shape that matters. Resolution reads a caller's grants on the
  first evaluation of a session and never again until something invalidates it, so the read
  worth pinning is that one: it returns the grants that could apply to this caller and not
  the account's, however many the account holds. The latency half of the story's criterion is
  a Go benchmark beside the repository, not a scenario — a wall-clock assertion in a godog
  suite measures the machine it ran on.

  Background:
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the instance declares these features in code:
      | key             | display name    | description                              | default | manageable | grantable |
      | episodic-recall | Episodic recall | Recall past events during a conversation | on      | yes        | yes       |
      | ledger-export   | Ledger export   | Export the ledger to a spreadsheet       | off     | yes        | yes       |
      | audit-trail     | Audit trail     | Record who read what, for compliance     | on      | no         | no        |
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"

  # --- Making a grant ---

  Scenario: An admin grants a feature to one person, who has it in the session they are already in
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" is signed in and has been answered off for "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" evaluates "ledger-export" again in the session they already held
    Then the change was accepted
    And "counsel@harborlegal.example" is answered on
    And they were not signed out
    And the instance was not restarted

  Scenario: The grant that is stored names the person by their agent id, not by the address it was asked for
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API
    Then the change was accepted
    And the stored grant names the agent id of "counsel@harborlegal.example"
    And the stored grant does not carry their email address
    And the stored grant is scoped to "Harbor Legal"

  Scenario: A grant records who made it and when
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API
    Then the change was accepted
    And the grant records "ops@harborlegal.example" as who made it
    And the grant carries the time it was made
    And the change was recorded with the key "ledger-export" and the person it was granted to
    And the record names "ops@harborlegal.example" as who made it

  Scenario: Granting the same person the same feature twice leaves one grant, not two
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API again
    Then the change was accepted
    And the instance holds one stored grant of "ledger-export" for "counsel@harborlegal.example"

  # --- Who may grant, and where the grant lands ---

  Scenario: An ordinary member may read the grants on a feature but may not make one
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    When they ask the API for the grants on "ledger-export"
    And they try to grant "ledger-export" to themselves through the API
    Then the listing was served
    And the attempt to grant it is refused as forbidden
    And no grant of "ledger-export" is stored for "clerk@harborlegal.example"

  Scenario: A request carrying no session cannot grant anything
    When a request carrying no session tries to grant "ledger-export" to "ops@harborlegal.example"
    Then the request is refused as not authenticated
    And no grant of "ledger-export" is stored for "ops@harborlegal.example"

  Scenario: A grant lands in the account the caller is signed in to, not one they named
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API, naming the account "Cedar Realty"
    Then the stored grant is scoped to "Harbor Legal"
    And no grant of "ledger-export" is stored in "Cedar Realty"

  Scenario: An admin cannot grant to somebody who is not a member of the account they are signed in to
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "ledger-export" to "broker@cedarrealty.example" through the API
    Then the attempt is refused as a bad request
    And the refusal says that person is not a member of this account
    And no grant of "ledger-export" is stored for "broker@cedarrealty.example"

  Scenario: An address nobody signed up with is refused rather than stored against nothing
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "ledger-export" to "nobody@harborlegal.example" through the API
    Then the attempt is refused as not found
    And the failure names the address "nobody@harborlegal.example"
    And no grant of "ledger-export" is stored at all

  Scenario: A feature that may not be granted is refused rather than silently stored
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "audit-trail" to "counsel@harborlegal.example" through the API
    Then the attempt is refused as a bad request
    And the refusal says the feature cannot be granted
    And no grant of "audit-trail" is stored at all

  Scenario: A key nobody declared is refused rather than stored
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "shipping-labels" to "counsel@harborlegal.example" through the API
    Then the attempt is refused as not found
    And the failure names the key "shipping-labels"
    And no grant of "shipping-labels" is stored at all

  # --- Granting to a role ---

  Scenario: A role grant re-resolves the members who hold the role and leaves the rest of the account alone
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And both of them are signed in and have been answered off for "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to the role "admin" through the API
    And each of them evaluates "ledger-export" again in the session they already held
    Then "counsel@harborlegal.example" is answered on
    And "clerk@harborlegal.example" is answered off
    And "clerk@harborlegal.example" read no feature state from the database to be answered

  Scenario: A grant to a role nobody holds yet reaches whoever is given it afterwards
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in and has been answered off for "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to the role "admin" through the API
    And "clerk@harborlegal.example" is given the role "admin" in "Harbor Legal"
    And "clerk@harborlegal.example" evaluates "ledger-export" again in the session they already held
    Then the change was accepted
    And "clerk@harborlegal.example" is answered on

  Scenario: A role the instance does not know is refused rather than stored against nobody
    Given "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "ledger-export" to the role "auditor" through the API
    Then the attempt is refused as a bad request
    And the refusal names the roles that exist
    And no grant of "ledger-export" is stored for the role "auditor"

  # --- Validity windows ---

  Scenario: A grant with no window is live from the moment it is made and stays live
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they grant "ledger-export" to "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" signs in and evaluates "ledger-export" with default off
    Then "counsel@harborlegal.example" is answered on
    And the grant reports no window

  Scenario: A grant whose window has already closed is denied, and nothing removed the row
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until an hour ago
    When "counsel@harborlegal.example" signs in and evaluates "ledger-export" with default off
    Then "counsel@harborlegal.example" is answered off
    And the instance holds one stored grant of "ledger-export" for "counsel@harborlegal.example"
    And the grants on "ledger-export" report that grant as expired

  Scenario: A grant that has not started yet is denied, and starts on its own in a session already open
    Given the instance is configured with a maximum cache age of 10 minutes
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And "counsel@harborlegal.example" is signed in and has been answered off for "ledger-export"
    When "ops@harborlegal.example" grants "ledger-export" to "counsel@harborlegal.example" through the API, valid from 2 seconds from now
    And "counsel@harborlegal.example" evaluates "ledger-export" again straight away
    And "counsel@harborlegal.example" evaluates "ledger-export" again once that moment has passed
    Then the evaluation straight away is answered off
    And the evaluation after that moment is answered on
    And nothing invalidated the session in between
    And the maximum cache age had not run out

  Scenario: A grant that expires is denied from the moment it expires, not when the maximum cache age runs out
    Given the instance is configured with a maximum cache age of 10 minutes
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until 2 seconds from now
    And "counsel@harborlegal.example" is signed in and has been answered on for "ledger-export"
    When "counsel@harborlegal.example" evaluates "ledger-export" again straight away
    And "counsel@harborlegal.example" evaluates "ledger-export" again once that moment has passed
    Then the evaluation straight away is answered on
    And the evaluation after that moment is answered off
    And nothing invalidated the session in between
    And the maximum cache age had not run out
    And "counsel@harborlegal.example" is still signed in

  Scenario: A window that ends before it begins is refused
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they try to grant "ledger-export" to "counsel@harborlegal.example" through the API, valid from tomorrow until yesterday
    Then the attempt is refused as a bad request
    And the refusal says the window ends before it begins
    And no grant of "ledger-export" is stored for "counsel@harborlegal.example"

  Scenario: An expired grant of their own does not cancel the live one their role carries
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to the role "admin" through the API
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until an hour ago
    When "counsel@harborlegal.example" signs in and evaluates "ledger-export" with default off
    Then "counsel@harborlegal.example" is answered on

  # --- Taking a grant back ---

  Scenario: A revoked grant is denied in the session the person is already in, without a re-login
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" is signed in and has been answered on for "ledger-export"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" evaluates "ledger-export" again in the session they already held
    Then the change was accepted
    And "counsel@harborlegal.example" is answered off
    And "counsel@harborlegal.example" is still signed in
    And the request "counsel@harborlegal.example" makes next is served
    And the grants on "ledger-export" no longer name "counsel@harborlegal.example"

  Scenario: Revoking a role grant reaches every member who holds the role, in the sessions they already have
    Given "counsel@harborlegal.example" and "clerk@harborlegal.example" both belong to "Harbor Legal" with the role "admin"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to the role "admin" through the API
    And both of them are signed in and have been answered on for "ledger-export"
    When "ops@harborlegal.example" revokes "ledger-export" from the role "admin" through the API
    And each of them evaluates "ledger-export" again in the session they already held
    Then both of them are answered off
    And neither of them was signed out

  Scenario: Revoking a person's own grant does not take away the one their role carries
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to the role "admin" through the API
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" is signed in and has been answered on for "ledger-export"
    When "ops@harborlegal.example" revokes "ledger-export" from "counsel@harborlegal.example" through the API
    And "counsel@harborlegal.example" evaluates "ledger-export" again in the session they already held
    Then "counsel@harborlegal.example" is answered on
    And the grants on "ledger-export" still name the role "admin"

  Scenario: A revocation is recorded at the moment it is made
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    When they revoke "ledger-export" from "counsel@harborlegal.example" through the API
    Then the change was accepted
    And the change was recorded as a revocation of "ledger-export" from "counsel@harborlegal.example"
    And the record names "ops@harborlegal.example" as who made it
    And the record carries the time the change was made

  Scenario: Revoking a grant nobody holds succeeds and changes nothing
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they revoke "ledger-export" from "counsel@harborlegal.example" through the API
    Then the change was accepted
    And no grant of "ledger-export" is stored at all

  # --- Seeing what has been granted ---

  Scenario: The grants on a feature are listed with their subject, their window and who made them
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until tomorrow
    And they have granted "ledger-export" to the role "admin" through the API
    When they ask the API for the grants on "ledger-export"
    Then the listing was served
    And the grants on "ledger-export" name "counsel@harborlegal.example" and the role "admin"
    And the grant to "counsel@harborlegal.example" reports it is in effect until tomorrow
    And the grant to "counsel@harborlegal.example" records "ops@harborlegal.example" as who made it
    And the grant to the role "admin" reports no window

  Scenario: Everything one person holds is listed, whether they hold it themselves or through a role
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal" with the role "admin"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    And they have granted "episodic-recall" to the role "admin" through the API
    When they ask the API for everything granted to "counsel@harborlegal.example"
    Then the answer names "ledger-export", granted to them directly
    And the answer names "episodic-recall", granted through the role "admin"

  Scenario: One account's grants are not visible from another
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    When "broker@cedarrealty.example" signs in and asks the API for the grants on "ledger-export"
    Then the listing was served
    And the grants "broker@cedarrealty.example" is shown name nobody

  # --- The command line ---

  Scenario: An operator grants from the command line on an instance with one account
    When the operator runs "weos feature grant ledger-export --email ops@harborlegal.example"
    Then the command exits successfully
    And the instance holds one stored grant of "ledger-export" for "ops@harborlegal.example"

  Scenario: An operator takes a grant back from the command line
    Given the operator has run "weos feature grant ledger-export --email ops@harborlegal.example"
    When the operator runs "weos feature revoke ledger-export --email ops@harborlegal.example"
    Then the command exits successfully
    And no grant of "ledger-export" is stored at all

  Scenario: The command line lists the grants on a feature, with their windows
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until tomorrow
    When the operator runs "weos feature grants ledger-export" against the account "Harbor Legal"
    Then the command exits successfully
    And the command names "counsel@harborlegal.example"
    And the command reports that grant as in effect until tomorrow

  Scenario: On an instance with more than one account the command line must be told which
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    When the operator runs "weos feature grant ledger-export --email ops@harborlegal.example"
    Then the command exits with a failure
    And the failure says which account must be named
    And no grant of "ledger-export" is stored at all

  Scenario: Named explicitly, the command line grants into the account it was given
    Given the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    When the operator runs "weos feature grant ledger-export --email ops@harborlegal.example" against the account "Harbor Legal"
    Then the command exits successfully
    And the stored grant is scoped to "Harbor Legal"
    And no grant of "ledger-export" is stored in "Cedar Realty"

  Scenario: A grant made from the command line names the command line as what made it
    When the operator runs "weos feature grant ledger-export --email ops@harborlegal.example"
    Then the command exits successfully
    And the change was recorded with the key "ledger-export" and the person it was granted to
    And the record names the command line as what made the change
    And the record carries the time the change was made

  # --- The MCP tools an agent calls ---

  Scenario: An agent grants a feature and the person has it in the session they already hold
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "counsel@harborlegal.example" is signed in and has been answered off for "ledger-export"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    When they call the MCP tool "feature_grant" to grant "ledger-export" to "counsel@harborlegal.example"
    And "counsel@harborlegal.example" evaluates "ledger-export" again in the session they already held
    Then the call succeeds
    And "counsel@harborlegal.example" is answered on

  Scenario: An agent takes a grant back
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API
    When they call the MCP tool "feature_revoke" to take "ledger-export" from "counsel@harborlegal.example"
    Then the call succeeds
    And no grant of "ledger-export" is stored at all

  Scenario: What the MCP grant listing reports matches what the API lists
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "ops@harborlegal.example" is signed in to "Harbor Legal"
    And they have granted "ledger-export" to "counsel@harborlegal.example" through the API, valid until tomorrow
    When they call the MCP tool "feature_grants" for "ledger-export"
    Then the call succeeds
    And what the tool reported matches the grants the API lists for "ledger-export"

  Scenario: A member calling the MCP grant tool is refused, as they are at the API
    Given "clerk@harborlegal.example" belongs to "Harbor Legal" as an ordinary member
    And "clerk@harborlegal.example" is signed in to "Harbor Legal"
    When they call the MCP tool "feature_grant" to grant "ledger-export" to "clerk@harborlegal.example"
    Then the call is refused as forbidden
    And no grant of "ledger-export" is stored at all

  Scenario: A caller on the local stdio transport is refused a grant and told where to make one
    Given an MCP client attached to the store over the local stdio transport
    When it calls "feature_grant" to grant "ledger-export" to "ops@harborlegal.example"
    Then the call is refused
    And the refusal says a grant needs an account and names the command line
    And no grant of "ledger-export" is stored at all

  # --- What resolution costs ---

  Scenario: Resolving one caller reads the grants that could apply to them, not the account's
    Given "counsel@harborlegal.example" also belongs to "Harbor Legal"
    And "Harbor Legal" holds 200 grants of "ledger-export", one for each of 200 other members
    And "counsel@harborlegal.example" holds one of their own for "ledger-export"
    And "counsel@harborlegal.example" is signed in to "Harbor Legal"
    When they evaluate "ledger-export" with default off
    Then the feature answers on
    And the grant store was read once to answer them
    And that read returned only the grants that could apply to them
