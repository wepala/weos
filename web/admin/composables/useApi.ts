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

import type { ApiMessage } from './useNotifications'

/**
 * Checks whether a raw response looks like the API envelope
 * (`{ data: ... }` with an optional `messages` array).
 */
function isEnvelope(obj: unknown): obj is { data: unknown; messages?: ApiMessage[] } {
  return (
    typeof obj === 'object' &&
    obj !== null &&
    'data' in obj
  )
}

/**
 * Unwraps an API response envelope.
 * If the response has the `{ data, messages? }` shape the payload inside
 * `data` is returned and any `messages` are forwarded to the notification
 * system.  Non-envelope responses are returned as-is for backward compat.
 */
export function unwrapEnvelope<T>(raw: unknown): T {
  if (isEnvelope(raw)) {
    const { processApiMessages } = useNotifications()
    processApiMessages(raw.messages)
    return raw.data as T
  }
  return raw as T
}

export function useApi() {
  async function request<T>(
    url: string,
    options?: RequestInit,
  ): Promise<T> {
    const res = await $fetch<unknown>(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...(options?.headers as Record<string, string>),
      },
    } as any)
    return unwrapEnvelope<T>(res)
  }

  return { request }
}
