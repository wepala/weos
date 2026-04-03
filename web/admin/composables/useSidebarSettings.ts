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

export function useSidebarSettings() {
  const hiddenSlugs = useState<string[]>('sidebarHiddenTypes', () => [])
  const menuGroups = useState<Record<string, string>>('sidebarMenuGroups', () => ({}))

  async function loadSettings() {
    const { getGlobalSettings } = useSidebarSettingsApi()
    try {
      const data = await getGlobalSettings()
      hiddenSlugs.value = data.hidden_slugs || []
      menuGroups.value = data.menu_groups || {}
    } catch (err) {
      console.error('[useSidebarSettings] loadSettings failed:', err)
      hiddenSlugs.value = []
      menuGroups.value = {}
    }
  }

  function isVisible(slug: string): boolean {
    return !hiddenSlugs.value.includes(slug)
  }

  function getParent(slug: string): string | undefined {
    return menuGroups.value[slug] || undefined
  }

  function getChildren(parentSlug: string): string[] {
    return Object.entries(menuGroups.value)
      .filter(([, parent]) => parent === parentSlug)
      .map(([child]) => child)
  }

  function getDescendants(slug: string): string[] {
    const descendants: string[] = []
    const visited = new Set<string>()
    const stack = getChildren(slug)
    while (stack.length > 0) {
      const current = stack.pop()!
      if (visited.has(current)) continue
      visited.add(current)
      descendants.push(current)
      stack.push(...getChildren(current))
    }
    return descendants
  }

  function getAncestors(slug: string): string[] {
    const ancestors: string[] = []
    const visited = new Set<string>()
    let current = getParent(slug)
    while (current && !visited.has(current)) {
      visited.add(current)
      ancestors.push(current)
      current = getParent(current)
    }
    return ancestors
  }

  return {
    hiddenSlugs,
    menuGroups,
    loadSettings,
    isVisible,
    getParent,
    getChildren,
    getDescendants,
    getAncestors,
  }
}
