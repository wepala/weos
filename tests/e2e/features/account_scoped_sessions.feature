@wip @story-472
Feature: A session knows which account it acts in
  As someone signed in to a WeOS instance
  I want everything I do afterwards to happen in the account I signed in to
  So that the instance's per-account settings, data and graph are the ones I get

  A sign-in resolves an account — the person's own, or one they were added to — and that
  moment is the only chance the instance has to record which account the session acts in.
  Nothing downstream can recover it. A person may belong to several accounts, and no rule
  that picks "the first one found" picks the right one for the person who just signed in.

  Until now the sign-in resolved the account and then dropped it. Sessions were stored with
  no account at all, so every request that authenticated by its session acted in no account.
  This was invisible on an instance with no sign-in configured, because the development
  stand-in fills the field in from a lookup of its own; only instances that actually
  authenticate people ran the broken path. What it cost there was quiet rather than loud:
  nothing failed, the account's own settings simply stopped being read and the account's own
  graph simply stopped being found, so a configured instance behaved like a fresh one. The
  section "What the account was for" is here because a fix that only makes the field
  non-empty would leave both of those symptoms exactly where they are.

  That is the second thing these scenarios are shaped around. The account is one string
  among adjacent strings — an agent before it, a credential after it — so nothing but a test
  can tell a session scoped to the right account from one scoped to a credential. "Not
  empty" is therefore not the property. Identity is. Every scenario below names the account
  by something the person can see — the data they get back, the settings that apply, the
  graph that answers — and several stage a second account on purpose, so that the wrong
  answer is a different account rather than an absent one.

  Sessions made before the account was recorded carry none, and there is no repair: such a
  session is refused rather than quietly re-scoped by a lookup, because a lookup is the one
  thing that cannot know the answer. Everyone signed in at the time signs in again. That
  cost is intended, and the scenario for it is here so that a later convenience fix has to
  delete it deliberately rather than absorb it.

  Four refusals share the 401 status and need different handling from whoever receives them,
  so three of the four carry a code in the body and the scenarios assert the code rather
  than the status all four share. Signing in again fixes a session that expired. It cannot
  fix a session belonging to someone with no account to act in, and a client that retries
  that one loops for as long as the person keeps trying. It may fix a session whose account
  the person was removed from, because the next sign-in finds another account they still
  belong to. And it will not fix a suspended account: the person is still a member, so
  nothing about them has to change — an operator has to turn the account back on. The last
  two are the pair most likely to be treated as one, and they are opposites: in one the
  person has lost the account, in the other the account has lost its licence to be used.

  Suspending an account is now a boundary rather than a filter on new sign-ins. Sessions
  already open inside a suspended account stop being served, and an outstanding invitation
  into one can no longer be accepted, so that suspension does not depend on when a person
  last signed in. Nothing in WeOS suspends an account — there is no such route, command or
  field anywhere in this codebase — so the scenarios below state the situation and leave the
  mechanism to the step definitions, which reach the account service directly.

  A sign-in vouches for the account it just resolved, so nothing re-reads it in the same
  request. That is not only one read saved. A brand-new account writes its membership and
  then checks it moments later, and on a replica or an asynchronous read model that row may
  not be visible yet — which used to fail the newcomer's very first sign-in and nothing
  else. The registration scenario asserts the first authenticated request, because that is
  the request that used to break.

  One property in this area cannot be reached over HTTP and is pinned by a unit test
  instead: revocation is skipped entirely when memberships cannot be read, so that a
  database outage cannot sign every user out at once. Staging it here would mean handing the
  server a deliberately broken repository, which demonstrates the harness rather than the
  product.

  # --- The account a sign-in resolved is the account the session acts in ---

  Scenario: A signed-in person's work happens in the account they signed in to
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    When "ops@harborlegal.example" signs in
    And they list the projects they can see
    Then the projects they see include "Q3 Roadmap"

  # Blocked by wepala/weos#474: resource listing is gated per resource, not by
  # the session's active account, so a member of both accounts sees both. The
  # session scoping this scenario also asserts is correct and covered elsewhere.
  @pending-product
  Scenario: A person who belongs to two accounts works in the one the sign-in chose
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" also belongs to the account "Cedar Realty"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    And "ops@harborlegal.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    When "ops@harborlegal.example" signs in
    Then the sign-in reports which account it signed them in to
    And the projects they see include "Q3 Roadmap"
    And the projects they see exclude "Bridge upgrade"
    And the account their requests act in is the one the sign-in reported

  Scenario: The session takes the account the sign-in resolved, not the one a lookup would find
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", which "counsel@harborlegal.example" was added to and signs in to with password "trellis-anchor-mango-9"
    And that person's own personal account has been deactivated
    And "counsel@harborlegal.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    When "counsel@harborlegal.example" signs in
    Then the sign-in succeeds
    And the projects they see include "Bridge upgrade"
    And the account their requests act in is "Cedar Realty"

  Scenario: A newcomer's very first request after registering is served
    Given a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session
    When "founder@harborlegal.example" registers with password "correct-horse-battery-staple"
    Then the registration succeeds
    And the very first request they make with the session they were given is served
    And the account that first request acts in is the one the registration reported
    And a "project" they create afterwards is one they can see

  Scenario: Two people signed in at once do not act in each other's accounts
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    When both of them sign in
    And each of them creates a "project" named "Annual audit"
    Then each of them sees only their own "Annual audit"
    And the two projects belong to different accounts

  # --- Sessions made before the account was recorded ---

  Scenario: An instance that already holds sessions keeps working after the upgrade
    Given a WeOS instance whose database already holds sessions from before the upgrade
    When the instance is started with password sign-in enabled
    Then the instance starts and serves requests
    And someone who signs in fresh is served

  Scenario: A session that names no account is refused rather than repaired
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "ops@harborlegal.example" holds a session made before sessions recorded an account
    When they make a request with that session
    Then the request is refused as not authenticated
    And the refusal says the session has no account to act in
    And the session still names no account afterwards

  Scenario: Signing in again replaces an unscoped session with a working one
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" holds a session made before sessions recorded an account
    When "ops@harborlegal.example" signs in again
    And they make a request with the session they were given
    Then the request is served
    And the account their requests act in is "Harbor Legal"

  # --- Telling the four refusals apart ---

  Scenario: A request carrying no session at all is refused with no code to interpret
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    When someone makes a request carrying no session
    Then the request is refused as not authenticated
    And the refusal carries no code

  Scenario Outline: A session that has stopped being valid is refused with no code
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" signed in and their session has since <become>
    When they make a request with that session
    Then the request is refused as not authenticated
    And the refusal carries no code

    Examples:
      | become          |
      | expired         |
      | been signed out |

  Scenario: A session with no account to act in says so, so a client does not retry sign-in
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "ops@harborlegal.example" holds a session with no account to act in
    When they make a request with that session
    Then the request is refused as not authenticated
    And the refusal says the session has no account to act in

  Scenario: Someone removed from their session's account is told the refusal may recover
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", which "counsel@harborlegal.example" was added to and signs in to with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" is signed in
    When "counsel@harborlegal.example" is removed from "Cedar Realty"
    And they make a request with the session they already held
    Then the request is refused as not authenticated
    And the refusal says their access to the account was taken away
    And the session is not signed out, so it serves again once the membership is back

  Scenario: The three coded refusals do not look alike to whoever receives them
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "ops@harborlegal.example" holds a session with no account to act in
    And "counsel@harborlegal.example" holds a session naming an account they were removed from
    And "broker@cedarrealty.example" holds a session naming an account that has been deactivated
    When each of them makes a request with the session they hold
    Then all three requests are refused as not authenticated
    And the three refusals carry three different codes
    And none of them looks like the refusal a session that simply expired gets

  Scenario: Losing the account and having it suspended are told apart
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", which "counsel@harborlegal.example" and "broker@cedarrealty.example" both belong to and sign in to
    And both of them are signed in
    When "counsel@harborlegal.example" is removed from "Cedar Realty"
    And "Cedar Realty" is deactivated
    Then the request "counsel@harborlegal.example" makes says their access to the account was taken away
    And the request "broker@cedarrealty.example" makes says the account itself is not available
    And "broker@cedarrealty.example" is still a member of "Cedar Realty"

  Scenario: Signing in again after removal lands the person in an account they still belong to
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" was also added to the account "Cedar Realty" and is signed in to it
    When "counsel@harborlegal.example" is removed from "Cedar Realty"
    And they sign in again
    Then the sign-in succeeds
    And the account their requests act in is "Harbor Legal"

  Scenario: Signing in again after losing every account is refused for the reason that stays
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", which "counsel@harborlegal.example" was added to and signs in to with password "trellis-anchor-mango-9"
    And that person's own personal account has been deactivated
    When "counsel@harborlegal.example" is removed from "Cedar Realty"
    And they sign in again
    And they make a request with the session they were given
    Then the request is refused as not authenticated
    And the refusal says the session has no account to act in
    And signing in a further time does not change the answer

  # --- Suspending an account locks its members out ---

  Scenario: A session already open in an account that is then suspended stops being served
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "broker@cedarrealty.example" is signed in and their requests are being served
    When "Cedar Realty" is deactivated
    And they make a request with the session they already held
    Then the request is refused as not authenticated
    And the refusal says the account itself is not available
    And the refusal does not say they were removed from the account

  Scenario: Signing in again does not get a member back into a suspended account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "Cedar Realty" has been deactivated
    When "broker@cedarrealty.example" signs in again
    And they make a request with the session they were given
    Then the request is refused as not authenticated
    And they are not served anything belonging to "Cedar Realty"

  Scenario: A member of a suspended account works on in another account they belong to
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" was also added to the account "Cedar Realty" and is signed in to it
    When "Cedar Realty" is deactivated
    And they sign in again
    Then the sign-in succeeds
    And the account their requests act in is "Harbor Legal"

  Scenario: An outstanding invitation into a suspended account can no longer be accepted
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty" has invited "newcomer@cedarrealty.example", who has not accepted yet
    When "Cedar Realty" is deactivated
    And "newcomer@cedarrealty.example" accepts the invitation
    Then the invitation is refused
    And the answer says the account is not available rather than that the instance failed
    And "newcomer@cedarrealty.example" is not a member of "Cedar Realty"

  # --- What the account was for ---

  Scenario: An account's own settings apply to what a signed-in person creates
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning types are installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has turned off the rule that only one pantry may be the default one
    When they sign in and mark a second pantry as the default one
    Then both pantries are marked as the default one

  Scenario: An account that changed nothing keeps the settings the preset shipped with
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning types are installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has changed no settings
    When they sign in and mark a second pantry as the default one
    Then only the second pantry is marked as the default one

  Scenario: One account's settings do not follow a person into another account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning types are installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" has turned off the rule that only one pantry may be the default one
    When "broker@cedarrealty.example" signs in and marks a second pantry as the default one
    Then only the second pantry is marked as the default one

  @pending-steps
  Scenario: A signed-in person can reach their own knowledge graph
    Given a per-account WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    When they sign in and search the knowledge graph for "Q3 Roadmap"
    Then the knowledge graph answers rather than reporting it is not configured
    And the search results include the "project" resource "Q3 Roadmap"

  @pending-steps
  Scenario: Two signed-in people reach their own graphs and not each other's
    Given a per-account WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    And "broker@cedarrealty.example" has a "project" named "Q3 Roadmap" in "Cedar Realty"
    When both of them sign in and search the knowledge graph for "Q3 Roadmap"
    Then each of them sees only the "Q3 Roadmap" owned by their own account

  # --- The token the sign-in hands back ---

  @pending-steps
  Scenario: The token a sign-in returns names the account the session was scoped to
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" also belongs to the account "Cedar Realty"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    And "ops@harborlegal.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    When "ops@harborlegal.example" signs in
    And they make a request presenting the token the sign-in handed back
    Then the projects they see include "Q3 Roadmap"
    And the projects they see exclude "Bridge upgrade"

  @pending-steps
  Scenario: A sign-in that resolved no account hands back no token and sets no token cookie
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "stranded@harborlegal.example" signs in with password "trellis-anchor-mango-9" and belongs to no account they can act in
    When "stranded@harborlegal.example" signs in
    Then the sign-in reports no account
    And the sign-in hands back no token
    And no token cookie is set
    And the instance records that the session it made has no account to act in

  # --- The connector path reads the same account ---

  @pending-steps
  Scenario: An access token minted from a session acts in the account that session was scoped to
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the account "Cedar Realty", which "counsel@harborlegal.example" was added to and signs in to with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" also belongs to the account "Harbor Legal"
    And "counsel@harborlegal.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in to "Cedar Realty" as "counsel@harborlegal.example"
    When Claude asks the instance to authorize the connector
    And Claude exchanges the authorization code it received, presenting the proof key it started with
    Then the access token acts in "Cedar Realty"
    And what Claude reads through the instance's tools includes "Bridge upgrade"
