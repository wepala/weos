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

// Steps for what the admin DRAWS from a feature set (#486/#488).
//
// Every answer here is scripted with page.route, the same way
// session-refusals.steps.ts scripts a 401. This suite cannot prove the answer
// is true — the server's half is pinned by the godog suite in
// tests/e2e/features/feature_flag_admin_surface.feature, and every hiding
// scenario below has a refusal scenario over there. Neither file is a control
// on its own.

import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { createBdd } from 'playwright-bdd'

const { Given, When, Then, Before } = createBdd()

const SIGNED_IN_USER = {
  id: 'agent-1',
  name: 'Ada',
  email: 'ada@example.com',
  role: 'owner',
}

// featureRequests counts what actually went to /api/features, per page. The
// cost claim is that navigating does not ask again, so it has to be counted
// rather than assumed.
const featureRequests = new WeakMap<Page, string[]>()

function recordRequest(page: Page, url: string) {
  const seen = featureRequests.get(page) ?? []
  seen.push(url)
  featureRequests.set(page, seen)
}

function requestsFor(page: Page): string[] {
  return featureRequests.get(page) ?? []
}

/** The listing shape the server really sends — the envelope plus the full
 *  status of every declared feature, which is what #487 pinned. */
function featureSet(agentChat: boolean) {
  return {
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      data: [
        {
          key: 'agent-chat',
          displayName: 'Assistant',
          description: "Talk to this instance's assistant",
          enabled: agentChat,
          source: agentChat ? 'default' : 'instance',
          default: true,
          manageable: true,
          grantable: true,
        },
      ],
    }),
  }
}

const EMPTY_LIST = {
  status: 200,
  contentType: 'application/json',
  body: JSON.stringify({ data: [] }),
}

/**
 * scriptApi answers the whole API: the identity, the feature set, and an empty
 * list for everything else. Routed once per scenario and re-routable, so a
 * scenario can change the answer while the person stays in the admin.
 */
async function scriptApi(
  page: Page,
  opts: { agentChat: boolean; anonymousAgentChat?: boolean; signedIn?: boolean; agentRefusal?: boolean },
) {
  await page.unroute('**/api/**')
  await page.route('**/api/**', async (route) => {
    const url = route.request().url()

    if (url.includes('/api/auth/me')) {
      if (opts.signedIn === false) {
        await route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not authenticated' }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: SIGNED_IN_USER }),
      })
      return
    }

    if (url.includes('/api/features')) {
      recordRequest(page, new URL(url).pathname)
      const on = opts.signedIn === false ? (opts.anonymousAgentChat ?? false) : opts.agentChat
      await route.fulfill(featureSet(on))
      return
    }

    if (opts.agentRefusal && url.includes('/api/agent/')) {
      await route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({
          error: '/api/agent/conversations/x/messages is not available: ' +
            'the "agent-chat" capability is not enabled for you.',
        }),
      })
      return
    }

    await route.fulfill(EMPTY_LIST)
  })
}

// Kept per page so a scenario can re-script without losing what it asked for.
const scriptState = new WeakMap<Page, { agentChat: boolean; anonymousAgentChat?: boolean; signedIn?: boolean; agentRefusal?: boolean }>()

async function rescript(page: Page, patch: Partial<{ agentChat: boolean; anonymousAgentChat: boolean; signedIn: boolean; agentRefusal: boolean }>) {
  const next = { ...(scriptState.get(page) ?? { agentChat: true }), ...patch }
  scriptState.set(page, next)
  await scriptApi(page, next)
}

// --- staging --------------------------------------------------------------

// "the API is scripted to serve the signed-in user" belongs to
// session-refusals.steps.ts and this file's Background reuses it. It routes the
// identity call; the feature-set steps below re-route the whole API on top,
// which is why they call unroute first.

// Registered once. playwright-bdd matches on the text, not the keyword, so
// this serves both the Given that stages a set and the When that changes it
// while the person stays in the admin.
Given('the feature set is scripted with {string} {word}', async ({ page }, key: string, state: string) => {
  expect(key, 'only agent-chat is gated in the admin today').toBe('agent-chat')
  await rescript(page, { agentChat: state === 'on' })
})

Given(
  'the feature set is scripted with {string} {word} for a caller with no session',
  async ({ page }, key: string, state: string) => {
    expect(key).toBe('agent-chat')
    await rescript(page, { anonymousAgentChat: state === 'on' })
  },
)

Given(
  'the feature set is scripted with {string} {word} for the signed-in user',
  async ({ page }, key: string, state: string) => {
    expect(key).toBe('agent-chat')
    await rescript(page, { agentChat: state === 'on' })
  },
)

Given('the user is signed out', async ({ page }) => {
  await rescript(page, { signedIn: false })
})

Given('the signed-in user is an owner', async () => {
  // SIGNED_IN_USER is an owner already; stated so the scenario reads true.
})

Given('the user is on a narrow screen', async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 })
})

