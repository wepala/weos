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
      label="Website ID"
      name="website_id"
      :rules="[{ required: true, message: 'Website ID is required' }]"
    >
      <a-input v-model:value="form.website_id" placeholder="Website ID" />
    </a-form-item>

    <a-form-item
      label="Name"
      name="name"
      :rules="[{ required: true, message: 'Name is required' }]"
    >
      <a-input v-model:value="form.name" placeholder="Home" />
    </a-form-item>

    <a-form-item label="Slug" name="slug">
      <a-input v-model:value="form.slug" placeholder="auto-generated-from-name" />
    </a-form-item>

    <a-form-item label="Description" name="description">
      <a-textarea v-model:value="form.description" :rows="3" />
    </a-form-item>

    <a-form-item label="Template" name="template">
      <a-input v-model:value="form.template" placeholder="default" />
    </a-form-item>

    <a-form-item label="Position" name="position">
      <a-input-number v-model:value="form.position" :min="0" />
    </a-form-item>

    <a-form-item v-if="isEdit" label="Status" name="status">
      <a-select v-model:value="form.status">
        <a-select-option value="draft">Draft</a-select-option>
        <a-select-option value="published">Published</a-select-option>
      </a-select>
    </a-form-item>

    <a-form-item>
      <a-space>
        <a-button type="primary" html-type="submit" :loading="submitting">
          {{ isEdit ? 'Update' : 'Create' }}
        </a-button>
        <NuxtLink to="/pages">
          <a-button>Cancel</a-button>
        </NuxtLink>
      </a-space>
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
const props = defineProps<{
  initialData?: {
    website_id: string
    name: string
    slug: string
    description: string
    template: string
    position: number
    status: string
  }
  isEdit?: boolean
  submitting?: boolean
}>()

const emit = defineEmits<{
  submit: [data: typeof form]
}>()

const form = reactive({
  website_id: props.initialData?.website_id ?? '',
  name: props.initialData?.name ?? '',
  slug: props.initialData?.slug ?? '',
  description: props.initialData?.description ?? '',
  template: props.initialData?.template ?? '',
  position: props.initialData?.position ?? 0,
  status: props.initialData?.status ?? 'draft',
})

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
