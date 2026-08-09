// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Steps for how the admin answers a refused session.
//
// Every 401 here is scripted with page.route. The test server runs dev auth
// with no OAuth provider, so /api/auth/me always succeeds against it and the
// real API never produces these codes — which is correct, because the wire
// shape is pinned by the godog suite in tests/e2e. What this file pins is the
// SPA's reaction to it.

import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { createBdd } from 'playwright-bdd'

const { Given, When, Then } = createBdd()

// The reload step is worded "reloads the admin" rather than "reloads the
// page": agent-chat already owns the latter and its implementation asserts
// the agent page is showing, which is not what these scenarios reload into.

const REFUSAL = '[data-testid="session-refusal"]'
const REFUSAL_TEXT = '[data-testid="session-refusal-text"]'
const RETRY = '[data-testid="session-refusal-retry"]'
const SIGN_IN = '[data-testid="session-refusal-signin"]'

// A refused session, shaped the way pericarp's RequireAuth writes it: 401 with
// `error`, plus `code` for the three refusals that carry one.
function refusal(code?: string) {
  return {
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify(code ? { error: 'not authenticated', code } : { error: 'not authenticated' }),
  }
}

const SIGNED_IN_USER = {
  status: 200,
  contentType: 'application/json',
  body: JSON.stringify({ data: { id: 'agent-1', name: 'Ada', email: 'ada@example.com' } }),
}

// refuseEverything answers every API call with the same refusal, which is the
// shape of a session that is refused at the door.
// The pattern is cleared first so a scenario can re-script mid-flight — one
// says the refusal changes while the person stays in the admin — without the
// old handler lingering underneath.
async function refuseEverything(page: Page, code?: string) {
  await page.unroute('**/api/**')
  await page.route('**/api/**', async (route) => route.fulfill(refusal(code)))
}

/** Serve the identity call but refuse a named collection — a refusal that
 *  arrives after the person is already through the auth gate. */
async function serveUserButRefuse(page: Page, collection: string, code?: string) {
  await page.route('**/api/**', async (route) => {
    const url = route.request().url()
    if (url.includes('/api/auth/me')) {
      await route.fulfill(SIGNED_IN_USER)
      return
    }
    if (url.includes(`/api/${collection}`)) {
      await route.fulfill(refusal(code))
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: [] }) })
  })
}

Given('the API is scripted to refuse the session with no code', async ({ page }) => {
  await refuseEverything(page)
})

Given('the API is scripted to refuse the session with the code {string}', async ({ page }, code: string) => {
  await refuseEverything(page, code)
})

// The transient case: refused once, served afterwards. account_access_revoked
// is recomputed per request, so a membership that was briefly invisible comes
// back by itself — the retry affordance exists for exactly this.
Given(
  'the API is scripted to refuse the session once with the code {string}, then serve it',
  async ({ page }, code: string) => {
    let refused = false
    await page.route('**/api/**', async (route) => {
      if (!refused) {
        refused = true
        await route.fulfill(refusal(code))
        return
      }
      if (route.request().url().includes('/api/auth/me')) {
        await route.fulfill(SIGNED_IN_USER)
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: [] }) })
    })
  },
)

Given('the API is scripted to serve the signed-in user', async ({ page }) => {
  await page.route('**/api/auth/me', async (route) => route.fulfill(SIGNED_IN_USER))
})

Given('loading persons is scripted to refuse the session with the code {string}', async ({ page }, code: string) => {
  await serveUserButRefuse(page, 'person', code)
})

Given('loading persons is scripted to refuse the session with no code', async ({ page }) => {
  await serveUserButRefuse(page, 'person')
})

Given('the user has opened the persons page', async ({ page }) => {
  await page.goto('/persons')
  await expect(page.locator(REFUSAL)).toBeVisible()
})

When('the user opens the persons page', async ({ page }) => {
  await page.goto('/persons')
})

When('the user reloads the admin', async ({ page }) => {
  await page.reload()
})

When('the user opens the users page', async ({ page }) => {
  await page.goto('/users')
})

// A client-side move, not a fresh load: the explanation is held in memory on
// purpose, so a scenario about it surviving must not reload. The refusal screen
// replaces the admin chrome, so there is no nav link to click in this state —
// the History API is how the router is driven without leaving the page.
When('the user moves to the users page without leaving the admin', async ({ page }) => {
  await page.evaluate(() => {
    window.history.pushState({}, '', '/users')
    window.dispatchEvent(new PopStateEvent('popstate', { state: {} }))
  })
  await page.waitForTimeout(200)
})

When('the user takes the offer to try again', async ({ page }) => {
  await page.locator(RETRY).click()
})

Then('the user is sent to the sign-in page', async ({ page }) => {
  await expect(page).toHaveURL(/\/login/)
})

Then('the user is not sent to the sign-in page', async ({ page }) => {
  await expect(page).not.toHaveURL(/\/login/)
})

Then('the page explains they have no account to work in', async ({ page }) => {
  await expect(page.locator(REFUSAL_TEXT)).toContainText('do not have an account to work in')
})

Then('the page explains their access to the account was taken away', async ({ page }) => {
  await expect(page.locator(REFUSAL_TEXT)).toContainText('access to this account was removed')
})

Then(
  'the page explains the account is suspended and an operator has to turn it back on',
  async ({ page }) => {
    await expect(page.locator(REFUSAL_TEXT)).toContainText('suspended')
    await expect(page.locator(REFUSAL_TEXT)).toContainText('operator')
  },
)

Then('the page still explains the account is suspended', async ({ page }) => {
  await expect(page.locator(REFUSAL_TEXT)).toContainText('suspended')
})

// The suspension must not be replaced by the vaguer message. Asserting the
// absence of the unscoped wording is what stops an implementation from simply
// showing whichever refusal arrived last.
Then('the page does not explain only that they have no account to work in', async ({ page }) => {
  await expect(page.locator(REFUSAL_TEXT)).not.toContainText('do not have an account to work in')
})

// Akeem's ruling, asserted positively: the explanation is deliberately not
// persisted, because a stored suspension notice would outlive the suspension.
Then('once the user reloads the page, the suspension is no longer explained', async ({ page }) => {
  await page.reload()
  await expect(page.locator(REFUSAL_TEXT)).not.toContainText('suspended')
})

Then('the page offers to try again', async ({ page }) => {
  await expect(page.locator(RETRY)).toBeVisible()
})

Then('the page offers to sign in again', async ({ page }) => {
  await expect(page.locator(SIGN_IN)).toBeVisible()
})

Then('the page does not offer to sign in again', async ({ page }) => {
  await expect(page.locator(SIGN_IN)).toHaveCount(0)
})

Then('the persons page is shown', async ({ page }) => {
  await expect(page).toHaveURL(/\/persons/)
  await expect(page.locator(REFUSAL)).toHaveCount(0)
})

Then('no refusal is explained on the page', async ({ page }) => {
  await expect(page.locator(REFUSAL)).toHaveCount(0)
})
