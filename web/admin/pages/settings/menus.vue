<!--
  Copyright (C) 2026 Wepala, LLC

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<template>
  <div>
    <a-page-header title="Sidebar Menu" @back="$router.back()" />
    <a-card>
      <template #extra>
        <a-space>
          <a-button :loading="loading" @click="loadForRole">Load</a-button>
          <a-button type="primary" :loading="saving" @click="handleSave">Save</a-button>
          <a-button @click="editMenuGroups = {}">Reset Grouping</a-button>
          <a-button @click="editHiddenSlugs = []">Show All</a-button>
        </a-space>
      </template>

      <a-form-item label="Role" style="max-width: 300px; margin-bottom: 16px">
        <a-select v-model:value="selectedRole" @change="loadForRole">
          <a-select-option value="default">Default (all roles)</a-select-option>
          <a-select-option v-for="r in editableRoles" :key="r" :value="r">
            {{ capitalize(r) }}
          </a-select-option>
        </a-select>
      </a-form-item>

      <p style="margin-bottom: 16px">
        Configure which resource types appear in the sidebar for
        <strong>{{ selectedRole === 'default' ? 'all roles' : capitalize(selectedRole) }}</strong>.
        Roles without custom settings inherit from the default.
      </p>

      <a-list :data-source="resourceTypes" :loading="!loaded">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-list-item-meta
              :title="item.name"
              :description="item.description"
              :style="{ paddingLeft: `${getDepth(item.slug) * 24}px` }"
            />
            <template #actions>
              <a-select
                :value="editMenuGroups[item.slug] || undefined"
                placeholder="Top level"
                allow-clear
                style="width: 150px; margin-right: 8px"
                @change="(val: string) => setParent(item.slug, val || null)"
              >
                <a-select-option
                  v-for="parent in availableParents(item.slug)"
                  :key="parent.slug"
                  :value="parent.slug"
                >
                  {{ parent.name }}
                </a-select-option>
              </a-select>
              <a-switch
                :checked="!editHiddenSlugs.includes(item.slug)"
                @change="(checked: boolean) => toggleVisibility(item.slug, checked)"
              />
            </template>
          </a-list-item>
        </template>
      </a-list>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'
import { unwrapEnvelope } from '~/composables/useApi'

const { resourceTypes, loaded, fetchResourceTypes } = useResourceTypeStore()
const { getGlobalSettings, saveGlobalSettings } = useSidebarSettingsApi()

const saving = ref(false)
const loading = ref(false)
const selectedRole = ref('default')
const roles = ref<string[]>([])

// Local editing state — isolated from the sidebar's shared state.
const editHiddenSlugs = ref<string[]>([])
const editMenuGroups = ref<Record<string, string>>({})

const editableRoles = computed(() =>
  roles.value.filter((r) => r !== 'admin' && r !== 'owner')
)

function capitalize(s: string): string {
  if (!s) return ''
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function toggleVisibility(slug: string, visible: boolean) {
  if (visible) {
    editHiddenSlugs.value = editHiddenSlugs.value.filter((s) => s !== slug)
  } else {
    if (!editHiddenSlugs.value.includes(slug)) {
      editHiddenSlugs.value = [...editHiddenSlugs.value, slug]
    }
  }
}

function setParent(childSlug: string, parentSlug: string | null) {
  if (parentSlug) {
    if (parentSlug === childSlug) return
    const descendants = getDescendants(childSlug)
    if (descendants.includes(parentSlug)) return
    editMenuGroups.value = { ...editMenuGroups.value, [childSlug]: parentSlug }
  } else {
    const { [childSlug]: _, ...rest } = editMenuGroups.value
    editMenuGroups.value = rest
  }
}

function getChildren(parentSlug: string): string[] {
  return Object.entries(editMenuGroups.value)
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
  let current = editMenuGroups.value[slug]
  while (current && !visited.has(current)) {
    visited.add(current)
    ancestors.push(current)
    current = editMenuGroups.value[current]
  }
  return ancestors
}

function getDepth(slug: string): number {
  return getAncestors(slug).length
}

function availableParents(slug: string) {
  const descendants = new Set(getDescendants(slug))
  return resourceTypes.value.filter((rt) => {
    if (rt.slug === slug) return false
    if (descendants.has(rt.slug)) return false
    return true
  })
}

async function loadForRole() {
  loading.value = true
  try {
    const role = selectedRole.value === 'default' ? undefined : selectedRole.value
    const data = await getGlobalSettings(role)
    editHiddenSlugs.value = data.hidden_slugs || []
    editMenuGroups.value = data.menu_groups || {}
  } catch {
    message.error('Failed to load settings')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const role = selectedRole.value === 'default' ? undefined : selectedRole.value
    await saveGlobalSettings({
      hidden_slugs: editHiddenSlugs.value,
      menu_groups: editMenuGroups.value,
    }, role)
    message.success('Settings saved')
  } catch {
    message.error('Failed to save settings')
  } finally {
    saving.value = false
  }
}

async function fetchRoles() {
  try {
    const raw = await $fetch<unknown>('/api/settings/roles')
    const res = unwrapEnvelope<any>(raw)
    roles.value = res.roles || []
  } catch {
    roles.value = []
  }
}

onMounted(() => {
  loadForRole()
  fetchRoles()
  if (!loaded.value) {
    fetchResourceTypes()
  }
})
</script>