Given(
  'the agent API is scripted to refuse the call because the capability is not enabled',
  async ({ page }) => {
    await rescript(page, { agentRefusal: true })
  },
)

Given('the user is impersonating another user', async ({ page }) => {
  await rescript(page, {})
})

// --- navigating -----------------------------------------------------------

const PAGES: Record<string, string> = {
  dashboard: '/',
  persons: '/persons',
  users: '/users',
  settings: '/settings',
  agent: '/agent',
}

async function open(page: Page, name: string) {
  const path = PAGES[name]
  expect(path, `no page named ${name}`).toBeTruthy()
  await page.goto(path)
  await page.waitForLoadState('networkidle')
}

// Navigation steps are shared across this suite. agent-chat.steps.ts owns
// "the user opens the agent page", and session-refusals.steps.ts owns the
// persons and users pages, so this file defines only what neither has —
// otherwise playwright-bdd refuses the whole run for an ambiguous match.
When('the user opens the settings page', async ({ page }) => {
  await open(page, 'settings')
})

/**
 * A client-side move: what a person does. page.goto is a fresh app boot, which
 * legitimately re-reads the set, so a scenario about navigation costing nothing
 * has to click.
 */
When('the user clicks through to the {word} page', async ({ page }, name: string) => {
  const label = NAV_LABELS[name]
  expect(label, `no sidebar entry named ${name}`).toBeTruthy()
  await page.locator('.ant-menu').getByText(label, { exact: true }).first().click()
  await page.waitForURL(`**${PAGES[name]}`)
  await page.waitForLoadState('networkidle')
})

// The admin's own sidebar entries. Resource-type entries are not listed: they
// exist only when the instance has that type, and this scenario needs links
// that are always drawn.
const NAV_LABELS: Record<string, string> = {
  dashboard: 'Dashboard',
  users: 'Users',
  settings: 'Settings',
  agent: 'Agent',
}

When('the user opens the dashboard', async ({ page }) => {
  await open(page, 'dashboard')
})

When('the user opens the admin', async ({ page }) => {
  await open(page, 'dashboard')
})

Given('the user has opened the dashboard and been offered {string}', async ({ page }, label: string) => {
  await open(page, 'dashboard')
  await expect(page.locator('.ant-menu').getByText(label, { exact: true }).first()).toBeVisible()
})

Given('the user has opened the dashboard and not been offered {string}', async ({ page }, label: string) => {
  await open(page, 'dashboard')
  await expect(page.locator('.ant-menu').getByText(label, { exact: true })).toHaveCount(0)
})

When('the user opens the agent page directly by its address', async ({ page }) => {
  await open(page, 'agent')
})

When('the user opens the mobile menu', async ({ page }) => {
  await page.locator('[data-testid="mobile-menu-toggle"], .mobile-menu-trigger, .anticon-menu').first().click()
  await page.waitForTimeout(300)
})

// --- acting ---------------------------------------------------------------

When('the user signs in', async ({ page }) => {
  // Signing in lands on the admin, not on whatever page a signed-out visitor
  // was bounced to — reloading /login would only draw /login again.
  await rescript(page, { signedIn: true })
  await open(page, 'dashboard')
})

When('the user starts impersonating another user', async ({ page }) => {
  await page.reload()
  await page.waitForLoadState('networkidle')
})

When('the user stops impersonating', async ({ page }) => {
  await page.reload()
  await page.waitForLoadState('networkidle')
})

// "the user sends {string}" belongs to agent-chat.steps.ts; this file reuses it.

// --- outcomes -------------------------------------------------------------

Then('the admin asked for the feature set once', async ({ page }) => {
  expect(requestsFor(page).length, `feature-set requests: ${requestsFor(page).join(', ')}`).toBe(1)
})

Then('the request for the feature set went to {string}', async ({ page }, path: string) => {
  // The root case of #352: exactly one request, and it lands where the API is,
  // not somewhere with a prefix in front of it. A non-root mount cannot be
  // booted here — app.baseURL is fixed at build time — so that half is #352's.
  expect(requestsFor(page)).toContain(path)
})

Then('the admin asked for the feature set again after signing in', async ({ page }) => {
  expect(requestsFor(page).length).toBeGreaterThan(1)
})

Then('the admin asked for the feature set again', async ({ page }) => {
  expect(requestsFor(page).length).toBeGreaterThan(1)
})

Then('moving between pages does not read the set again', async ({ page }) => {
  expect(requestsFor(page).length).toBe(1)
})

Then('the sidebar offers {string}', async ({ page }, label: string) => {
  await expect(page.locator('.ant-menu').getByText(label, { exact: true }).first()).toBeVisible()
})

Then('the sidebar still offers {string}', async ({ page }, label: string) => {
  await expect(page.locator('.ant-menu').getByText(label, { exact: true }).first()).toBeVisible()
})

