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

import { createResolver } from '@nuxt/kit'

// `~/` in a Nuxt layer's config resolves against the *consumer's* rootDir,
// not the layer's own rootDir. So `~/components` and `~/assets/...` quietly
// point at the consumer's project when this layer is fetched via giget or
// listed under `extends`, which means none of the layer's own components
// get auto-imported and the layer's CSS fails to load. Anchor every
// layer-local path to this file's directory instead so the layer works
// both standalone and as a dependency. Closes wepala/weos#337.
const { resolve } = createResolver(import.meta.url)

export default defineNuxtConfig({
  ssr: false,
  // Preset screens (.mjs) use string templates — requires the runtime compiler.
  vue: { runtimeCompiler: true },
  modules: ['@ant-design-vue/nuxt'],
  components: [
    { path: resolve('./components'), pathPrefix: false },
  ],
  css: [resolve('./assets/css/main.css')],
  devtools: { enabled: false },
  devServer: {
    port: 3000,
  },
  nitro: {
    devProxy: {
      '/api': {
        target: 'http://localhost:8080/api',
        changeOrigin: true,
      },
    },
  },
  compatibilityDate: '2025-01-01',
})
