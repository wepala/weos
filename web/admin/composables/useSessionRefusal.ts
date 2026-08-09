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
 * The server refuses a request four different ways behind one 401 status, and
 * three of them carry a `code` in the body. They need opposite handling, so
 * treating them alike is worse than not decoding them at all: sending someone
 * back to sign in when signing in cannot help produces an endless loop, and
 * that loop is what this module exists to prevent.
 *
 * The uncoded refusal is deliberately absent from the table below. It means
 * the session is gone or expired, signing in again is exactly the remedy, and
 * so it is handled by redirecting rather than by explaining.
 */
export type RefusalCode = 'unscoped_session' | 'account_access_revoked' | 'account_deactivated'

export interface SessionRefusal {
  code: RefusalCode
  /** What the person is told. */
  text: string
  /** Offer to run the failed request again, for refusals that can be transient. */
  canRetry: boolean
  /** Offer a fresh sign-in, only where a fresh sign-in can actually resolve it. */
  canSignIn: boolean
}

const REFUSALS: Record<RefusalCode, Omit<SessionRefusal, 'code'>> = {
  // No account means nothing to act in, and a new session would be built the
  // same way. Offering sign-in here is precisely the loop.
  unscoped_session: {
    text: 'You are signed in, but you do not have an account to work in. '
      + 'Signing in again will not change that. Ask an administrator to add you to an account.',
    canRetry: false,
    canSignIn: false,
  },
  // Recomputed per request against live memberships, so a membership rewrite
  // or a lagging replica can hide a membership that is really there. Retry
  // covers that; a fresh sign-in covers a removal that was real, by scoping
  // the next session to an account they still belong to.
  account_access_revoked: {
    text: 'Your access to this account was removed. '
      + 'This is sometimes temporary, so trying again may work. '
      + 'Otherwise sign in again to continue in another account you belong to.',
    canRetry: true,
    canSignIn: true,
  },
  // Membership is intact; the account itself is suspended. Neither retrying
  // nor signing in again touches that, so offering either would be a lie.
  account_deactivated: {
    text: 'This account is suspended, so it cannot be used at the moment. '
      + 'You are still a member of it. Signing in again will not help — '
      + 'an operator has to reactivate the account.',
    canRetry: false,
    canSignIn: false,
  },
}

export function isRefusalCode(value: unknown): value is RefusalCode {
  return typeof value === 'string' && value in REFUSALS
}

export function useSessionRefusal() {
  // useState, not a module-level ref, so the value is per request on the
  // server and shared across components on the client. It is intentionally
  // not persisted: a suspension explained into sessionStorage would outlive
  // the suspension itself and keep telling someone their account is suspended
  // after an operator has reactivated it.
  const refusal = useState<SessionRefusal | null>('session-refusal', () => null)

  function noteRefusal(code: RefusalCode) {
    // A suspension is not downgraded to "you have no account".
    //
    // Once the account is suspended, signing in again resolves nothing, so the
    // next refusal is unscoped_session — which is true but says less: it drops
    // the fact that an operator can fix this by reactivating the account, and
    // it reads as a permanent dead end. The more specific explanation stays
    // until this page session ends, and a reload clears it, so the notice can
    // never outlive the suspension itself.
    if (refusal.value?.code === 'account_deactivated' && code === 'unscoped_session') {
      return
    }
    refusal.value = { code, ...REFUSALS[code] }
  }

  function clearRefusal() {
    refusal.value = null
  }

  return { refusal, noteRefusal, clearRefusal }
}