Then('the sidebar does not offer {string}', async ({ page }, label: string) => {
  await expect(page.locator('.ant-menu').getByText(label, { exact: true })).toHaveCount(0)
})

Then('the mobile menu does not offer {string}', async ({ page }, label: string) => {
  await expect(page.locator('.ant-menu').getByText(label, { exact: true })).toHaveCount(0)
})

Then('the mobile menu still offers {string}', async ({ page }, label: string) => {
  await expect(page.locator('.ant-menu').getByText(label, { exact: true }).first()).toBeVisible()
})

Then('the conversation is not shown', async ({ page }) => {
  await expect(page.locator('.chat-scroll')).toHaveCount(0)
})

Then('the page explains the assistant is not available to them', async ({ page }) => {
  await expect(page.locator('[data-testid="agent-not-enabled"]')).toBeVisible()
})

Then('the page does not explain the in-app agent is not configured', async ({ page }) => {
  await expect(page.locator('[data-testid="agent-not-configured"]')).toHaveCount(0)
})

// "the user is not sent to the sign-in page" belongs to
// session-refusals.steps.ts; this file reuses it, which is right — a feature
// refusal and a session refusal must both leave the person where they are.

Then('the conversation shows no reply', async ({ page }) => {
  await expect(page.locator('.chat-message.agent')).toHaveCount(0)
})

// consoleErrors is collected for every scenario, not on request. A page that
// draws a hidden section by throwing is a page that passes every visual
// assertion, and a watcher a scenario has to ask for is a watcher that is
// never attached — the assertion would then pass because the list is empty
// rather than because nothing went wrong.
const consoleErrors = new WeakMap<Page, string[]>()

Before(async ({ page }) => {
  consoleErrors.set(page, [])
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    // The browser narrates every non-2xx response as a console error. An
    // expected 401 on the identity probe while signed out is the app working,
    // not the app breaking, so what is collected here is what the page's own
    // code logged plus anything uncaught.
    if (msg.text().startsWith('Failed to load resource')) return
    consoleErrors.get(page)?.push(msg.text())
  })
  page.on('pageerror', (err) => {
    consoleErrors.get(page)?.push(String(err))
  })
})

Then('the page reports no errors in the console', async ({ page }) => {
  const errors = consoleErrors.get(page)
  expect(errors, 'the console was never watched, so this would pass for the wrong reason').toBeDefined()
  expect(errors, `console errors: ${(errors ?? []).join(' | ')}`).toHaveLength(0)
})

Given('the user has opened the dashboard', async ({ page }) => {
  await open(page, 'dashboard')
})

Then('the admin did not ask for the feature set again', async ({ page }) => {
  expect(requestsFor(page).length, `feature-set requests: ${requestsFor(page).join(', ')}`).toBe(1)
})

Then('the user is still signed in', async ({ page }) => {
  expect(new URL(page.url()).pathname).not.toContain('/login')
})

Then('the sign-in page is shown', async ({ page }) => {
  // The login page is a card, not a form element.
  await expect(page.getByText('Sign in to manage your site')).toBeVisible()
})

Then('the persons page still loads', async ({ page }) => {
  await open(page, 'persons')
  expect(new URL(page.url()).pathname).toContain('/persons')
})

/**
 * An all-off answer that says why. This is the outage shape the API half
 * pins: 200, every feature off, plus the message — so the admin agrees with
 * the gates instead of guessing, and the person has something to read.
 */
Given(
  'the feature set is scripted with every feature off and a message that the state could not be read',
  async ({ page }) => {
    await page.unroute('**/api/**')
    await page.route('**/api/**', async (route) => {
      const url = route.request().url()
      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: SIGNED_IN_USER }),
        })
        return
      }
      if (url.includes('/api/features')) {
        recordRequest(page, new URL(url).pathname)
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [{ key: 'agent-chat', displayName: 'Assistant', enabled: false, source: 'error' }],
            messages: [
              { type: 'warning', text: 'Feature state could not be read, so every feature is reported off.' },
            ],
          }),
        })
        return
      }
      await route.fulfill(EMPTY_LIST)
    })
  },
)

Then('the page explains the feature state could not be read', async ({ page }) => {
  await expect(page.locator('.api-notifications').getByText(/could not be read/i).first()).toBeVisible()
})

/**
 * The set cannot be fetched at all. The gated sections go — the server refuses
 * them anyway, so showing them would only offer links that lead to a refusal —
 * and this is NOT a sign-in problem, so nobody is sent to sign in.
 */
Given('asking for the feature set is scripted to fail', async ({ page }) => {
  await page.unroute('**/api/**')
  await page.route('**/api/**', async (route) => {
    const url = route.request().url()
    if (url.includes('/api/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: SIGNED_IN_USER }),
      })
      return
    }
    if (url.includes('/api/features')) {
      recordRequest(page, new URL(url).pathname)
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'boom' }),
      })
      return
    }
    await route.fulfill(EMPTY_LIST)
  })
})
