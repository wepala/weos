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
      <a-button type="primary" @click="openInviteModal">Invite User</a-button>
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

    <!-- Pending Invites -->
    <div v-if="invites.length > 0" style="margin-top: 32px">
      <h3>Pending Invites</h3>
      <a-table
        :columns="inviteColumns"
        :data-source="invites"
        :pagination="false"
        :scroll="{ x: 'max-content' }"
        row-key="id"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'role'">
            {{ capitalize(record.role) }}
          </template>
          <template v-if="column.key === 'status'">
            <a-tag :color="record.status === 'pending' ? 'blue' : 'default'">
              {{ capitalize(record.status) }}
            </a-tag>
          </template>
          <template v-if="column.key === 'actions'">
            <a-button
              v-if="record.status === 'pending'"
              size="small"
              danger
              @click="revokeInvite(record.id)"
            >Revoke</a-button>
          </template>
        </template>
      </a-table>
    </div>

    <!-- Edit User Modal -->
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

    <!-- Invite User Modal -->
    <a-modal
      v-model:open="showInviteModalFlag"
      title="Invite User"
      :footer="null"
    >
      <a-form layout="vertical">
        <a-form-item label="Email" :required="true">
          <a-input v-model:value="inviteForm.email" placeholder="user@example.com" />
        </a-form-item>
        <a-form-item label="Role">
          <a-select v-model:value="inviteForm.role">
            <a-select-option v-for="r in availableRoles" :key="r" :value="r">
              {{ capitalize(r) }}
            </a-select-option>
          </a-select>
        </a-form-item>

        <div v-if="inviteLink" style="margin-bottom: 16px">
          <a-form-item label="Invite Link">
            <a-input-group compact>
              <a-input :value="inviteLink" readonly style="width: calc(100% - 80px)" />
              <a-button type="primary" @click="copyLink">Copy</a-button>
            </a-input-group>
          </a-form-item>
        </div>

        <a-form-item>
          <a-button
            type="primary"
            :loading="inviting"
            @click="handleInvite"
          >
            {{ inviteLink ? 'Generate New Link' : 'Generate Invite Link' }}
          </a-button>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'
import { unwrapEnvelope, forwardMessages } from '~/composables/useApi'

const { user, startImpersonation } = useAuth()
const router = useRouter()
const loading = ref(true)
const users = ref<any[]>([])
const invites = ref<any[]>([])
const availableRoles = ref<string[]>([])
const showEditModal = ref(false)
const editing = ref(false)
const showInviteModalFlag = ref(false)
const inviting = ref(false)
const inviteLink = ref('')

const editForm = reactive({
  id: '',
  name: '',
  email: '',
  role: '',
})

const inviteForm = reactive({
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

const inviteColumns = [
  { title: 'Email', dataIndex: 'email', key: 'email' },
  { title: 'Role', dataIndex: 'role', key: 'role' },
  { title: 'Status', dataIndex: 'status', key: 'status' },
  { title: 'Expires', dataIndex: 'expires_at', key: 'expires_at' },
  { title: 'Actions', key: 'actions', width: 100 },
]

async function fetchRoles() {
  try {
    const raw = await $fetch<unknown>('/api/settings/roles')
    const res = unwrapEnvelope<{ roles: string[] }>(raw)
    availableRoles.value = res.roles || []
  } catch (err) {
    message.warning('Could not load roles — using defaults')
    console.error('[users] fetchRoles failed:', err)
    availableRoles.value = ['admin', 'instructor']
  }
}

async function fetchUsers() {
  loading.value = true
  try {
    const raw = await $fetch<unknown>('/api/users')
    users.value = unwrapEnvelope<any[]>(raw) || []
  } catch (err: any) {
    if (err?.data) forwardMessages(err.data)
    message.error('Failed to load users')
    console.error('[users] fetchUsers failed:', err)
  } finally {
    loading.value = false
  }
}

async function fetchInvites() {
  try {
    const raw = await $fetch<unknown>('/api/invites')
    const all = unwrapEnvelope<any[]>(raw) || []
    invites.value = all.filter((i) => i.status === 'pending')
  } catch (err: any) {
    if (err?.data) forwardMessages(err.data)
    message.error('Failed to load invites')
    console.error('[users] fetchInvites failed:', err)
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

function openInviteModal() {
  inviteForm.email = ''
  inviteForm.role = availableRoles.value[0] ?? ''
  inviteLink.value = ''
  showInviteModalFlag.value = true
}

async function handleInvite() {
  if (!inviteForm.email) {
    message.error('Email is required')
    return
  }
  if (!inviteForm.role) {
    message.error('Role is required')
    return
  }
  inviting.value = true
  try {
    const raw = await $fetch<unknown>('/api/invites', {
      method: 'POST',
      body: { email: inviteForm.email, role: inviteForm.role },
    })
    const res = unwrapEnvelope<any>(raw)
    inviteLink.value = `${window.location.origin}/invite?token=${res.token}`
    message.success('Invite link generated')
    await fetchInvites()
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to create invite')
  } finally {
    inviting.value = false
  }
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(inviteLink.value)
    message.success('Link copied to clipboard')
  } catch {
    message.error('Failed to copy link')
  }
}

async function revokeInvite(id: string) {
  try {
    await $fetch(`/api/invites/${id}`, { method: 'DELETE' })
    message.success('Invite revoked')
    await fetchInvites()
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to revoke invite')
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
  fetchInvites()
})
</script>
