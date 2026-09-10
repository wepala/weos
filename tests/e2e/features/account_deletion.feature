@story-wm-kb6sg-3
Feature: A person can delete their account from the app
  As an account holder
  I want to delete my account from inside the app
  So that nothing of mine remains on the instance afterwards

  Nothing on a WeOS instance has been able to remove an account. What existed was a lock:
  a deactivated account refuses every session and every sign-in, and its data stays where
  it was. The app stores that make WeHungry possible require the opposite — a person who
  creates an account in the app must be able to delete it, and its data, from inside the
  app. This feature is that route, the service behind it, and the operator command that
  finishes a deletion the person can no longer reach.

  Deletion is a hard delete of every store that holds something of the account: its
  events, its projection rows, its graph triples, its files, its sessions, its members'
  memberships, the connector codes and tokens that grant access to it, and its feature
  grants. The scenarios that assert "nothing remains" do not ask the deletion service what
  it removed. They query every table by the account's own identifiers — recorded before
  the deletion — so that a store the service forgot fails here rather than in the field.

  Three things about the request are decided here and were open in the design record.
  The body field is "confirm", and its value is the word DELETE exactly: the design's
  "confirmation" lost to the story's wording, and the app's delete button sends "confirm".
  Anything else is 400 and changes nothing. Only an owner or an admin of the account may
  delete it — an ordinary member is refused, because the account is not theirs to end.
  And a request made while an administrator is impersonating somebody is refused
  outright, so an instance admin cannot erase a person's account through their identity.

  An account may have several members, and erasing it takes the account away from every
  one of them. That is not left silent. The identity the app reads before it offers
  deletion reports how many people share the account, so the app can say so before the
  person confirms, and the answer to the deletion reports how many people lost it. A
  member who belongs to another account lands there on their next sign-in; a member who
  belonged to nothing else is gone with it — their credential is removed, and the email
  can register afresh into a new, empty account.

  What a deleted account's person meets afterwards is decided by which credential they
  hold. The session cookie the deletion cleared is refused as not signed in — plainly, with
  no code, the same as any session that no longer exists — so a second DELETE with it is
  401. A bearer token issued before the deletion stops authenticating, because the bearer
  path now makes the one account lookup the session path always made; a write through it
  cannot recreate rows or a graph directory under the deleted account's id. A caller the
  instance still authenticates whose account is gone is answered 404 by the route itself;
  the suite cannot stage that caller deterministically over HTTP, so that answer is pinned
  by a unit test of the handler and the scenario for it is tagged @unit-pinned.

  Recovery is the part the design premortem ranked first. The service locks the account
  before it removes anything, then drains the background projections, then deletes the
  external stores, then deletes every SQL row in one transaction with the account row
  last. A failure anywhere leaves a locked account with its SQL state intact, and a re-run
  finishes it. The person is not stranded by that lock: a sign-in with their credential
  succeeds for exactly one purpose, running the deletion again, and every other request in
  the locked account is refused with a code that names the unfinished deletion — distinct
  from the code a suspended account gets, because an app has to offer "finish deleting"
  for one and nothing for the other. A suspended account gets no such path.

  The export offered beside the delete button holds the account's recipes and nothing
  else — no pantry, no photos, no meal logs — and the document says so about itself, so
  the app can tell the person what they are about to keep. It filters on the account
  explicitly rather than on what the caller may read, because a person who belongs to two
  accounts must not be handed the other account's recipes as their own.

  Files stored before uploads had an owner keep their flat keys. They cannot be attributed
  to any account, so deletion does not touch them: a flat file might be anyone's, and
  removing it would be erasing somebody else's data. Moving them under their accounts is
  tracked separately, and the scenario that pins their survival is here so that a later
  change to it is deliberate.

  Four properties cannot be exercised from this suite and are pinned by unit tests the
  implementation names in its report: the drain's bounded wait and the failed deletion it
  turns into when a subscriber group never reaches head; the GCS and S3 prefix deletes
  (listing by prefix, batches of 1000, errors joined and never best-effort); the chunking
  of the SQL deletes under SQLite's bound-parameter cap; and the constraint that the
  event sweep by shared transaction only ever takes events of aggregates being deleted.
  The scenarios tagged @unit-pinned state those properties so they are read here; the
  suite excludes the tag. Scenarios tagged @requires-embedded-graph need the embedded
  store built with -tags oxigraph_embedded, as in per_account_knowledge_graph.feature.

  # --- The request, and who may make it ---

  Scenario: An owner who confirms with the word DELETE is signed out with their account gone
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the deletion is accepted
    And the answer clears the session cookie and the token cookie
    And the answer reports that 1 person lost the account
    And "ops@harborlegal.example" cannot sign in with password "correct-horse-battery-staple"

  Scenario Outline: A body that does not confirm with the word DELETE changes nothing
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    When "ops@harborlegal.example" asks to delete their account sending the body "<body>"
    Then the request is refused as a bad request
    And they make a request with the session they already held
    And the request is served
    And the recipe "Sunday Lasagna" is still there for "Harbor Legal"

    Examples:
      | body                       |
      | {"confirm":"delete"}       |
      | {"confirm":"DELETE "}      |
      | {"confirmation":"DELETE"}  |
      | {"confirm":""}             |
      | {}                         |
      |                            |

  Scenario: A request carrying no session cannot delete anything
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When someone carrying no session asks to delete an account, confirming with "DELETE"
    Then the request is refused as not authenticated
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"

  Scenario: An ordinary member cannot delete the account they were added to
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" belongs to "Cedar Realty" as an ordinary member
    And "counsel@harborlegal.example" is signed in to "Cedar Realty"
    When "counsel@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the request is refused as forbidden
    And "counsel@harborlegal.example" is still a member of "Cedar Realty"
    And "broker@cedarrealty.example" can sign in with password "trellis-anchor-mango-9"

  Scenario: An admin who is not the owner may delete the account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" also belongs to "Cedar Realty" with the role "admin"
    And "counsel@harborlegal.example" is signed in to "Cedar Realty"
    When "counsel@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the deletion is accepted
    And the answer reports that 2 people lost the account
    And "broker@cedarrealty.example" cannot sign in with password "trellis-anchor-mango-9"

  Scenario: An administrator impersonating somebody cannot delete their account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" is an instance admin
    And the account "Cedar Realty", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    When "ops@harborlegal.example" impersonates "counsel@harborlegal.example"
    And they ask to delete the account they are acting in, confirming with "DELETE"
    Then the request is refused as forbidden
    And "counsel@harborlegal.example" can sign in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"

  Scenario: Before deleting, the app can learn how many people share the account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" belongs to "Cedar Realty" as an ordinary member
    And "newcomer@cedarrealty.example" belongs to "Cedar Realty" as an ordinary member
    When "broker@cedarrealty.example" signs in
    And they read who they are signed in as
    Then the answer says the account they act in has 3 members

  # --- What deletion removes ---

  Scenario: After deleting and signing in again, the pantry is empty and the old photo is gone
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And "ops@harborlegal.example" signs in through the provider "google" and owns the account "Harbor Legal"
    And "Harbor Legal" has a pantry holding "Basmati rice" and a stored photo named "lasagna.jpg"
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    And "ops@harborlegal.example" signs in again through the provider "google"
    Then the account their requests act in is not the one they deleted
    And their pantry holds nothing
    And the photo's old URL is refused as not found

  Scenario: The password that opened a deleted account no longer signs in
    Given a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    And "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in is refused as if the person had never registered

  Scenario: The email of a deleted account can register afresh into an empty account
    Given a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And "ops@harborlegal.example" has deleted their account
    When "ops@harborlegal.example" registers with password "correct-horse-battery-staple"
    Then the registration succeeds
    And the account their requests act in is not the one they deleted
    And the recipes they see exclude "Sunday Lasagna"

  Scenario: Every store the account touched is empty of it afterwards
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "Harbor Legal" holds one of everything an account can own: a recipe, a pantry item, a stored photo, a feature grant, an authorized connector and an outstanding invitation
    And every identifier of "Harbor Legal" has been noted: its id, its resource URNs, its event ids and its members' agent ids
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then nothing of "Harbor Legal" remains in any store on the instance:
      | store                                         | looked up by                               |
      | events                                        | the account id in the payload              |
      | parked events                                 | the account id in the payload              |
      | resources and every projection table          | account_id                                 |
      | triples                                       | subject in the noted resource URNs         |
      | event_references                              | the noted resource URNs                    |
      | resource_permissions                          | the noted resource ids                     |
      | behavior_settings, feature_grants             | account_id                                 |
      | feature_settings                              | scope_id                                   |
      | accounts, account_members, auth_sessions      | account id                                 |
      | invites                                       | account id                                 |
      | oauth_authorization_codes, oauth_refresh_tokens | account id, and the noted agent ids      |
      | casbin grouping policies                      | the account id as the domain               |
      | agents, credentials, password_credentials     | the noted agent ids left with no account   |
      | the file store                                | the folder named after the account id      |

  Scenario: Another account on the same instance is untouched by the deletion
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "broker@cedarrealty.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    And "Cedar Realty" has a stored photo named "dal.jpg"
    And "broker@cedarrealty.example" is signed in and their requests are being served
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the projects "broker@cedarrealty.example" sees with the session they already held include "Bridge upgrade"
    And "Cedar Realty" can still read its photo "dal.jpg" by its URL

  # Needs the embedded graph: -tags oxigraph_embedded, CGO, and the static
  # library `make fetch-oxigraph-lib` downloads. The normal test job does not
  # build that, so these live behind their own tag rather than unwritten.
  @requires-embedded-graph
  Scenario: A per-account graph directory is removed with its account
    Given a per-account WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    And "Harbor Legal" has a knowledge-graph store directory of its own
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then no knowledge-graph store directory exists for "Harbor Legal"
    And no knowledge-graph store directory is created for it by a later request

  @requires-embedded-graph
  Scenario: In a single-tenant graph only the deleted account's subjects are removed
    Given a WeOS instance with one shared knowledge-graph store where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "ops@harborlegal.example" has a "project" named "Q3 Roadmap" in "Harbor Legal"
    And "broker@cedarrealty.example" has a "project" named "Bridge upgrade" in "Cedar Realty"
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the knowledge graph no longer returns the "project" resource "Q3 Roadmap" owned by account "Harbor Legal"
    And the knowledge graph still returns the "project" resource "Bridge upgrade" owned by account "Cedar Realty"

  # Accepted at the plan gate: flat files predate ownership and cannot be
  # attributed, so deletion leaves them alone. Moving them is bead wm-snzgr.
  Scenario: A file stored before files had an owner survives the deletion at its flat URL
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And a photo "old-menu.jpg" was stored at its flat URL before files were kept by account
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    And "broker@cedarrealty.example" signs in
    And "broker@cedarrealty.example" requests "old-menu.jpg" at its flat URL
    Then the photo is served

  # --- Sharing an account, and sharing a person ---

  # wm-atptf: events are enumerated by the account in their payload together
  # with every event sharing a transaction, and a person's agent, credential
  # and membership events are committed in one transaction. A person shared
  # between two accounts is where a careless sweep takes the survivor's history.
  Scenario: Deleting an account a person shares leaves their other account's history whole
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And the account "Cedar Realty" has invited "counsel@harborlegal.example", who accepted and now also belongs to it
    When the owner of "Cedar Realty" deletes it, confirming with "DELETE"
    And the projections are rebuilt from event history
    And "counsel@harborlegal.example" signs in again
    Then the recipes they see include "Sunday Lasagna"

  Scenario: A member with another account is told the account went away
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" was also added to the account "Cedar Realty" and is signed in to it
    When the owner of "Cedar Realty" deletes it, confirming with "DELETE"
    And "counsel@harborlegal.example" makes a request with the session they already held
    Then the request is refused as not authenticated
    And the refusal says their access to the account was taken away

  Scenario: A member with another account lands in it on their next sign-in
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And "counsel@harborlegal.example" was also added to the account "Cedar Realty" and is signed in to it
    And the account "Cedar Realty" has been deleted by its owner
    When "counsel@harborlegal.example" signs in again
    Then the sign-in succeeds
    And the account their requests act in is "Harbor Legal"

  # wm-tqi1q: the member who belonged to nothing else. Their credential goes
  # with the account, so the old password is refused the way an unknown one
  # is, and the address registers into a new, empty account.
  Scenario: A member who belonged to nothing else is gone with the account
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And the account "Cedar Realty" has invited "newcomer@cedarrealty.example", who accepted with password "correct-horse-battery-staple" and belongs to nothing else
    When "broker@cedarrealty.example" deletes their account, confirming with "DELETE"
    Then the answer reports that 2 people lost the account
    And "newcomer@cedarrealty.example" cannot sign in with password "correct-horse-battery-staple"

  Scenario: A member who belonged to nothing else may start over with the same email
    Given a WeOS instance where password sign-in and registration are enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And the account "Cedar Realty" has invited "newcomer@cedarrealty.example", who accepted with password "correct-horse-battery-staple" and belongs to nothing else
    And the account "Cedar Realty" has been deleted by its owner
    When "newcomer@cedarrealty.example" registers with password "correct-horse-battery-staple"
    Then the registration succeeds
    And the account their requests act in is not "Cedar Realty"

  Scenario: An invitation into a deleted account can no longer be accepted
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And the account "Cedar Realty" has invited "newcomer@cedarrealty.example", who has not accepted yet
    When "broker@cedarrealty.example" deletes their account, confirming with "DELETE"
    And "newcomer@cedarrealty.example" accepts the invitation
    Then the invitation is refused
    And the answer says the invitation is not one the instance knows rather than that the instance failed
    And "newcomer@cedarrealty.example" is not a member of "Cedar Realty"

  # --- Sessions, tokens and a second deletion ---

  Scenario: A second deletion with the cookie the first one cleared is refused as not signed in
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has deleted their account, keeping the cookie they held before
    When "ops@harborlegal.example" deletes their account again with that cookie, confirming with "DELETE"
    Then the request is refused as not authenticated
    And the refusal carries no code

  Scenario: A session open on another device is signed out by the deletion
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" is signed in on a second device as well
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    And they make a request with the session on the second device
    Then the request is refused as not authenticated

  Scenario: A connector's token issued before the deletion stops authenticating
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude has been authorized as a connector by "demo@harborlegal.example" signing in
    When "demo@harborlegal.example" deletes their account, confirming with "DELETE"
    And Claude asks the instance which tools it offers
    And Claude creates the task "File the quarterly compliance report" through the instance's tools
    Then both of Claude's requests are refused as not authenticated
    And no row and no graph store directory names the deleted account afterwards

  Scenario: A connector's refresh token issued before the deletion can no longer be exchanged
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude has been authorized as a connector by "demo@harborlegal.example" signing in
    When "demo@harborlegal.example" deletes their account, confirming with "DELETE"
    And Claude presents the refresh token it was issued for a new access token
    Then the exchange is refused
    And no access token is issued

  # The connector registration names no account, so it is a shared row: it
  # goes only when no code or token from any account still names it.
  Scenario: A connector registration another account still uses survives the deletion
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And a second account "counsel@harborlegal.example" with password "trellis-anchor-mango-9"
    And Claude has been authorized as a connector by "demo@harborlegal.example" signing in
    And Claude has also been authorized as a connector by "counsel@harborlegal.example" signing in
    When "demo@harborlegal.example" deletes their account, confirming with "DELETE"
    And Claude, acting as "counsel@harborlegal.example", asks the instance which tools it offers
    Then the instance lists the tools it offers
    And Claude is still registered as a connector on that instance

  # The route answers 404 when the caller authenticates but the account is
  # gone. Every credential the suite can hold is refused before the handler
  # runs, so this is pinned by a unit test of the handler, not staged here.
  @unit-pinned
  Scenario: A caller the instance still authenticates is told a gone account is not found
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "ops@harborlegal.example" holds a session naming an account that has already been erased
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the request is refused as not found

  # --- A deletion that fails part-way, and the way back ---

  # wm-421nl. The file store is the external step the suite can make fail; it
  # stands for any failure after the lock and before the SQL commit.
  Scenario: A deletion that fails part-way locks the account and says the deletion is unfinished
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the file store will refuse to remove the folder of "Harbor Legal" the first time it is asked
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the answer says the deletion did not finish and can be run again
    And they make a request with the session they already held
    And the request is refused as not authenticated
    And the refusal says the account's deletion is unfinished

  Scenario: A deletion that fails part-way leaves the account's data for the re-run
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And the file store will refuse to remove the folder of "Harbor Legal" the first time it is asked
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the answer says the deletion did not finish and can be run again
    And the recipe "Sunday Lasagna" is still stored for "Harbor Legal"
    And the account "Harbor Legal" is still stored, marked as being erased

  Scenario: Signing in to a locked account succeeds and says the deletion is unfinished
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And a deletion of "Harbor Legal" failed after the lock was taken, leaving the account locked
    When "ops@harborlegal.example" signs in again
    Then the sign-in succeeds
    And the sign-in says the account's deletion is unfinished

  Scenario: A locked account serves nothing but the deletion
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And a deletion of "Harbor Legal" failed after the lock was taken, leaving the account locked
    When "ops@harborlegal.example" signs in again
    And they list the projects they can see
    Then the request is refused as not authenticated
    And the refusal says the account's deletion is unfinished

  Scenario: Running the deletion again from a fresh sign-in finishes it
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "Harbor Legal" holds one of everything an account can own: a recipe, a pantry item, a stored photo, a feature grant, an authorized connector and an outstanding invitation
    And every identifier of "Harbor Legal" has been noted: its id, its resource URNs, its event ids and its members' agent ids
    And a deletion of "Harbor Legal" failed after the lock was taken, leaving the account locked
    When "ops@harborlegal.example" signs in again
    And "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the deletion is accepted
    And nothing of "Harbor Legal" remains in any store on the instance

  # wm-iiasy. The lock holds against an instance admin too: impersonating a
  # member of a locked account does not open it.
  Scenario: An administrator impersonating a member of a locked account is refused
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" is an instance admin
    And the account "Cedar Realty", whose owner "counsel@harborlegal.example" signs in with password "trellis-anchor-mango-9"
    And a deletion of "Cedar Realty" failed after the lock was taken, leaving the account locked
    When "ops@harborlegal.example" impersonates "counsel@harborlegal.example"
    And they list the projects they can see
    Then the request is refused as not authenticated
    And the refusal says the account's deletion is unfinished

  # wm-or9a5. WeHungry signs people in through Google and Apple, and a
  # provider sign-in resolves no active account for a locked one, so the
  # session it makes names no account. The deletion route admits the owner
  # of a locked account from that session all the same.
  Scenario: A person who signs in through a provider can finish a deletion that failed part-way
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And "ops@harborlegal.example" signs in through the provider "google" and owns the account "Harbor Legal"
    And "Harbor Legal" holds one of everything an account can own: a recipe, a pantry item, a stored photo, a feature grant, an authorized connector and an outstanding invitation
    And every identifier of "Harbor Legal" has been noted: its id, its resource URNs, its event ids and its members' agent ids
    And a deletion of "Harbor Legal" failed after the lock was taken, leaving the account locked
    When "ops@harborlegal.example" signs in again through the provider "google"
    And "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the deletion is accepted
    And nothing of "Harbor Legal" remains in any store on the instance

  Scenario: A suspended account gets no deletion path from a fresh sign-in
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "Cedar Realty" has been deactivated
    When "broker@cedarrealty.example" signs in again
    And "broker@cedarrealty.example" deletes their account, confirming with "DELETE"
    Then the request is refused as not authenticated
    And the refusal says the account itself is not available
    And the refusal does not say the account's deletion is unfinished
    And "broker@cedarrealty.example" is still a member of "Cedar Realty"

  # Staging a group that never reaches head means handing the server a broken
  # subscriber, which demonstrates the harness rather than the product. The
  # bounded wait and the lock it leaves are pinned by unit tests of the service.
  @unit-pinned
  Scenario: A background projection that never catches up fails the deletion and keeps the lock
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And a background projection group that never reaches the head of the event log
    When "ops@harborlegal.example" deletes their account, confirming with "DELETE"
    Then the answer says the deletion did not finish and can be run again
    And the refusal every later request gets says the account's deletion is unfinished
    And every store still holds what it held before the deletion was attempted

  # --- The operator finishes what the person cannot ---

  Scenario: An operator deletes an account from the command line
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the account "Cedar Realty", whose owner "broker@cedarrealty.example" signs in with password "trellis-anchor-mango-9"
    And "Harbor Legal" holds one of everything an account can own: a recipe, a pantry item, a stored photo, a feature grant, an authorized connector and an outstanding invitation
    And every identifier of "Harbor Legal" has been noted: its id, its resource URNs, its event ids and its members' agent ids
    When the operator runs "weos account delete <the id of Harbor Legal> --confirm"
    Then the command exits successfully
    And nothing of "Harbor Legal" remains in any store on the instance
    And "broker@cedarrealty.example" can sign in with password "trellis-anchor-mango-9"

  Scenario: The operator command refuses to delete without the confirm flag
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When the operator runs "weos account delete <the id of Harbor Legal>"
    Then the command exits with a failure
    And the failure names the confirm flag as what was missing
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"

  Scenario: The operator command names an account it cannot find
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When the operator runs "weos account delete 2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA --confirm"
    Then the command exits with a failure
    And the failure names the account id it could not find
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"

  Scenario: An operator finishes a deletion that failed part-way
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "Harbor Legal" holds one of everything an account can own: a recipe, a pantry item, a stored photo, a feature grant, an authorized connector and an outstanding invitation
    And every identifier of "Harbor Legal" has been noted: its id, its resource URNs, its event ids and its members' agent ids
    And a deletion of "Harbor Legal" failed after the lock was taken, leaving the account locked
    When the operator runs "weos account delete <the id of Harbor Legal> --confirm"
    Then the command exits successfully
    And nothing of "Harbor Legal" remains in any store on the instance
    And "ops@harborlegal.example" cannot sign in with password "correct-horse-battery-staple"

  # --- Exporting recipes before deleting ---

  Scenario: The export hands back the account's recipes as one JSON-LD document
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And "ops@harborlegal.example" has a "recipe" named "Weeknight Dal" in "Harbor Legal"
    When "ops@harborlegal.example" exports their account
    Then the export is served as JSON-LD with a context and a graph
    And the export's graph holds the recipes "Sunday Lasagna" and "Weeknight Dal"
    And the export says of itself that it holds recipes and nothing else

  Scenario: The export holds recipes only, and says so
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And "Harbor Legal" has a pantry holding "Basmati rice" and a stored photo named "lasagna.jpg"
    When "ops@harborlegal.example" exports their account
    Then the export's graph holds the recipes "Sunday Lasagna"
    And the export's graph holds no pantry, no food item and no photo
    And the export says of itself that it holds recipes and nothing else

  Scenario: An account with no recipes exports an empty graph that still names its scope
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" exports their account
    Then the export is served as JSON-LD with a context and a graph
    And the export's graph is empty
    And the export says of itself that it holds recipes and nothing else

  Scenario: The export holds only the recipes of the account the session acts in
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the meal-planning preset is installed
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" also belongs to the account "Cedar Realty"
    And "ops@harborlegal.example" has a "recipe" named "Sunday Lasagna" in "Harbor Legal"
    And "ops@harborlegal.example" has a "recipe" named "Weeknight Dal" in "Cedar Realty"
    When "ops@harborlegal.example" exports their account
    Then the export's graph holds the recipes "Sunday Lasagna"
    And the export's graph does not hold the recipe "Weeknight Dal"

  Scenario: An export request carrying no session is refused
    Given a WeOS instance where password sign-in is enabled and requests are authenticated by their session
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    When someone carrying no session asks for an account export
    Then the request is refused as not authenticated
