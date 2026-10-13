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

// wepala/weos#352. Run with `npm run test:unit` (Node's own test runner, which
// reads TypeScript directly on Node 23.6 or later).
//
// A host app that extends this admin as a Nuxt layer may serve it under a
// path, e.g. app.baseURL '/admin/'. Nuxt creates the client $fetch with
// baseURL = app.baseURL, so before the api-origin plugin every
// $fetch('/api/...') in the admin went to '/admin/api/...', which a host
// answers with the SPA's index.html. These tests build the client $fetch the
// way Nuxt does, run the real plugin over it, and record the URL each call
// is sent to.

import { test } from 'node:test'
import assert from 'node:assert/strict'
import { $fetch as ofetch } from 'ofetch'

type Fetch = typeof ofetch

/** The plugin's shape, as defineNuxtPlugin receives it. */
interface ObjectPlugin {
  name?: string
  enforce?: 'pre' | 'default' | 'post'
  setup: () => void
}

const g = globalThis as unknown as {
  $fetch?: Fetch
  defineNuxtPlugin?: (plugin: ObjectPlugin) => ObjectPlugin
}

// defineNuxtPlugin is a Nuxt auto-import. In a build it returns the plugin;
// here it returns the object so the test can read and run it.
g.defineNuxtPlugin = (plugin) => plugin

const { default: apiOrigin } = (await import('../../plugins/api-origin.client.ts')) as {
  default: ObjectPlugin
}

/**
 * Installs the client $fetch exactly as Nuxt's #build/fetch.mjs does
 * (`$fetch.create({ baseURL: baseURL() })`), over a network stub that
 * records each request's URL and answers with the given status.
 */
function installNuxtFetch(appBaseURL: string, status = 200): string[] {
  const seen: string[] = []
  const network = async (request: RequestInfo | URL) => {
    seen.push(typeof request === 'string' ? request : String((request as Request).url ?? request))
    return new Response(JSON.stringify({ data: { ok: true }, code: status === 401 ? 'account_deactivated' : undefined }), {
      status,
      headers: { 'content-type': 'application/json' },
    })
  }
  g.$fetch = ofetch.create({ baseURL: appBaseURL }, { fetch: network as typeof fetch })
  return seen
}

// One path per admin caller that uses $fetch, directly or through useApi.
const ADMIN_API_PATHS = [
  '/api/auth/me',
  '/api/auth/logout',
  '/api/admin/impersonate',
  '/api/admin/stop-impersonation',
  '/api/features',
  '/api/settings/sidebar',
  '/api/settings/roles',
  '/api/settings/role-access',
  '/api/resource-types?limit=100',
  '/api/resource-types/presets',
  '/api/resource-types/presets/tasks/screens/task/list.mjs',
  '/api/persons?limit=20',
  '/api/organizations/o-1/members?limit=20',
  '/api/users',
  '/api/invites',
  '/api/invites/accept',
  '/api/task/t-1',
]

test('the harness reproduces the bug: Nuxt alone sends /api calls under app.baseURL', async () => {
  const seen = installNuxtFetch('/admin/')
  await g.$fetch!('/api/auth/me')
  assert.deepEqual(seen, ['/admin/api/auth/me'])
})

test('the plugin runs before every default plugin, so later $fetch.create calls inherit it', () => {
  assert.equal(apiOrigin.enforce, 'pre')
  assert.equal(apiOrigin.name, 'weos:api-origin')
})

for (const appBaseURL of ['/admin/', '/', '/a/b/']) {
  test(`under app.baseURL '${appBaseURL}' every admin API call goes to the origin's /api`, async () => {
    const seen = installNuxtFetch(appBaseURL)
    apiOrigin.setup()
    for (const path of ADMIN_API_PATHS) {
      await g.$fetch!(path)
    }
    assert.deepEqual(seen, ADMIN_API_PATHS)
  })
}

test('the session-refusal interceptor created after the plugin keeps the origin pin and still fires', async () => {
  const seen = installNuxtFetch('/admin/', 401)
  apiOrigin.setup()
  // What plugins/session-refusal.client.ts does, in plugin order after this one.
  const refused: Array<{ request: unknown; status?: number; code?: unknown }> = []
  g.$fetch = g.$fetch!.create({
    onResponseError({ request, response }) {
      refused.push({ request, status: response?.status, code: (response?._data as { code?: unknown })?.code })
    },
  })
  await assert.rejects(g.$fetch!('/api/auth/me'))
  assert.deepEqual(seen, ['/api/auth/me'])
  assert.deepEqual(refused, [{ request: '/api/auth/me', status: 401, code: 'account_deactivated' }])
})

test('a URL that already names a full origin is left alone', async () => {
  const seen = installNuxtFetch('/admin/')
  apiOrigin.setup()
  await g.$fetch!('https://api.example.test/api/health')
  assert.deepEqual(seen, ['https://api.example.test/api/health'])
})
