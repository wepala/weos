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
 * Sends every relative admin API call ('/api' and '/api/...') to the origin's
 * /api, not under app.baseURL, so the admin reaches the API whatever path it
 * is served under (wepala/weos#352). Every other relative call keeps
 * app.baseURL.
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
 * The pin covers relative /api calls ONLY. A host page that fetches a file
 * from its own bundle ($fetch('/config.json') under '/admin/') still gets
 * '/admin/config.json', as Nuxt resolves it. A URL with an origin, a Request
 * object, and a call that names its own baseURL are left alone. So is a
 * fetcher derived with its own base ($fetch.create({ baseURL: '/tools/' })):
 * its '/api/x' goes to '/tools/api/x', as the caller configured it.
 *
 * It wraps $fetch rather than adding an onRequest hook: ofetch replaces a
 * default hook with any onRequest given later — per call, or by a
 * $fetch.create({ onRequest }) in a host plugin — and the pin would be lost
 * without a sign. The wrapper's create wraps what it creates unless the
 * create names a baseURL, so a derived fetcher that only adds hooks or
 * headers keeps the pin whatever options it adds.
 *
 * Pinning here, once, is what lets every caller keep writing '/api/...':
 * useApi, the composables and pages that call $fetch directly, and a host
 * layer's own calls. Calls that bypass $fetch already resolve against the
 * origin — native fetch in useAgentApi, and the full-page navigations to
 * /api/auth/login.
 *
 * enforce 'pre' runs this before every default plugin, the session-refusal
 * plugin and a host layer's plugins included. They replace $fetch with
 * $fetch.create(...), which here returns a pinned fetcher that keeps the
 * defaults of the $fetch it is called on, so the pin and their interceptors
 * both hold. That holds only for a create that runs after this setup: a
 * $fetch.create at module scope runs at import, before any plugin, and its
 * /api calls follow app.baseURL. Create derived fetchers inside a plugin or a
 * composable. The plugin is kept free of imports so Node's test runner can
 * load it directly (tests/unit).
 */
type Fetch = typeof $fetch
type FetchOptions = Parameters<Fetch>[1]

/** True for a relative path to the API: '/api', '/api/...', '/api?...'. */
function isApiPath(request: unknown): boolean {
  return (
    typeof request === 'string' &&
    (request === '/api' || request.startsWith('/api/') || request.startsWith('/api?'))
  )
}

/** The options for one call: the origin's root as base, for an /api path the caller gave no base for. */
function pinOptions(request: unknown, options: FetchOptions): FetchOptions {
  if (!isApiPath(request) || options?.baseURL !== undefined) {
    return options
  }
  return { ...options, baseURL: '/' }
}

/** A $fetch that sends relative /api calls to the origin's root, and whose create does the same. */
function pinApiOrigin(base: Fetch): Fetch {
  const call = (request: Parameters<Fetch>[0], options?: FetchOptions) =>
    base(request, pinOptions(request, options))
  // Copies raw, native and create off the base; raw and create are replaced below.
  const pinned = Object.assign(call, base) as Fetch
  pinned.raw = ((request: Parameters<Fetch['raw']>[0], options?: FetchOptions) =>
    base.raw(request, pinOptions(request, options))) as Fetch['raw']
  // A derived fetcher with its own baseURL keeps it: no pin is put on it.
  // Rest arguments pass ofetch's second argument (global options) through too.
  pinned.create = ((...args: Parameters<Fetch['create']>) => {
    const derived = base.create(...args)
    return args[0]?.baseURL !== undefined ? derived : pinApiOrigin(derived)
  }) as Fetch['create']
  return pinned
}

export default defineNuxtPlugin({
  name: 'weos:api-origin',
  enforce: 'pre',
  setup() {
    globalThis.$fetch = pinApiOrigin($fetch)
  },
})
