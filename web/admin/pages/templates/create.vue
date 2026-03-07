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
    <h2>Create Template</h2>
    <TemplateForm :submitting="submitting" @submit="handleSubmit" />
  </div>
</template>

<script setup lang="ts">
const { createTemplate } = useTemplateApi()
const router = useRouter()
const submitting = ref(false)

async function handleSubmit(data: any) {
  submitting.value = true
  try {
    await createTemplate(data)
    router.push('/templates')
  } finally {
    submitting.value = false
  }
}
</script>
