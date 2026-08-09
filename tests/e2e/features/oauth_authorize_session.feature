@epic-135 @story-140
Feature: Authorizing an MCP connector against the instance's own sign-in
  As someone looking at a demo WeOS instance
  I want to add it to my own Claude as an MCP connector
  So that I can use the instance from my assistant without a Google account and
  without being on anybody's allowlist

  Today the authorization endpoint has exactly one idea of who the resource
  owner is: it validates the connector's OAuth parameters and then, always,
  hands the person to Google. An instance that offers password sign-in and no
  Google provider therefore cannot complete an authorization at all, however
  well the person is signed in to it.

  This story changes *who* authenticates the resource owner, and nothing else.
  A request that already carries a valid session is one whose resource owner has
  already been authenticated, so the code is minted there and then and the
  person goes straight back to the connector. A request that carries no session
  is sent to whatever sign-in this instance has configured, with a way back to
  the authorization it interrupted — rather than to Google by name.

  Everything the protocol does stays where it is. The connector and its redirect
  URI are validated first and answered in place, because an unverified redirect
  URI must not be redirected to; every later error still goes back to the
  connector as an OAuth error; the proof key must be present and S256; the scope
  must be one this instance issues; the state comes back untouched. A session
  changes none of that, which is why several scenarios below pin the protocol
  *on the session path* specifically. They exist so that a later simplification
  of the new branch — skipping the proof key because "the person is already
  signed in" — fails here rather than in the field.

  "A valid session" means one this instance would accept anywhere else: the
  cookie decodes, and the session it names is live. Three ways it can fail to be
  valid are staged below — expired, revoked by signing out, and undecodable —
  because the two wrong answers are both plausible and both bad. Minting a code
  for a session that has expired would make sign-out meaningless for the rail
  that matters most; answering with an internal error would strand a person
  whose only mistake was leaving a tab open overnight. Both must instead be
  treated as "not signed in".

  The instance that must not regress is the one already in production on the
  Google path with OAUTH_ALLOWED_EMAILS set. Its authorization still has to
  reach Google, and its allowlist still has to decide who gets in. Two limits are
  worth stating. The allowlist gate itself sits in the Google callback, which
  cannot be driven from here without a real Google — the provider's endpoints
  are compiled in, not configured — so that gate stays pinned by the unit tests
  in internal/oauth/callback_test.go, and what these scenarios add is the part
  that is reachable: an allowlisted instance must not let the *new* path around
  the allowlist. And the final acceptance criterion, adding the instance in
  Claude's own connector UI, is manual by nature. What is automated here is
  everything that UI does over the wire — discovery, dynamic registration,
  authorization, token exchange, and then listing and calling tools with the
  token that came out — so a manual pass confirms the UI, not the protocol.

  The instance is a demo: every viewer's Claude connects as the same bootstrap
  account and they see each other's writes. That is the decided expectation, not
  an oversight, and the last scenario says so out loud.

  # --- The instance is an authorization server at all ---

  Scenario: A password-only instance tells a connector how to authorize
    Given a demo instance where password sign-in is enabled, no Google provider is configured, and dynamic client registration is on
    When a connector asks the instance how to authorize against it
    Then the instance names itself as the authorization server
    And it offers to register connectors on the spot
    And it requires the S256 proof-key method

  Scenario: A connector registers itself on a password-only instance
    Given a demo instance where password sign-in is enabled, no Google provider is configured, and dynamic client registration is on
    When Claude registers itself as a connector with the redirect URI "https://claude.ai/api/mcp/auth_callback"
    Then the registration succeeds
    And the connector is issued its own client identifier
    And the redirect URI it registered is the one the instance will send people back to

  Scenario: With dynamic registration off, a connector has no way to register itself
    Given a demo instance where password sign-in is enabled, no Google provider is configured, and dynamic client registration is off
    When a connector asks the instance how to authorize against it
    Then the instance offers no way to register connectors on the spot
    When Claude registers itself as a connector with the redirect URI "https://claude.ai/api/mcp/auth_callback"
    Then the registration is refused

  # --- A request that already carries a session ---

  Scenario: A signed-in person's authorization comes straight back to the connector
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector
    Then the person is sent back to Claude with an authorization code
    And the state Claude sent is returned with it
    And the person is never sent to Google

  Scenario: The code from a signed-in authorization exchanges for a working access token
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector
    And Claude exchanges the authorization code it received, presenting the proof key it started with
    Then Claude receives an access token
    And the token is issued for the scope Claude asked for

  Scenario: The access token acts as the account that was signed in
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And a second account "counsel@harborlegal.example" with password "trellis-anchor-mango-9"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "counsel@harborlegal.example"
    When Claude asks the instance to authorize the connector
    And Claude exchanges the authorization code it received, presenting the proof key it started with
    Then the access token acts as "counsel@harborlegal.example"

  Scenario: A code minted from a session can only be exchanged once
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector
    And Claude exchanges the authorization code it received, presenting the proof key it started with
    And Claude exchanges the same authorization code a second time
    Then the second exchange is refused
    And no second access token is issued

  Scenario: A code minted from a session still requires the proof key it was minted with
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector
    And someone else exchanges the authorization code presenting a proof key of their own
    Then the exchange is refused
    And no access token is issued

  Scenario Outline: A signed-in person gets no code for an authorization the protocol rejects
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector <flaw>
    Then Claude is sent the error "<error>" instead of an authorization code
    And the state Claude sent is returned with the error

    Examples:
      | flaw                                          | error                     |
      | asking for a token rather than a code         | unsupported_response_type |
      | with no proof key at all                      | invalid_request           |
      | with an unhashed proof key                    | invalid_request           |
      | asking for a scope this instance never issues | invalid_scope             |

  Scenario: An unknown connector is refused in place even for a signed-in person
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When a connector the instance has never registered asks it to authorize
    Then the instance answers the request itself rather than redirecting anywhere
    And it reports that the client is not one it knows
    And no authorization code is issued

  Scenario: A redirect URI the connector never registered is refused in place, not redirected to
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "demo@harborlegal.example"
    When Claude asks the instance to authorize the connector, naming a redirect URI it never registered
    Then the instance answers the request itself rather than redirecting anywhere
    And the person is not sent to the unregistered redirect URI
    And no authorization code is issued

  Scenario: An unknown connector is refused in place rather than sent to sign in
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the person adding the connector is not signed in
    When a connector the instance has never registered asks it to authorize
    Then the instance answers the request itself rather than redirecting anywhere
    And the person is not sent to a sign-in page

  # --- A request that carries no session ---

  Scenario: An authorization with no session goes to the instance's own sign-in
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is not signed in
    When Claude asks the instance to authorize the connector
    Then the person is sent to a sign-in page on this instance
    And the person is not sent to Google
    And Claude is not sent an authorization code

  Scenario: Signing in returns the person to the authorization they interrupted
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector is not signed in
    And Claude has asked the instance to authorize the connector
    When the person signs in as "demo@harborlegal.example" and follows the way back they were given
    Then the person is sent back to Claude with an authorization code
    And the person is not asked to sign in a second time

  Scenario: A session that names no account is treated as no session
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector signed in as "demo@harborlegal.example" and their session names no account
    When Claude asks the instance to authorize the connector
    Then the person is sent to a sign-in page on this instance
    And Claude is not sent an authorization code
    And the instance does not report a failure of its own

  Scenario: An expired session is treated as no session
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector signed in as "demo@harborlegal.example" and their session has since expired
    When Claude asks the instance to authorize the connector
    Then the person is sent to a sign-in page on this instance
    And Claude is not sent an authorization code
    And the instance does not report a failure of its own

  Scenario: A browser that has signed out is treated as no session
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector signed in as "demo@harborlegal.example" and then signed out, keeping the cookie they were left with
    When Claude asks the instance to authorize the connector
    Then the person is sent to a sign-in page on this instance
    And Claude is not sent an authorization code

  Scenario: A session cookie the instance cannot read is treated as no session
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude is registered as a connector on that instance
    And the person adding the connector carries a session cookie this instance cannot read
    When Claude asks the instance to authorize the connector
    Then the person is sent to a sign-in page on this instance
    And Claude is not sent an authorization code
    And the instance does not report a failure of its own

  # --- The instance already in production must not regress ---

  Scenario: On a Google instance an unauthenticated authorization still ends at Google
    Given an instance where Google sign-in is configured and the allowlist names "ops@harborlegal.example"
    And Claude is registered as a connector on that instance
    And the person adding the connector is not signed in
    When Claude asks the instance to authorize the connector
    Then the person ends up at Google's sign-in
    And Claude is not sent an authorization code yet

  Scenario: On a Google instance an allowlisted person who is already signed in is not sent to Google again
    Given an instance where Google sign-in is configured and the allowlist names "ops@harborlegal.example"
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "ops@harborlegal.example"
    When Claude asks the instance to authorize the connector
    Then the person is sent back to Claude with an authorization code
    And the person is never sent to Google

  Scenario: An allowlisted instance is not opened up by signing in another way
    Given an instance where Google sign-in is configured and the allowlist names "ops@harborlegal.example"
    And password sign-in is also enabled on that instance
    And the account "stranger@harborlegal.example" with password "correct-horse-battery-staple", whom the allowlist does not name
    And Claude is registered as a connector on that instance
    And the person adding the connector is signed in as "stranger@harborlegal.example"
    When Claude asks the instance to authorize the connector
    Then Claude is not sent an authorization code
    And nothing Claude can present afterwards acts as "stranger@harborlegal.example"

  # --- What the connector is for ---

  Scenario: A connector authorized on a password-only instance can list the instance's tools
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude has been authorized as a connector by "demo@harborlegal.example" signing in
    When Claude asks the instance which tools it offers
    Then the instance lists the tools it offers

  Scenario: A connector authorized on a password-only instance can call a tool
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And Claude has been authorized as a connector by "demo@harborlegal.example" signing in
    When Claude creates the task "File the quarterly compliance report" through the instance's tools
    Then the task "File the quarterly compliance report" is one of the demo account's tasks

  Scenario: Two people who add the demo connector share the one demo identity
    Given a demo instance where password sign-in is enabled and no Google provider is configured
    And the bootstrap account "demo@harborlegal.example" with password "correct-horse-battery-staple"
    And two people have each authorized their own Claude by signing in as "demo@harborlegal.example"
    When the first person's Claude creates the task "File the quarterly compliance report" through the instance's tools
    Then the second person's Claude sees the task "File the quarterly compliance report"
    And both connectors act as "demo@harborlegal.example"
