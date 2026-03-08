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

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

export function useResourceApi(typeSlug: string) {
  function list(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return $fetch<PaginatedResponse<any>>(
      `/api/${typeSlug}?${params}`,
    )
  }

  function get(id: string) {
    return $fetch<any>(`/api/${typeSlug}/${id}`)
  }

  function create(data: Record<string, any>) {
    return $fetch<any>(`/api/${typeSlug}`, {
      method: 'POST',
      body: data,
    })
  }

  function update(id: string, data: Record<string, any>) {
    return $fetch<any>(`/api/${typeSlug}/${id}`, {
      method: 'PUT',
      body: data,
    })
  }

  function remove(id: string) {
    return $fetch(`/api/${typeSlug}/${id}`, { method: 'DELETE' })
  }

  return { list, get, create, update, remove }
}
