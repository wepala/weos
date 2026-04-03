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
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
      <h2 style="margin: 0">Users</h2>
    </div>

    <a-table
      :columns="columns"
      :data-source="users"
      :loading="loading"
      :pagination="false"
      :scroll="{ x: 'max-content' }"
      row-key="id"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'role'">
          {{ capitalize(record.role) || '-' }}
        </template>
        <template v-if="column.key === 'actions'">
          <a-space>
            <a-button size="small" @click="openEditModal(record)">Edit</a-button>
            <a-button
              v-if="record.status === 'active' && record.id !== user?.id"
              size="small"
              @click="impersonate(record.id)"
            >Impersonate</a-button>
          </a-space>
        </template>
      </template>
    </a-table>

    <a-modal
      v-model:open="showEditModal"
      title="Edit User"
      @ok="handleEdit"
      :confirm-loading="editing"
    >
      <a-form layout="vertical">
        <a-form-item label="Name">
          <a-input v-model:value="editForm.name" />
        </a-form-item>
        <a-form-item label="Email">
          <a-input :value="editForm.email" disabled />
        </a-form-item>
        <a-form-item label="Role">
          <a-select v-model:value="editForm.role">
            <a-select-option v-for="r in availableRoles" :key="r" :value="r">
              {{ capitalize(r) }}
            </a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

const { user, startImpersonation } = useAuth()
const router = useRouter()
const loading = ref(true)
const users = ref<any[]>([])
const availableRoles = ref<string[]>([])
const showEditModal = ref(false)
const editing = ref(false)

const editForm = reactive({
  id: '',
  name: '',
  email: '',
  role: '',
})

function capitalize(s: string): string {
  if (!s) return ''
  return s.charAt(0).toUpperCase() + s.slice(1)
}

const columns = computed(() => {
  const roleFilters = availableRoles.value.map((r) => ({ text: capitalize(r), value: r }))
  roleFilters.unshift({ text: 'Owner', value: 'owner' })
  return [
    { title: 'Name', dataIndex: 'name', key: 'name',
      sorter: (a: any, b: any) => (a.name || '').localeCompare(b.name || '') },
    { title: 'Email', dataIndex: 'email', key: 'email' },
    { title: 'Role', dataIndex: 'role', key: 'role',
      filters: roleFilters,
      onFilter: (value: string, record: any) => record.role === value },
    { title: 'Status', dataIndex: 'status', key: 'status',
      filters: [{ text: 'Active', value: 'active' }],
      onFilter: (value: string, record: any) => record.status === value },
    { title: 'Actions', key: 'actions', width: 200 },
  ]
})

async function fetchRoles() {
  try {
    const res = await $fetch<{ roles: string[] }>('/api/settings/roles')
    availableRoles.value = res.roles || []
  } catch (err) {
    console.warn('[users] fetchRoles failed, using defaults:', err)
    availableRoles.value = ['admin', 'instructor']
  }
}

async function fetchUsers() {
  loading.value = true
  try {
    const res = await $fetch<any>('/api/users')
    users.value = res.data || []
  } catch (err: any) {
    message.error('Failed to load users')
    console.error('[users] fetchUsers failed:', err)
  } finally {
    loading.value = false
  }
}

function openEditModal(record: any) {
  editForm.id = record.id
  editForm.name = record.name || ''
  editForm.email = record.email || ''
  editForm.role = record.role || (availableRoles.value[0] ?? '')
  showEditModal.value = true
}

async function handleEdit() {
  if (!editForm.id) return
  editing.value = true
  try {
    await $fetch(`/api/users/${editForm.id}`, {
      method: 'PUT',
      body: { name: editForm.name, role: editForm.role },
    })
    showEditModal.value = false
    await fetchUsers()
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to update user')
  } finally {
    editing.value = false
  }
}

async function impersonate(agentId: string) {
  try {
    await startImpersonation(agentId)
    router.push('/')
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to start impersonation')
  }
}

onMounted(() => {
  fetchRoles()
  fetchUsers()
})
</script>
