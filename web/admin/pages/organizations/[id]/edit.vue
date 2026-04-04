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
    <h2>Edit Organization</h2>
    <a-spin v-if="loading" />
    <OrganizationForm
      v-else-if="org"
      :initial-data="org"
      :is-edit="true"
      :submitting="submitting"
      @submit="handleSubmit"
    />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const { getOrganization, updateOrganization } = useOrganizationApi()

const org = ref<any>(null)
const loading = ref(true)
const submitting = ref(false)

onMounted(async () => {
  try {
    org.value = await getOrganization(route.params.id as string)
  } finally {
    loading.value = false
  }
})

async function handleSubmit(data: any) {
  submitting.value = true
  try {
    await updateOrganization(route.params.id as string, data)
    router.push('/organizations')
  } finally {
    submitting.value = false
  }
}
</script>
