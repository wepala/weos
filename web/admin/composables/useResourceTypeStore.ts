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

export interface ResourceTypeInfo {
  id: string
  name: string
  slug: string
  description?: string
  schema?: any
  status: string
}

interface PaginatedResponse {
  data: ResourceTypeInfo[]
  cursor: string
  has_more: boolean
}

export function useResourceTypeStore() {
  const resourceTypes = useState<ResourceTypeInfo[]>('resourceTypes', () => [])
  const loaded = useState<boolean>('resourceTypesLoaded', () => false)

  async function fetchResourceTypes() {
    try {
      const res = await $fetch<PaginatedResponse>('/api/resource-types?limit=100')
      resourceTypes.value = res.data
      loaded.value = true
    } catch {
      // API may not be running yet
    }
  }

  function getBySlug(slug: string): ResourceTypeInfo | undefined {
    return resourceTypes.value.find((rt) => rt.slug === slug)
  }

  return { resourceTypes, loaded, fetchResourceTypes, getBySlug }
}
