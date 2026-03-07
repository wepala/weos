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
  <a-form
    :model="form"
    layout="vertical"
    @finish="handleSubmit"
  >
    <a-form-item
      label="Name"
      name="name"
      :rules="[{ required: true, message: 'Name is required' }]"
    >
      <a-input v-model:value="form.name" placeholder="My Website" />
    </a-form-item>

    <a-form-item
      label="URL"
      name="url"
      :rules="[{ required: true, message: 'URL is required' }]"
    >
      <a-input v-model:value="form.url" placeholder="https://example.com" />
    </a-form-item>

    <a-form-item label="Slug" name="slug">
      <a-input v-model:value="form.slug" placeholder="auto-generated-from-name" />
    </a-form-item>

    <a-form-item label="Description" name="description">
      <a-textarea v-model:value="form.description" :rows="3" />
    </a-form-item>

    <a-form-item label="Language" name="language">
      <a-input v-model:value="form.language" placeholder="en" />
    </a-form-item>

    <a-form-item v-if="isEdit" label="Status" name="status">
      <a-select v-model:value="form.status">
        <a-select-option value="draft">Draft</a-select-option>
        <a-select-option value="published">Published</a-select-option>
        <a-select-option value="archived">Archived</a-select-option>
      </a-select>
    </a-form-item>

    <a-form-item>
      <a-space>
        <a-button type="primary" html-type="submit" :loading="submitting">
          {{ isEdit ? 'Update' : 'Create' }}
        </a-button>
        <NuxtLink to="/websites">
          <a-button>Cancel</a-button>
        </NuxtLink>
      </a-space>
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
const props = defineProps<{
  initialData?: {
    name: string
    url: string
    slug: string
    description: string
    language: string
    status: string
  }
  isEdit?: boolean
  submitting?: boolean
}>()

const emit = defineEmits<{
  submit: [data: typeof form]
}>()

const form = reactive({
  name: props.initialData?.name ?? '',
  url: props.initialData?.url ?? '',
  slug: props.initialData?.slug ?? '',
  description: props.initialData?.description ?? '',
  language: props.initialData?.language ?? 'en',
  status: props.initialData?.status ?? 'draft',
})

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
