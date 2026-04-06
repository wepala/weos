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
    <a-page-header title="Role Access" @back="$router.back()" />
    <a-card>
      <template #extra>
        <a-button type="primary" :loading="saving" @click="handleSave">Save</a-button>
      </template>

      <p style="margin-bottom: 16px">
        Configure which resource types each role can access and what actions they can perform.
        Admin and owner roles always have full access. Roles with no configuration have unrestricted access.
      </p>

      <a-form-item label="Role" style="max-width: 300px; margin-bottom: 24px">
        <a-select v-model:value="selectedRole" placeholder="Select a role">
          <a-select-option v-for="r in editableRoles" :key="r" :value="r">
            {{ capitalize(r) }}
          </a-select-option>
        </a-select>
      </a-form-item>

      <template v-if="selectedRole">
        <div style="margin-bottom: 12px">
          <a-button size="small" @click="selectAll" style="margin-right: 8px">Select All</a-button>
          <a-button size="small" @click="deselectAll">Deselect All</a-button>
        </div>

        <a-table
          :columns="accessColumns"
          :data-source="resourceTypeRows"
          :pagination="false"
          row-key="slug"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'read'">
              <a-checkbox
                :checked="hasPermission(record.slug, 'read')"
                @change="(e: any) => togglePermission(record.slug, 'read', e.target.checked)"
              />
            </template>
            <template v-if="column.key === 'modify'">
              <a-checkbox
                :checked="hasPermission(record.slug, 'modify')"
                @change="(e: any) => togglePermission(record.slug, 'modify', e.target.checked)"
              />
            </template>
            <template v-if="column.key === 'delete'">
              <a-checkbox
                :checked="hasPermission(record.slug, 'delete')"
                @change="(e: any) => togglePermission(record.slug, 'delete', e.target.checked)"
              />
            </template>
          </template>
        </a-table>
      </template>

      <a-empty v-else description="Select a role to configure access" />
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

type AccessMap = Record<string, Record<string, string[]>>

const { resourceTypes, fetchResourceTypes } = useResourceTypeStore()

const roles = ref<string[]>([])
const accessMap = ref<AccessMap>({})
const selectedRole = ref<string>('')
const saving = ref(false)

const editableRoles = computed(() =>
  roles.value.filter((r) => r !== 'admin' && r !== 'owner')
)

const resourceTypeRows = computed(() =>
  resourceTypes.value.map((rt) => ({ slug: rt.slug, name: rt.name }))
)

const accessColumns = [
  { title: 'Resource Type', dataIndex: 'name', key: 'name' },
  { title: 'Read', key: 'read', width: 80, align: 'center' as const },
  { title: 'Create / Edit', key: 'modify', width: 100, align: 'center' as const },
  { title: 'Delete', key: 'delete', width: 80, align: 'center' as const },
]

function capitalize(s: string): string {
  if (!s) return ''
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function hasPermission(slug: string, action: string): boolean {
  const roleAccess = accessMap.value[selectedRole.value]
  if (!roleAccess) return false
  const actions = roleAccess[slug]
  return actions?.includes(action) || false
}

function togglePermission(slug: string, action: string, checked: boolean) {
  if (!accessMap.value[selectedRole.value]) {
    accessMap.value[selectedRole.value] = {}
  }
  const roleAccess = accessMap.value[selectedRole.value]
  if (!roleAccess[slug]) {
    roleAccess[slug] = []
  }
  if (checked) {
    if (!roleAccess[slug].includes(action)) {
      roleAccess[slug].push(action)
    }
  } else {
    roleAccess[slug] = roleAccess[slug].filter((a) => a !== action)
    if (roleAccess[slug].length === 0) {
      delete roleAccess[slug]
    }
  }
}

function selectAll() {
  if (!selectedRole.value) return
  if (!accessMap.value[selectedRole.value]) {
    accessMap.value[selectedRole.value] = {}
  }
  for (const rt of resourceTypes.value) {
    accessMap.value[selectedRole.value][rt.slug] = ['read', 'modify', 'delete']
  }
}

function deselectAll() {
  if (!selectedRole.value) return
  accessMap.value[selectedRole.value] = {}
}

async function fetchRoles() {
  try {
    const raw = await $fetch<any>('/api/settings/roles')
    const res = raw?.data !== undefined ? raw.data : raw
    roles.value = res.roles || []
  } catch {
    roles.value = []
  }
}

async function fetchAccess() {
  try {
    const raw = await $fetch<any>('/api/settings/role-access')
    const res = raw?.data !== undefined ? raw.data : raw
    accessMap.value = res.roles || {}
  } catch {
    accessMap.value = {}
  }
}

async function handleSave() {
  saving.value = true
  try {
    const raw = await $fetch<any>('/api/settings/role-access', {
      method: 'PUT',
      body: { roles: accessMap.value },
    })
    const res = raw?.data !== undefined ? raw.data : raw
    accessMap.value = res.roles || accessMap.value
    message.success('Role access saved')
  } catch {
    message.error('Failed to save role access')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  fetchRoles()
  fetchAccess()
  fetchResourceTypes()
})
</script>
