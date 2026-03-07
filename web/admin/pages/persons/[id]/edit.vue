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
    <h2>Edit Person</h2>
    <a-spin v-if="loading" />
    <PersonForm
      v-else-if="person"
      :initial-data="person"
      :is-edit="true"
      :submitting="submitting"
      @submit="handleSubmit"
    />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const { getPerson, updatePerson } = usePersonApi()

const person = ref<any>(null)
const loading = ref(true)
const submitting = ref(false)

onMounted(async () => {
  try {
    person.value = await getPerson(route.params.id as string)
  } finally {
    loading.value = false
  }
})

async function handleSubmit(data: any) {
  submitting.value = true
  try {
    await updatePerson(route.params.id as string, data)
    router.push('/persons')
  } finally {
    submitting.value = false
  }
}
</script>
