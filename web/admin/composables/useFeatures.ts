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
 * The signed-in person's feature set (#486/#488).
 *
 * Read once when the admin boots and shared by every page, then read again on
 * a known signal. The admin has exactly two such signals today — signing in,
 * and starting or stopping impersonation — because both change who the caller
 * is. Switching accounts is the third the story names and the admin cannot do
 * it: there is no account switcher and no endpoint behind one, so that case is
 * proved on the API side instead of faked here.
 *
 * IMPORTANT: hiding is presentation, never a control. A person who edits this
 * state in a console, or types a gated address, reaches a server that refuses
 * them — see api/middleware/require_feature.go. What this buys is that nobody
 * is offered a link that leads to a refusal.
 */

export interface FeatureStatus {
  key: string
  enabled: boolean
  displayName?: string
  description?: string
  source?: string
}

export function useFeatures() {
  const features = useState<Record<string, boolean>>('featureSet', () => ({}))
  const loaded = useState<boolean>('featureSetLoaded', () => false)
  const unavailable = useState<boolean>('featureSetUnavailable', () => false)

  /**
   * Fetches the set. Goes through useApi so it uses the one request seam every
   * other composable will be moved onto, which is how #352's non-root baseURL
   * fix reaches this without touching it again.
   */
  async function load(): Promise<void> {
    const { request } = useApi()
    try {
      const list = await request<FeatureStatus[]>('/api/features')
      const next: Record<string, boolean> = {}
      for (const f of list || []) {
        next[f.key] = f.enabled
      }
      features.value = next
      unavailable.value = false
    } catch (err) {
      // Hide the gated sections rather than showing them. The server refuses
      // them anyway, so showing them would only offer links that lead to a
      // refusal — and this is NOT a sign-in problem, so it must not send
      // anybody to the sign-in page. The session-refusal plugin owns 401.
      features.value = {}
      unavailable.value = true
      console.warn('[useFeatures] could not read the feature set; gated sections are hidden', err)
    } finally {
      loaded.value = true
    }
  }

  /** Fetches the set only the first time, so navigating costs nothing. */
  async function ensureLoaded(): Promise<void> {
    if (loaded.value) return
    await load()
  }

  /**
   * Re-reads the set because the caller changed. Signing in and starting or
   * stopping impersonation both change who the answer is about.
   */
  async function refresh(): Promise<void> {
    loaded.value = false
    await load()
  }

  /**
   * Whether a feature is on for the signed-in person.
   *
   * An unknown key is OFF here, and that is the opposite of what the gates do
   * on the server, deliberately. On the server an undeclared key leaves a
   * capability where it was, because closing it would be a silent outage. In
   * the browser the same key means the set has not arrived yet, or the server
   * does not have that feature at all — and drawing a link that leads to a
   * refusal is the worse of the two mistakes, because the server refuses it
   * either way.
   */
  function isEnabled(key: string): boolean {
    return features.value[key] === true
  }

  return { features, loaded, unavailable, load, ensureLoaded, refresh, isEnabled }
}

/** Feature keys the admin gates on. Named so a typo is a build error. */
export const FEATURE_AGENT_CHAT = 'agent-chat'
