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
    <h2>Create {{ resourceType?.name || typeSlug }}</h2>
    <a-spin v-if="!resourceType" />
    <ResourceForm
      v-else-if="resourceType?.schema"
      :schema="resourceType.schema"
      :type-slug="typeSlug"
      :submitting="submitting"
      @submit="handleSubmit"
    />
    <a-empty v-else description="No schema defined for this resource type" />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const typeSlug = route.params.typeSlug as string

const { getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()
const { create } = useResourceApi(typeSlug)

const submitting = ref(false)

const resourceType = computed(() => getBySlug(typeSlug))

async function handleSubmit(data: Record<string, any>) {
  submitting.value = true
  try {
    await create(data)
    router.push(`/resources/${typeSlug}`)
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
})
</script>
