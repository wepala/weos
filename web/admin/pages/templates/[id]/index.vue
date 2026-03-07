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
    <a-page-header title="Template Details" @back="router.push('/templates')">
      <template #extra>
        <NuxtLink :to="`/templates/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="template" bordered :column="1">
      <a-descriptions-item label="ID">{{ template.id }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ template.name }}</a-descriptions-item>
      <a-descriptions-item label="Slug">{{ template.slug }}</a-descriptions-item>
      <a-descriptions-item label="Description">{{ template.description || '-' }}</a-descriptions-item>
      <a-descriptions-item label="File Path">{{ template.file_path || '-' }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="statusColor(template.status)">{{ template.status }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Created At">{{ template.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const { getTemplate } = useTemplateApi()
const route = useRoute()
const router = useRouter()

const id = route.params.id as string
const template = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    template.value = await getTemplate(id)
  } finally {
    loading.value = false
  }
})

function statusColor(status: string) {
  if (status === 'active') return 'green'
  if (status === 'archived') return 'red'
  return 'blue'
}
</script>
