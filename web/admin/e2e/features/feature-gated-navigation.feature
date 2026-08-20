Feature: The admin shows only what the person signed in actually has
  As someone using the WeOS admin
  I want the sections and actions for capabilities I do not hold to be absent
  So that I am never offered a link that leads to a refusal

  This is the SPA half of #486. The other half is the API's, in the godog suite at
  tests/e2e/features/feature_flag_admin_surface.feature: what GET /api/features answers,
  to whom, and what a gated route does when somebody reaches it with the URL bar. The
  split is forced by what each suite can do. That suite can sign real people in, hold real
  sessions, and get a real 403. This one boots the real binary but runs dev auth and
  scripts its API answers with page.route, exactly as session-refusals.feature does — so
  it can prove what the admin DRAWS from an answer and cannot prove the answer is true.

  Which is the whole risk this file has to work around. Hiding is presentation. A file
  that only checked sidebar entries would pass against an admin sitting on a server with
  no gate at all, and the person reading a green suite would have no way to tell. So every
  hiding scenario below is written in a pair: the entry is gone, AND the door behind it is
  shut — opening the route directly shows no chat, and an API that refuses produces
  something the person can read rather than a broken page. The refusal those scenarios
  script is the same refusal the godog file proves the server actually sends. Neither file
  is a control on its own and both say so.

  What is pinned about cost is the choice the story offers. The set is fetched once when
  the admin boots and shared by every page; it is refetched on a known signal, and today
  there are exactly two signals the admin has — signing in, and starting or stopping
  impersonation. Account switching is the third the story names and the admin cannot do
  it: there is no account switcher and no endpoint behind one, so the account case is
  proved on the API side, where a session can be made to act in another account, and not
  faked here. Five navigations issuing one request is the measurement, the way #484 pinned
  thirty tool calls resolving once.

  The non-root baseURL problem in #352 is pinned only as far as this suite reaches, and it
  is worth saying how far that is rather than implying more. The binary embeds an admin
  built for the root of an origin, app.baseURL is fixed at build time, and there is no unit
  test runner in web/admin — so an admin mounted under /admin/ cannot be booted here at
  all. What this file pins is the root case: exactly one request goes out for the feature
  set and it lands on /api/features, not somewhere with a prefix in front of it. Making the
  non-root case testable means either a second playwright project that builds the admin
  under a prefix or a unit runner that does not exist yet, and either is #352's own work,
  not this story's. The composable must nonetheless go through the same single request
  seam every other composable will be moved onto, so that #352's fix reaches it without
  touching this feature again.

  One shipped surface is gated today and it is the Agent entry, backed by the agent-chat
  feature and the /api/agent routes. "Action buttons" appear in the story's criteria and
  have no shipped instance — there is no button in this admin whose visibility a feature
  decides. That is stated rather than invented: the composable exposes the answer in the
  form a button would need, the first gated button inherits it, and no scenario here
  pretends to cover one.

  Background:
    Given the API is scripted to serve the signed-in user

  # --- The set is read once and shared ---

  Scenario: The admin reads the feature set once and reuses it across pages
    Given the feature set is scripted with "agent-chat" on
    When the user opens the dashboard
    And the user opens the persons page
    And the user opens the users page
    And the user opens the settings page
    And the user opens the agent page
    Then the admin asked for the feature set once
    And the request for the feature set went to "/api/features"

  Scenario: A user who holds nothing gated sees nothing gated, and no errors
    Given the feature set is scripted with "agent-chat" off
    When the user opens the dashboard
    Then the sidebar does not offer "Agent"
    And the page reports no errors in the console
    And the sidebar still offers "Dashboard"

  # --- What is hidden, and what is behind it ---

  Scenario Outline: A gated entry is offered only when its feature is on
    Given the feature set is scripted with "agent-chat" <state>
    When the user opens the dashboard
    Then the sidebar <presence> "Agent"

    Examples:
      | state | presence      |
      | on    | offers        |
      | off   | does not offer |

  Scenario: The mobile menu hides what the sidebar hides
    Given the feature set is scripted with "agent-chat" off
    And the user is on a narrow screen
    When the user opens the dashboard
    And the user opens the mobile menu
    Then the mobile menu does not offer "Agent"
    And the mobile menu still offers "Dashboard"

  Scenario: Hiding the entry is not the control — the route is shut too
    Given the feature set is scripted with "agent-chat" off
    When the user opens the agent page directly by its address
    Then the conversation is not shown
    And the page explains the assistant is not available to them
    And the user is not sent to the sign-in page

  Scenario: A refusal from the API is explained rather than left as a broken page
    Given the feature set is scripted with "agent-chat" on
    And the agent API is scripted to refuse the call because the capability is not enabled
    When the user opens the agent page
    And the user sends "how many people do we know?"
    Then the page explains the assistant is not available to them
    And the conversation shows no reply
    And the user is not sent to the sign-in page

  Scenario: A capability the person does not have reads differently from one nobody configured
    Given the feature set is scripted with "agent-chat" off
    When the user opens the agent page directly by its address
    Then the page explains the assistant is not available to them
    And the page does not explain the in-app agent is not configured

  Scenario: Role-gated sections are not hidden by features, and gated sections are not shown by roles
    Given the feature set is scripted with "agent-chat" off
    And the signed-in user is an owner
    When the user opens the dashboard
    Then the sidebar offers "Users"
    And the sidebar offers "Settings"
    And the sidebar does not offer "Agent"

  # --- Refreshed on a signal, not on every page ---

  Scenario: Signing in reads the set for the person who signed in
    Given the user is signed out
    And the feature set is scripted with "agent-chat" off for a caller with no session
    And the feature set is scripted with "agent-chat" on for the signed-in user
    When the user opens the admin
    And the user signs in
    Then the sidebar offers "Agent"
    And the admin asked for the feature set again after signing in

  Scenario: Starting impersonation reads the set again for the person being impersonated
    Given the feature set is scripted with "agent-chat" on
    And the user has opened the dashboard and been offered "Agent"
    When the feature set is scripted with "agent-chat" off
    And the user starts impersonating another user
    Then the admin asked for the feature set again
    And the sidebar does not offer "Agent"

  Scenario: Stopping impersonation reads the set again and puts back what was theirs
    Given the feature set is scripted with "agent-chat" off
    And the user is impersonating another user
    And the user has opened the dashboard and not been offered "Agent"
    When the feature set is scripted with "agent-chat" on
    And the user stops impersonating
    Then the admin asked for the feature set again
    And the sidebar offers "Agent"

  Scenario: Moving between pages does not read the set again
    Given the feature set is scripted with "agent-chat" on
    And the user has opened the dashboard
    When the user opens the persons page
    And the user opens the users page
    Then the admin did not ask for the feature set again
    And the sidebar still offers "Agent"

  # --- The set the browser holds is never the authority ---

  Scenario: A set held from before a change is not permission to use what is on it
    Given the feature set is scripted with "agent-chat" on
    And the user has opened the dashboard and been offered "Agent"
    When the agent API is scripted to refuse the call because the capability is not enabled
    And the user opens the agent page
    And the user sends "how many people do we know?"
    Then the page explains the assistant is not available to them
    And the user is not sent to the sign-in page
    And the user is still signed in

  # --- Failing safe in the browser ---

  Scenario: An answer with everything off draws the ungated admin and says why
    Given the feature set is scripted with every feature off and a message that the state could not be read
    When the user opens the dashboard
    Then the sidebar does not offer "Agent"
    And the sidebar still offers "Dashboard"
    And the page explains the feature state could not be read

  Scenario: A feature set that cannot be fetched hides the gated sections rather than showing them
    Given asking for the feature set is scripted to fail
    When the user opens the dashboard
    Then the sidebar does not offer "Agent"
    And the sidebar still offers "Dashboard"
    And the persons page still loads

  Scenario: A feature set that cannot be fetched does not send the person to sign in
    Given asking for the feature set is scripted to fail
    When the user opens the persons page
    Then the user is not sent to the sign-in page
    And the persons page is shown

  # --- Nobody signed in ---

  Scenario: The sign-in page is drawn without a session and the feature request is not a refusal
    Given the user is signed out
    And the feature set is scripted with "agent-chat" off for a caller with no session
    When the user opens the admin
    Then the sign-in page is shown
    And no refusal is explained on the page
    And the page reports no errors in the console
