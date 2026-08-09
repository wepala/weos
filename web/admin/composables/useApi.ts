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
 * Checks whether a raw response is a single-entity envelope.
 * Distinguishes from paginated responses by excluding objects
 * that also have `cursor` or `has_more` keys.
 */
function isSingleEnvelope(obj: unknown): obj is { data: unknown; messages?: ApiMessage[] } {
  return (
    typeof obj === 'object' &&
    obj !== null &&
    'data' in obj &&
    !('cursor' in obj) &&
    !('has_more' in obj)
  )
}

/**
 * Process messages from any API response if present.
 * Exported so paginated endpoints can forward messages without unwrapping.
 */
export function forwardMessages(obj: unknown): void {
  if (
    typeof obj === 'object' &&
    obj !== null &&
    'messages' in obj &&
    Array.isArray((obj as any).messages)
  ) {
    const { processApiMessages } = useNotifications()
    processApiMessages((obj as any).messages)
  }
}

/**
 * Unwraps a single-entity API response envelope.
 * If the response has the `{ data, messages? }` shape (excluding paginated),
 * the payload inside `data` is returned and any `messages` are forwarded to
 * the notification system. Non-envelope responses are returned as-is.
 */
export function unwrapEnvelope<T>(raw: unknown): T {
  forwardMessages(raw)
  if (isSingleEnvelope(raw)) {
    return raw.data as T
  }
  return raw as T
}

export function useApi() {
  const { noteRefusal, clearRefusal } = useSessionRefusal()

  /**
   * Decides what a 401 means and acts on it.
   *
   * A coded refusal is explained where the person is standing, and explicitly
   * does NOT redirect: for two of the three codes a fresh sign-in cannot help,
   * and redirecting produces a loop that lasts as long as they keep trying.
   * An uncoded 401 is the ordinary expired-or-missing session, where signing
   * in again IS the remedy, so that one redirects — including mid-session,
   * where the request used to fail invisibly and leave a half-empty page.
   */
  function handleUnauthorized(body: unknown): void {
    const code = (body as { code?: unknown } | null)?.code
    if (isRefusalCode(code)) {
      noteRefusal(code)
      return
    }
    clearRefusal()
    const route = useRoute()
    if (route.path !== '/login') {
      navigateTo({ path: '/login', query: { redirect: route.fullPath } })
    }
  }

  async function request<T>(
    url: string,
    options?: RequestInit,
  ): Promise<T> {
    try {
      const res = await $fetch<unknown>(url, {
        ...options,
        headers: {
          'Content-Type': 'application/json',
          ...(options?.headers as Record<string, string>),
        },
      } as any)
      // A request that succeeds proves the refusal has passed — the revoked
      // case can be transient, so a stale explanation left on screen would
      // contradict a page that is now working.
      clearRefusal()
      return unwrapEnvelope<T>(res)
    } catch (err: any) {
      // $fetch throws FetchError on 4xx/5xx with .data containing the response body.
      // Forward structured messages from error envelopes to the notification system.
      if (err?.data) {
        forwardMessages(err.data)
      }
      if (err?.status === 401 || err?.statusCode === 401) {
        handleUnauthorized(err?.data)
      }
      throw err
    }
  }

  return { request }
}
