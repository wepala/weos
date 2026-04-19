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

// Registers all Ant Design Vue components on the app-level Vue instance so
// they're resolvable from *runtime-compiled* templates — specifically the
// string templates inside preset screen .mjs modules loaded via Blob URL.
//
// The `@ant-design-vue/nuxt` module handles component auto-imports for
// Nuxt-built SFCs at build time. That auto-import is invisible to modules
// imported via `import(blobUrl)` at runtime, so preset screens see
// <a-button>, <a-form>, <a-table> etc. as unresolved custom elements and
// render them as raw HTML.
//
// Calling `app.use(Antd)` installs ant-design-vue as a Vue plugin, which
// runs its install() function and registers every component globally.
// Global registration is the first thing Vue's resolveComponent() checks
// when it can't find a component locally, so preset screens pick them up.
//
// Bundle impact: the auto-import module tree-shakes unused components;
// this plugin registers them all. For the admin app today, most ant
// components are already bundled because the SFC pages use them; shipping
// the rest to preset screens costs a small fraction of the already-large
// admin bundle and keeps preset-screen authoring ergonomic.
//
// Client-only (.client.ts) because SSR is disabled for this app
// (nuxt.config.ts: `ssr: false`) and preset screens only ever load in
// the browser via a Blob URL.

import Antd from 'ant-design-vue'

export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.vueApp.use(Antd)
})
