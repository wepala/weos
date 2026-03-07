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

interface Template {
  id: string
  name: string
  slug: string
  description?: string
  file_path?: string
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreateTemplatePayload {
  theme_id: string
  name: string
  slug: string
}

interface UpdateTemplatePayload {
  theme_id: string
  name: string
  slug: string
  description: string
  file_path: string
  status: string
}

export function useTemplateApi() {
  function listTemplates(cursor = '', limit = 20, themeId = '') {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    if (themeId) params.set('theme_id', themeId)
    return $fetch<PaginatedResponse<Template>>(
      `/api/templates?${params}`,
    )
  }

  function getTemplate(id: string) {
    return $fetch<Template>(`/api/templates/${id}`)
  }

  function createTemplate(payload: CreateTemplatePayload) {
    return $fetch<Template>('/api/templates', {
      method: 'POST',
      body: payload,
    })
  }

  function updateTemplate(id: string, payload: UpdateTemplatePayload) {
    return $fetch<Template>(`/api/templates/${id}`, {
      method: 'PUT',
      body: payload,
    })
  }

  function deleteTemplate(id: string) {
    return $fetch(`/api/templates/${id}`, { method: 'DELETE' })
  }

  return {
    listTemplates,
    getTemplate,
    createTemplate,
    updateTemplate,
    deleteTemplate,
  }
}
