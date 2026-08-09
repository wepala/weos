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

import { applyRefusedResponse } from '../composables/useSessionRefusal'

/**
 * Decides what a 401 means, for every call the admin makes.
 *
 * This lives in a $fetch interceptor rather than in useApi because not every
 * caller goes through useApi: usePersonApi, for one, calls $fetch directly.
 * Handling refusals in the wrapper would mean a 401 from those endpoints was
 * silently ignored.
 *
 * It still does not cover literally every call. The agent chat streams with
 * native fetch, which no $fetch interceptor can see, so it calls
 * applyRefusedResponse itself. Any future caller that reaches past $fetch has
 * to do the same.
 *
 * A coded refusal is explained where the person is standing and explicitly
 * does NOT redirect: for two of the three codes a fresh sign-in cannot help,
 * and redirecting produces a loop that lasts as long as they keep trying. An
 * uncoded 401 is the ordinary expired-or-missing session, where signing in
 * again IS the remedy — including mid-session, where the request used to fail
 * invisibly and leave a half-empty page.
 */
export default defineNuxtPlugin(() => {
  globalThis.$fetch = $fetch.create({
    // Deliberately no onResponse hook clearing the refusal.
    //
    // A page issues several calls, and one of them succeeding says nothing
    // about the one that was refused: clearing on any success races with the
    // refusal that is still arriving, and the explanation disappears from a
    // page that genuinely cannot load. Recovery is handled where it actually
    // happens instead — "try again" reloads, and a reload starts with no
    // refusal because the state is held in memory by design.
    onResponseError({ response }) {
      applyRefusedResponse(response?.status, response?._data)
    },
  })
})
