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

interface SidebarSettingsPayload {
  hidden_slugs: string[]
  menu_groups: Record<string, string>
}

export function useSidebarSettingsApi() {
  const { request } = useApi()

  async function getGlobalSettings(role?: string): Promise<SidebarSettingsPayload> {
    const params = role ? `?role=${encodeURIComponent(role)}` : ''
    return request<SidebarSettingsPayload>(`/api/settings/sidebar${params}`)
  }

  async function saveGlobalSettings(payload: SidebarSettingsPayload, role?: string): Promise<SidebarSettingsPayload> {
    const params = role ? `?role=${encodeURIComponent(role)}` : ''
    return request<SidebarSettingsPayload>(`/api/settings/sidebar${params}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  return { getGlobalSettings, saveGlobalSettings }
}
