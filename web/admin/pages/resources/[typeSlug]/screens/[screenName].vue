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
    <a-page-header
      :title="screenTitle"
      @back="router.back()"
    />
    <a-spin v-if="loading" />
    <a-result
      v-else-if="error"
      status="error"
      :title="error"
      sub-title="The requested screen could not be loaded."
    >
      <template #extra>
        <NuxtLink :to="`/resources/${typeSlug}`">
          <a-button type="primary">Back to List</a-button>
        </NuxtLink>
      </template>
    </a-result>
    <component
      v-else-if="screen"
      :is="screen.component"
      :type-slug="typeSlug"
    />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const typeSlug = route.params.typeSlug as string
const screenName = route.params.screenName as string

const { fetchManifest, loadScreen } = usePresetScreens()
const { getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()

const screen = ref<{ component: any; meta: { name: string; label: string; icon?: string } } | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

const resourceType = computed(() => getBySlug(typeSlug))
const screenTitle = computed(() => {
  const typeName = resourceType.value?.name || typeSlug
  const label = screen.value?.meta?.label || screenName
  return `${typeName} — ${label}`
})

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
  await fetchManifest()
  try {
    const fileName = screenName.endsWith('.mjs') ? screenName : `${screenName}.mjs`
    const result = await loadScreen(typeSlug, fileName)
    if (!result) {
      error.value = 'Screen not found'
    } else {
      screen.value = result
    }
  } catch (err) {
    console.error(`[screenPage] loadScreen failed for ${typeSlug}/${screenName}:`, err)
    error.value = 'Failed to load screen'
  } finally {
    loading.value = false
  }
})
</script>
