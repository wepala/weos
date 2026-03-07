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
    <a-page-header title="Page Details" @back="router.push('/pages')">
      <template #extra>
        <NuxtLink :to="`/pages/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="page" bordered :column="1">
      <a-descriptions-item label="ID">{{ page.id }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ page.name }}</a-descriptions-item>
      <a-descriptions-item label="Slug">{{ page.slug }}</a-descriptions-item>
      <a-descriptions-item label="Description">{{ page.description || '-' }}</a-descriptions-item>
      <a-descriptions-item label="Template">{{ page.template || '-' }}</a-descriptions-item>
      <a-descriptions-item label="Position">{{ page.position }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="page.status === 'published' ? 'green' : 'blue'">{{ page.status }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Created At">{{ page.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const { getPage } = usePageApi()
const route = useRoute()
const router = useRouter()

const id = route.params.id as string
const page = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    page.value = await getPage(id)
  } finally {
    loading.value = false
  }
})
</script>
