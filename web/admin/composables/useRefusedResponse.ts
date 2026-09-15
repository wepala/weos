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

import { message } from 'ant-design-vue'
import { clearImpersonation, impersonationRefusalOutcome } from './impersonationRefusal'
import { applyRefusedResponse } from './useSessionRefusal'

// One identity read at a time. A page issues several calls, and every one the
// server refuses would otherwise start its own read.
let identityRead: Promise<void> | null = null

/**
 * Ends the impersonation the admin shows when the server refused a request
 * with the impersonation code (wm-669nf). The server has already expired the
 * cookie, so the banner goes at once and the identity is read again for the
 * person really signed in. The impersonation start route answers the same
 * code, and its page explains that refusal itself.
 */
function applyImpersonationRefusal(request: unknown, status: number | undefined, body: unknown) {
  const { user, fetchUser } = useAuth()
  const { ended, notice } = impersonationRefusalOutcome(status, body, request, !!user.value?.impersonating)
  if (!ended) return
  user.value = clearImpersonation(user.value)
  if (notice) message.warning(notice)
  if (!identityRead) {
    identityRead = fetchUser().finally(() => {
      identityRead = null
    })
  }
}

/**
 * Applies a failed response, wherever it came from: the impersonation refusal
 * first, then the session refusal. The $fetch interceptor
 * (plugins/session-refusal.client.ts) and the agent chat's native-fetch stream
 * (useAgentApi, through applyStreamRefusal) both call this one function, so the
 * two paths cannot answer the same refusal differently (wm-ptcuk).
 */
export function applyRefusal(request: unknown, status: number | undefined, body: unknown): void {
  applyImpersonationRefusal(request, status, body)
  if (status !== undefined) {
    applyRefusedResponse(status, body)
  }
}
