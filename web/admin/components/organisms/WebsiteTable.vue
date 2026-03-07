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
  <a-table
    :columns="columns"
    :data-source="items"
    :loading="loading"
    :pagination="false"
    row-key="id"
  >
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'status'">
        <a-tag :color="statusColor(record.status)">
          {{ record.status }}
        </a-tag>
      </template>
      <template v-if="column.key === 'actions'">
        <a-space>
          <NuxtLink :to="`/websites/${record.id}`">
            <a-button size="small">View</a-button>
          </NuxtLink>
          <NuxtLink :to="`/websites/${record.id}/edit`">
            <a-button size="small">Edit</a-button>
          </NuxtLink>
          <a-popconfirm
            title="Delete this website?"
            @confirm="$emit('delete', record.id)"
          >
            <a-button size="small" danger>Delete</a-button>
          </a-popconfirm>
        </a-space>
      </template>
    </template>
  </a-table>
  <div v-if="hasMore" style="text-align: center; margin-top: 16px">
    <a-button @click="$emit('load-more')">Load More</a-button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  items: any[]
  loading: boolean
  hasMore: boolean
}>()

defineEmits<{
  delete: [id: string]
  'load-more': []
}>()

const columns = [
  { title: 'Name', dataIndex: 'name', key: 'name' },
  { title: 'URL', dataIndex: 'url', key: 'url' },
  { title: 'Language', dataIndex: 'language', key: 'language' },
  { title: 'Status', dataIndex: 'status', key: 'status' },
  { title: 'Actions', key: 'actions', width: 250 },
]

function statusColor(status: string) {
  if (status === 'published') return 'green'
  if (status === 'archived') return 'red'
  return 'blue'
}
</script>
