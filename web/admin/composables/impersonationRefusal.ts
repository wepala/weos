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
 * How the admin answers the server's impersonation refusal (wm-669nf).
 *
 * The server refuses an impersonation with 403 and the code below in two
 * places: when it is started for a person who is not a member of the caller's
 * account, and on any later request whose impersonation no longer holds — the
 * person left the account, the caller's role changed, or the caller now acts
 * in another account. In the second case the server has already ended the
 * impersonation, so the admin treats the code on any request as "the
 * impersonation ended".
 *
 * Kept free of Nuxt and Vue imports so Node's test runner can load it
 * directly (tests/unit).
 */

export const IMPERSONATION_REFUSED_CODE = 'impersonation_target_not_member'

/** What a person is told when an impersonation is refused. */
export const IMPERSONATION_REFUSED_TEXT = 'You can only impersonate a member of your account.'

/** What a person is told when an impersonation they were in ends this way. */
export const IMPERSONATION_ENDED_TEXT =
  'Your impersonation has ended. You can only impersonate a member of your account.'

const START_PATH = '/api/admin/impersonate'

export function isImpersonationRefusal(status: number | undefined, body: unknown): boolean {
  return status === 403 && (body as { code?: unknown } | null | undefined)?.code === IMPERSONATION_REFUSED_CODE
}

interface FetchFailure {
  status?: number
  statusCode?: number
  response?: { status?: number }
  data?: { error?: unknown }
}

/**
 * The message for a failed impersonation call: the plain explanation for the
 * refusal, otherwise the server's own message, otherwise fallback.
 */
export function impersonationErrorText(err: unknown, fallback: string): string {
  const failure = (err ?? {}) as FetchFailure
  const status = failure.status ?? failure.statusCode ?? failure.response?.status
  if (isImpersonationRefusal(status, failure.data)) {
    return IMPERSONATION_REFUSED_TEXT
  }
  const text = failure.data?.error
  return typeof text === 'string' && text !== '' ? text : fallback
}

function pathOf(request: unknown): string {
  const raw = typeof request === 'string' ? request : (request as { url?: unknown } | null | undefined)?.url
  if (typeof raw !== 'string') {
    return ''
  }
  try {
    return new URL(raw, 'http://admin.invalid').pathname
  } catch {
    return raw
  }
}

export interface ImpersonationRefusalOutcome {
  /** The impersonation is over: clear the banner state and read the identity again. */
  ended: boolean
  /** A notice to show, or null when there is nothing to say or the page says it. */
  notice: string | null
}

/**
 * Decides what a failed response means for the impersonation. A refused start
 * gets no notice here, because the page that asked explains it.
 */
export function impersonationRefusalOutcome(
  status: number | undefined,
  body: unknown,
  request: unknown,
  wasImpersonating: boolean,
): ImpersonationRefusalOutcome {
  if (!isImpersonationRefusal(status, body)) {
    return { ended: false, notice: null }
  }
  const explainedByPage = pathOf(request) === START_PATH
  return { ended: true, notice: wasImpersonating && !explainedByPage ? IMPERSONATION_ENDED_TEXT : null }
}

/** The identity with the impersonation banner state removed. */
export function clearImpersonation<T extends { impersonating?: boolean; real_user?: unknown }>(user: T | null): T | null {
  if (!user) {
    return null
  }
  const copy: T = { ...user, impersonating: false }
  delete copy.real_user
  return copy
}
