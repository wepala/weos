Feature: How the admin answers a refused session
  As someone using the WeOS admin
  I want to be told what actually went wrong when my session is refused
  So that I am not sent to a sign-in page that cannot fix it

  The API refuses a session with 401 and, for three of the four reasons, a code in
  the body. The reasons need opposite handling: signing in again fixes an expired
  session, may fix one whose account access was taken away, and cannot fix either
  of the other two. Today every 401 sends the person to the sign-in page, which
  for a session with no account is a loop with no exit. The negative steps below
  are the point of this feature — a UI that redirects on any 401 has to fail them.

  Scenario: An expired session still sends the person to sign in
    Given the API is scripted to refuse the session with no code
    When the user opens the persons page
    Then the user is sent to the sign-in page

  Scenario: A session with no account does not send the person to sign in
    Given the API is scripted to refuse the session with the code "unscoped_session"
    When the user opens the persons page
    Then the user is not sent to the sign-in page
    And the page explains they have no account to work in
    And the page does not offer to sign in again

  Scenario: Trying again after a no-account refusal still does not bounce the person
    Given the API is scripted to refuse the session with the code "unscoped_session"
    When the user opens the persons page
    And the user opens the users page
    And the user reloads the page
    Then the user is not sent to the sign-in page
    And the page explains they have no account to work in

  Scenario: Access taken away is explained, and both remedies are offered
    Given the API is scripted to refuse the session with the code "account_access_revoked"
    When the user opens the persons page
    Then the page explains their access to the account was taken away
    And the page offers to try again
    And the page offers to sign in again

  Scenario: A refusal that succeeds on the next try leaves the person working normally
    Given the API is scripted to refuse the session once with the code "account_access_revoked", then serve it
    When the user opens the persons page
    And the user takes the offer to try again
    Then the persons page is shown
    And no refusal is explained on the page

  Scenario: A suspended account is explained, and signing in again is not offered
    Given the API is scripted to refuse the session with the code "account_deactivated"
    When the user opens the persons page
    Then the page explains the account is suspended and an operator has to turn it back on
    And the page does not offer to sign in again
    And the user is not sent to the sign-in page

  Scenario: The suspension stays explained while the person stays in the admin
    Given the API is scripted to refuse the session with the code "account_deactivated"
    And the user has opened the persons page
    When the API is scripted to refuse the session with the code "unscoped_session"
    And the user moves to the users page without leaving the admin
    Then the page still explains the account is suspended
    And the page does not explain only that they have no account to work in
    And once the user reloads the page, the suspension is no longer explained

  Scenario: A refusal while the person is working does not throw them out to sign in
    Given the API is scripted to serve the signed-in user
    And loading persons is scripted to refuse the session with the code "unscoped_session"
    When the user opens the persons page
    Then the user is not sent to the sign-in page
    And the page explains they have no account to work in

  Scenario: An expired session while the person is working sends them to sign in
    Given the API is scripted to serve the signed-in user
    And loading persons is scripted to refuse the session with no code
    When the user opens the persons page
    Then the user is sent to the sign-in page
