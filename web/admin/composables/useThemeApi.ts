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

interface Theme {
  id: string
  name: string
  slug: string
  description?: string
  version?: string
  thumbnail_url?: string
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreateThemePayload {
  name: string
  slug: string
}

interface UpdateThemePayload {
  name: string
  slug: string
  description: string
  version: string
  thumbnail_url: string
  status: string
}

export function useThemeApi() {
  function listThemes(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return $fetch<PaginatedResponse<Theme>>(
      `/api/themes?${params}`,
    )
  }

  function getTheme(id: string) {
    return $fetch<Theme>(`/api/themes/${id}`)
  }

  function createTheme(payload: CreateThemePayload) {
    return $fetch<Theme>('/api/themes', {
      method: 'POST',
      body: payload,
    })
  }

  function updateTheme(id: string, payload: UpdateThemePayload) {
    return $fetch<Theme>(`/api/themes/${id}`, {
      method: 'PUT',
      body: payload,
    })
  }

  function deleteTheme(id: string) {
    return $fetch(`/api/themes/${id}`, { method: 'DELETE' })
  }

  function uploadTheme(file: File) {
    const formData = new FormData()
    formData.append('theme', file)
    return $fetch<{ theme: Theme; templates: { id: string; name: string; slug: string }[] }>(
      '/api/themes/upload',
      { method: 'POST', body: formData },
    )
  }

  return {
    listThemes,
    getTheme,
    createTheme,
    updateTheme,
    deleteTheme,
    uploadTheme,
  }
}
