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
    <a-form-item label="Given Name" name="given_name" :rules="[{ required: true, message: 'Given name is required' }]">
      <a-input v-model:value="form.given_name" />
    </a-form-item>
    <a-form-item label="Family Name" name="family_name" :rules="[{ required: true, message: 'Family name is required' }]">
      <a-input v-model:value="form.family_name" />
    </a-form-item>
    <a-form-item label="Email" name="email" :rules="[{ required: true, type: 'email', message: 'Valid email is required' }]">
      <a-input v-model:value="form.email" />
    </a-form-item>
    <a-form-item v-if="isEdit" label="Avatar URL" name="avatar_url">
      <a-input v-model:value="form.avatar_url" />
    </a-form-item>
    <a-form-item v-if="isEdit" label="Status" name="status">
      <a-select v-model:value="form.status">
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
}>()

const emit = defineEmits<{
  submit: [data: any]
}>()

const form = reactive({
  given_name: props.initialData?.given_name || '',
  family_name: props.initialData?.family_name || '',
  email: props.initialData?.email || '',
  avatar_url: props.initialData?.avatar_url || '',
  status: props.initialData?.status || 'active',
})

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
