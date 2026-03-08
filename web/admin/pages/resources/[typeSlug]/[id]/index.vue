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
      :title="`${resourceType?.name || typeSlug} Details`"
      @back="router.push(`/resources/${typeSlug}`)"
    >
      <template #extra>
        <NuxtLink :to="`/resources/${typeSlug}/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="resource" bordered :column="1">
      <a-descriptions-item label="ID">{{ resource.id }}</a-descriptions-item>
      <a-descriptions-item
        v-for="field in fields"
        :key="field.key"
        :label="field.label"
      >
        <template v-if="field.inputType === 'checkbox'">
          {{ resource[field.key] ? 'Yes' : 'No' }}
        </template>
        <template v-else>
          {{ resource[field.key] || '-' }}
        </template>
      </a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const typeSlug = route.params.typeSlug as string
const id = route.params.id as string

const { getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()
const { get } = useResourceApi(typeSlug)
const { schemaToFields } = useSchemaUtils()

const resource = ref<any>(null)
const loading = ref(true)

const resourceType = computed(() => getBySlug(typeSlug))

const fields = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToFields(schema)
  return []
})

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
  try {
    resource.value = await get(id)
  } finally {
    loading.value = false
  }
})
</script>
