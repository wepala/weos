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
    <a-page-header title="Website Details" @back="router.push('/websites')">
      <template #extra>
        <NuxtLink :to="`/websites/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="website" bordered :column="1">
      <a-descriptions-item label="ID">{{ website.id }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ website.name }}</a-descriptions-item>
      <a-descriptions-item label="URL">{{ website.url }}</a-descriptions-item>
      <a-descriptions-item label="Description">{{ website.description || '-' }}</a-descriptions-item>
      <a-descriptions-item label="Language">{{ website.language }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="statusColor(website.status)">{{ website.status }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Created At">{{ website.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const { getWebsite } = useWebsiteApi()
const route = useRoute()
const router = useRouter()

const id = route.params.id as string
const website = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    website.value = await getWebsite(id)
  } finally {
    loading.value = false
  }
})

function statusColor(status: string) {
  if (status === 'published') return 'green'
  if (status === 'archived') return 'red'
  return 'blue'
}
</script>
