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
  <a-form :model="form" layout="vertical" @finish="handleSubmit">
    <a-form-item
      v-if="!isEdit"
      label="Theme ID"
      name="theme_id"
      :rules="[{ required: true, message: 'Theme ID is required' }]"
    >
      <a-input v-model:value="form.theme_id" />
    </a-form-item>
    <a-form-item label="Name" name="name" :rules="[{ required: true, message: 'Name is required' }]">
      <a-input v-model:value="form.name" />
    </a-form-item>
    <a-form-item label="Slug" name="slug" :rules="[{ required: true, message: 'Slug is required' }]">
      <a-input v-model:value="form.slug" />
    </a-form-item>
    <a-form-item v-if="isEdit" label="Description" name="description">
      <a-textarea v-model:value="form.description" :rows="3" />
    </a-form-item>
    <a-form-item v-if="isEdit" label="File Path" name="file_path">
      <a-input v-model:value="form.file_path" />
    </a-form-item>
    <a-form-item v-if="isEdit" label="Status" name="status">
      <a-select v-model:value="form.status">
        <a-select-option value="draft">Draft</a-select-option>
        <a-select-option value="active">Active</a-select-option>
        <a-select-option value="archived">Archived</a-select-option>
      </a-select>
    </a-form-item>
    <a-form-item>
      <a-button type="primary" html-type="submit" :loading="submitting">
        {{ isEdit ? 'Update' : 'Create' }}
      </a-button>
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
const props = defineProps<{
  initialData?: any
  isEdit?: boolean
  submitting?: boolean
  themeId?: string
}>()

const emit = defineEmits<{
  submit: [data: any]
}>()

const form = reactive({
  theme_id: props.themeId || props.initialData?.theme_id || '',
  name: props.initialData?.name || '',
  slug: props.initialData?.slug || '',
  description: props.initialData?.description || '',
  file_path: props.initialData?.file_path || '',
  status: props.initialData?.status || 'draft',
})

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
