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
    <a-page-header title="Roles" @back="$router.back()" />
    <a-card>
      <template #extra>
        <a-button type="primary" :loading="saving" @click="handleSave">Save</a-button>
      </template>
      <p style="margin-bottom: 16px">
        Manage the roles available when editing users.
        The "admin" role cannot be removed.
      </p>
      <div style="margin-bottom: 12px">
        <a-tag
          v-for="role in roles"
          :key="role"
          :closable="role !== 'admin'"
          @close="removeRole(role)"
          style="margin-bottom: 8px"
        >
          {{ role }}
        </a-tag>
      </div>
      <a-input-group compact style="max-width: 300px">
        <a-input
          v-model:value="newRoleName"
          placeholder="New role name"
          style="width: calc(100% - 70px)"
          @pressEnter="addRole"
        />
        <a-button type="primary" @click="addRole">Add</a-button>
      </a-input-group>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

const roles = ref<string[]>([])
const newRoleName = ref('')
const saving = ref(false)

async function fetchRoles() {
  try {
    const raw = await $fetch<any>('/api/settings/roles')
    const res = raw?.data !== undefined ? raw.data : raw
    roles.value = res.roles || []
  } catch {
    roles.value = ['admin', 'instructor']
  }
}

function addRole() {
  const name = newRoleName.value.trim().toLowerCase()
  if (!name) return
  if (roles.value.includes(name)) {
    message.warning('Role already exists')
    return
  }
  roles.value.push(name)
  newRoleName.value = ''
}

function removeRole(role: string) {
  roles.value = roles.value.filter((r) => r !== role)
}

async function handleSave() {
  saving.value = true
  try {
    const raw = await $fetch<any>('/api/settings/roles', {
      method: 'PUT',
      body: { roles: roles.value },
    })
    const res = raw?.data !== undefined ? raw.data : raw
    roles.value = res.roles || roles.value
    message.success('Roles saved')
  } catch {
    message.error('Failed to save roles')
  } finally {
    saving.value = false
  }
}

onMounted(fetchRoles)
</script>
