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
 * Hands a failed native-fetch response to the refusal handling the $fetch
 * interceptor uses (wm-ptcuk).
 *
 * The agent chat streams with native fetch, so the interceptor never sees its
 * responses. Before this, the stream applied only a 401, and a 403 with
 * impersonation_target_not_member left the impersonation banner up. Every
 * failure now goes to the same function the interceptor calls, which decides
 * what each status and code means.
 *
 * Kept free of Nuxt and Vue imports so Node's test runner can load it
 * directly (tests/unit).
 */

/** Applies a failed response: the request, its status, and its decoded body. */
export type RefusalApplier = (request: unknown, status: number | undefined, body: unknown) => void

/**
 * Applies res through apply when the server did not accept the request. The
 * body is read from a clone, so the caller can still read it afterwards. A
 * body that is not JSON is applied as null.
 */
export async function applyStreamRefusal(res: Response, request: unknown, apply: RefusalApplier): Promise<void> {
  if (res.ok) {
    return
  }
  let body: unknown = null
  try {
    body = await res.clone().json()
  } catch {
    body = null
  }
  apply(request, res.status, body)
}
