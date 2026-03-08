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

const STORAGE_KEY = 'weos-sidebar-hidden-types'

export function useSidebarSettings() {
  const hiddenSlugs = useState<string[]>('sidebarHiddenTypes', () => [])

  function loadSettings() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) {
        hiddenSlugs.value = JSON.parse(raw)
      }
    } catch {
      hiddenSlugs.value = []
    }
  }

  function persist() {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(hiddenSlugs.value))
  }

  function isVisible(slug: string): boolean {
    return !hiddenSlugs.value.includes(slug)
  }

  function setVisibility(slug: string, visible: boolean) {
    if (visible) {
      hiddenSlugs.value = hiddenSlugs.value.filter((s) => s !== slug)
    } else {
      if (!hiddenSlugs.value.includes(slug)) {
        hiddenSlugs.value = [...hiddenSlugs.value, slug]
      }
    }
    persist()
  }

  function showAll() {
    hiddenSlugs.value = []
    persist()
  }

  return { hiddenSlugs, loadSettings, isVisible, setVisibility, showAll }
}
