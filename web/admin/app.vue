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
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
  <div class="api-notifications">
    <div
      v-for="n in notifications"
      :key="n.id"
      :class="['api-notification', `api-notification--${n.type}`]"
    >
      {{ n.text }}
      <button
        type="button"
        class="api-notification__close"
        aria-label="Dismiss notification"
        @click="removeNotification(n.id)"
      >x</button>
    </div>
  </div>
</template>

<script setup lang="ts">
const { notifications, removeNotification } = useNotifications()
</script>

<style scoped>
.api-notifications {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 400px;
}
.api-notification {
  padding: 10px 16px;
  border-radius: 6px;
  color: #fff;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
}
.api-notification--success { background: #52c41a; }
.api-notification--info { background: #1890ff; }
.api-notification--warning { background: #faad14; color: #000; }
.api-notification--error { background: #ff4d4f; }
.api-notification__close {
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  font-size: 14px;
  padding: 0 4px;
}
</style>
