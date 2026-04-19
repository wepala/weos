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
    :columns="allColumns"
    :data-source="items"
    :loading="loading"
    :pagination="false"
    :scroll="{ x: 'max-content' }"
    row-key="id"
  >
    <template #bodyCell="{ column, record, text }">
      <template v-if="column.key === 'actions'">
        <a-space>
          <NuxtLink :to="`/resources/${typeSlug}/${record.id}`">
            <a-button size="small">View</a-button>
          </NuxtLink>
          <NuxtLink :to="`/resources/${typeSlug}/${record.id}/edit`">
            <a-button size="small">Edit</a-button>
          </NuxtLink>
          <a-popconfirm
            title="Delete this resource?"
            @confirm="$emit('delete', record.id)"
          >
            <a-button size="small" danger>Delete</a-button>
          </a-popconfirm>
        </a-space>
      </template>
      <template v-else-if="column.displayKey && text">
        {{ record[column.displayKey] || text }}
      </template>
      <template v-else-if="column.format && text">
        {{ formatDate(column.format, text) }}
      </template>
    </template>
  </a-table>
  <div v-if="hasMore" style="text-align: center; margin-top: 16px">
    <a-button @click="$emit('load-more')">Load More</a-button>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  items: any[]
  columns: Record<string, any>[]
  loading: boolean
  hasMore: boolean
  typeSlug: string
}>()

defineEmits<{
  delete: [id: string]
  'load-more': []
}>()

function formatDate(format: string, value: string): string {
  const d = new Date(value)
  if (isNaN(d.getTime())) return value
  if (format === 'time') return d.toLocaleTimeString()
  if (format === 'date') return d.toLocaleDateString()
  return d.toLocaleString()
}

const allColumns = computed(() => [
  ...props.columns,
  { title: 'Actions', key: 'actions', width: 250 },
])
</script>
