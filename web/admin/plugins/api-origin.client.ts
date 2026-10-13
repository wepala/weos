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
 * Sends every admin $fetch call to the origin's /api, whatever app.baseURL
 * the admin is served under (wepala/weos#352).
 *
 * Nuxt creates the client $fetch with baseURL = app.baseURL. Served at '/',
 * that changes nothing. But a host app that extends this admin as a Nuxt
 * layer and serves it under a path — app.baseURL '/admin/' — gets every
 * $fetch('/api/...') sent to '/admin/api/...', and a host answers that with
 * the SPA's index.html and a 200. The admin then reads HTML as its data and
 * loads nothing. The API is mounted at the origin's root, not under the
 * admin's path, so its calls must not follow app.baseURL.
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
 * so the pin and their interceptors both hold. The plugin is kept free of
 * imports so Node's test runner can load it directly (tests/unit).
 */
export default defineNuxtPlugin({
  name: 'weos:api-origin',
  enforce: 'pre',
  setup() {
    globalThis.$fetch = $fetch.create({ baseURL: '/' })
  },
})
