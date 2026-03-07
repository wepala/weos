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

interface Section {
  id: string
  name: string
  slot: string
  entity_type?: string
  content?: string
  position: number
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreateSectionPayload {
  page_id: string
  name: string
  slot: string
}

interface UpdateSectionPayload {
  page_id: string
  name: string
  slot: string
  entity_type: string
  content: string
  position: number
}

export function useSectionApi() {
  function listSections(cursor = '', limit = 20, pageId = '') {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    if (pageId) params.set('page_id', pageId)
    return $fetch<PaginatedResponse<Section>>(`/api/sections?${params}`)
  }

  function getSection(id: string) {
    return $fetch<Section>(`/api/sections/${id}`)
  }

  function createSection(payload: CreateSectionPayload) {
    return $fetch<Section>('/api/sections', {
      method: 'POST',
      body: payload,
    })
  }

  function updateSection(id: string, payload: UpdateSectionPayload) {
    return $fetch<Section>(`/api/sections/${id}`, {
      method: 'PUT',
      body: payload,
    })
  }

  function deleteSection(id: string) {
    return $fetch(`/api/sections/${id}`, { method: 'DELETE' })
  }

  return {
    listSections,
    getSection,
    createSection,
    updateSection,
    deleteSection,
  }
}
