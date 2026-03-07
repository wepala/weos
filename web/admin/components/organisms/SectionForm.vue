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
      label="Page ID"
      name="page_id"
      :rules="[{ required: true, message: 'Page ID is required' }]"
    >
      <a-input v-model:value="form.page_id" placeholder="Page ID" />
    </a-form-item>

    <a-form-item
      label="Name"
      name="name"
      :rules="[{ required: true, message: 'Name is required' }]"
    >
      <a-input v-model:value="form.name" placeholder="Hero Section" />
    </a-form-item>

    <a-form-item
      label="Slot"
      name="slot"
      :rules="[{ required: true, message: 'Slot is required' }]"
    >
      <a-input v-model:value="form.slot" placeholder="hero.headline" />
    </a-form-item>

    <a-form-item label="Entity Type" name="entity_type">
      <a-input
        v-model:value="form.entity_type"
        placeholder="Product, Event, Service..."
      />
    </a-form-item>

    <a-form-item label="Content" name="content">
      <a-textarea v-model:value="form.content" :rows="5" />
    </a-form-item>

    <a-form-item label="Position" name="position">
      <a-input-number v-model:value="form.position" :min="0" />
    </a-form-item>

    <a-form-item>
      <a-space>
        <a-button type="primary" html-type="submit" :loading="submitting">
          {{ isEdit ? 'Update' : 'Create' }}
        </a-button>
        <NuxtLink to="/sections">
          <a-button>Cancel</a-button>
        </NuxtLink>
      </a-space>
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
const props = defineProps<{
  initialData?: {
    page_id: string
    name: string
    slot: string
    entity_type: string
    content: string
    position: number
  }
  isEdit?: boolean
  submitting?: boolean
}>()

const emit = defineEmits<{
  submit: [data: typeof form]
}>()

const form = reactive({
  page_id: props.initialData?.page_id ?? '',
  name: props.initialData?.name ?? '',
  slot: props.initialData?.slot ?? '',
  entity_type: props.initialData?.entity_type ?? '',
  content: props.initialData?.content ?? '',
  position: props.initialData?.position ?? 0,
})

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
