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

/**
 * Resolves every relative admin $fetch call against the origin's root, not
 * app.baseURL, so '/api/...' reaches the API whatever path the admin is
 * served under (wepala/weos#352).
 *
 * Nuxt creates the client $fetch with baseURL = app.baseURL. Served at '/',
 * that changes nothing. But a host app that extends this admin as a Nuxt
 * layer and serves it under a path — app.baseURL '/admin/' — gets every
 * $fetch('/api/...') sent to '/admin/api/...', and a host answers that with
 * the SPA's index.html and a 200. The admin then reads HTML as its data and
 * loads nothing. The API is mounted at the origin's root, not under the
 * admin's path, so its calls must not follow app.baseURL.
 *
 * This assumes the API is at the origin's root. A deploy behind a proxy that
 * serves the whole service under a path prefix (API at /tools/api/) is not
 * supported by this pin: its calls go to the origin's /api.
 *
 * The pin covers EVERY relative $fetch call, not only /api ones. A host page
 * that fetches a file from its own bundle must give an absolute path that
 * includes app.baseURL ('/admin/config.json'); '/config.json' now goes to the
 * origin's root.
 *
 * Pinning the base here, once, is what lets every caller keep writing
 * '/api/...': useApi, the composables and pages that call $fetch directly,
 * and a host layer's own calls. Calls that bypass $fetch already resolve
 * against the origin — native fetch in useAgentApi, and the full-page
 * navigations to /api/auth/login.
 *
 * enforce 'pre' runs this before every default plugin, the session-refusal
 * plugin and a host layer's plugins included. They replace $fetch with
 * $fetch.create(...), which keeps the defaults of the $fetch it is called on,
 * so the pin and their interceptors both hold. That holds only for a create
 * that runs after this setup: a $fetch.create at module scope runs at import,
 * before any plugin, and keeps app.baseURL. Create derived fetchers inside a
 * plugin or a composable. The plugin is kept free of imports so Node's test
 * runner can load it directly (tests/unit).
 */
export default defineNuxtPlugin({
  name: 'weos:api-origin',
  enforce: 'pre',
  setup() {
    globalThis.$fetch = $fetch.create({ baseURL: '/' })
  },
})
