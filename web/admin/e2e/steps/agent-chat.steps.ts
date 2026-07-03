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

// Steps for the agent chat interface. The agent API is mocked with
// page.route so the UI is exercised deterministically without an LLM key;
// the API's own behavior is covered by the godog suite in tests/e2e.

import { expect } from '@playwright/test'
import { createBdd, DataTable } from 'playwright-bdd'

const { Given, When, Then } = createBdd()

// sse encodes agent events the way the server does (entities.AgentEvent).
function sse(events: Array<Record<string, unknown>>): string {
  return events
    .map((e) => `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`)
    .join('')
}

function sseResponse(body: string) {
  return {
    status: 200,
    headers: { 'content-type': 'text/event-stream' },
    body,
  }
}

function widgetsEvent(widgets: Array<Record<string, unknown>>) {
  return { type: 'widgets', widgets: { schemaVersion: 1, widgets } }
}

// mockAgentAPI stubs every /api/agent route: history is empty unless a
// scenario overrides it, message turns answer with the scripted stream, and
// confirmations answer with their own scripted stream.
async function mockAgentAPI(
  page: import('@playwright/test').Page,
  streams: { message?: string; confirmation?: string },
) {
  await page.route('**/api/agent/conversations/**', async (route) => {
    const url = route.request().url()
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      })
      return
    }
    if (url.includes('/confirmations/')) {
      await route.fulfill(sseResponse(streams.confirmation ?? sse([{ type: 'done' }])))
      return
    }
    await route.fulfill(sseResponse(streams.message ?? sse([{ type: 'done' }])))
  })
}

Given('the instance has no agent model configured', async () => {
  // Nothing to mock: the seeded test server has no Gemini key, so the real
  // agent API answers 503 not-configured.
})

Given(
  'the agent API is scripted to reply with the text {string}',
  async ({ page }, text: string) => {
    await mockAgentAPI(page, {
      message: sse([
        { type: 'text', text },
        widgetsEvent([{ type: 'markdown', markdown: text }]),
        { type: 'done' },
      ]),
    })
  },
)

Given(
  'the agent API is scripted to reply with a table titled {string} listing:',
  async ({ page }, title: string, table: DataTable) => {
    const [header, ...rows] = table.raw()
    await mockAgentAPI(page, {
      message: sse([
        widgetsEvent([{ type: 'table', title, columns: header, rows }]),
        { type: 'done' },
      ]),
    })
  },
)

Given(
  'the agent API is scripted to request confirmation for the tool {string}',
  async ({ page }, tool: string) => {
    await mockAgentAPI(page, {
      message: sse([
        {
          type: 'input_requested',
          callId: 'call-1',
          tool,
          hint: `The agent wants to run ${tool}`,
          args: { type_slug: 'note' },
        },
        widgetsEvent([{ type: 'markdown', markdown: 'Waiting for your approval.' }]),
        { type: 'done' },
      ]),
    })
  },
)

Given(
  'answering the confirmation is scripted to reply with the text {string}',
  async ({ page }, text: string) => {
    // Re-register: the newest route handler wins for the confirmations path.
    await page.route('**/api/agent/conversations/**/confirmations/**', async (route) => {
      await route.fulfill(
        sseResponse(
          sse([
            { type: 'text', text },
            widgetsEvent([{ type: 'markdown', markdown: text }]),
            { type: 'done' },
          ]),
        ),
      )
    })
  },
)

Given(
  'the agent history is scripted to return that exchange',
  async ({ page }) => {
    await page.route('**/api/agent/conversations/*', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.fallback()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [
            { role: 'user', text: 'remember the office moves Friday' },
            {
              role: 'agent',
              widgets: {
                schemaVersion: 1,
                widgets: [{ type: 'markdown', markdown: 'Noted.' }],
              },
            },
          ],
        }),
      })
    })
  },
)

When('the user opens the agent page', async ({ page }) => {
  await page.goto('/agent')
  await expect(page.getByRole('heading', { name: 'Agent' })).toBeVisible()
})

When('the user sends {string}', async ({ page }, message: string) => {
  await page.getByPlaceholder('Ask your app anything…').fill(message)
  await page.getByRole('button', { name: 'Send' }).click()
})

When('the user approves the pending action', async ({ page }) => {
  await page.getByRole('button', { name: 'Approve' }).click()
})

When('the user reloads the page', async ({ page }) => {
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Agent' })).toBeVisible()
})

Then('the page explains the in-app agent is not configured', async ({ page }) => {
  await expect(page.getByText('The in-app agent is not configured')).toBeVisible()
})

Then(
  'the conversation shows the user message {string}',
  async ({ page }, text: string) => {
    await expect(page.locator('.chat-message.user', { hasText: text })).toBeVisible()
  },
)

Then(
  'the conversation shows an agent reply containing {string}',
  async ({ page }, text: string) => {
    await expect(page.locator('.chat-message.agent', { hasText: text })).toBeVisible()
  },
)

Then('the conversation shows a table titled {string}', async ({ page }, title: string) => {
  await expect(page.locator('.chat-message.agent .widget-table h4', { hasText: title })).toBeVisible()
})

Then('the table lists {string}', async ({ page }, cell: string) => {
  await expect(page.locator('.chat-message.agent .ant-table-cell', { hasText: cell })).toBeVisible()
})

Then(
  'an approval card for the tool {string} appears',
  async ({ page }, tool: string) => {
    await expect(page.getByText(`The agent wants to run: ${tool}`)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Approve' })).toBeVisible()
  },
)
