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

<!--
  Explains a refusal the person cannot fix by signing in again.

  Two shapes, one message. Before they are through the auth gate there is no
  page to show, so this takes the screen. Once they are working, a refusal on
  one data call is not a reason to throw away what they were doing, so it sits
  above the page instead.

  Retry is a full reload on purpose: the requests behind a page are made by
  several composables and none of them is recorded, so re-running "the failed
  request" is not something this component can honestly offer. A reload
  re-runs all of them, which is what the person means by trying again.
-->
<template>
  <div
    :class="['session-refusal', `session-refusal--${variant}`]"
    data-testid="session-refusal"
    role="alert"
  >
    <p class="session-refusal__text" data-testid="session-refusal-text">
      {{ refusal.text }}
    </p>
    <div class="session-refusal__actions">
      <button
        v-if="refusal.canRetry"
        type="button"
        class="session-refusal__action"
        data-testid="session-refusal-retry"
        @click="retry"
      >
        Try again
      </button>
      <button
        v-if="refusal.canSignIn"
        type="button"
        class="session-refusal__action"
        data-testid="session-refusal-signin"
        @click="signInAgain"
      >
        Sign in again
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { SessionRefusal } from '../composables/useSessionRefusal'

defineProps<{
  refusal: SessionRefusal
  /** `screen` before the auth gate, `banner` once the person is working. */
  variant: 'screen' | 'banner'
}>()

function retry() {
  window.location.reload()
}

function signInAgain() {
  window.location.href = '/login'
}
</script>

<style scoped>
.session-refusal {
  background: #fff2f0;
  border: 1px solid #ffccc7;
  border-radius: 6px;
  padding: 16px 20px;
  color: #262626;
}
.session-refusal--screen {
  max-width: 520px;
  margin: 96px auto;
}
.session-refusal--banner {
  position: fixed;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 9998;
  max-width: 520px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
}
.session-refusal__text {
  margin: 0;
  font-size: 14px;
  line-height: 1.5;
}
.session-refusal__actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
.session-refusal__actions:empty {
  margin-top: 0;
}
.session-refusal__action {
  padding: 6px 14px;
  border: 1px solid #d9d9d9;
  border-radius: 4px;
  background: #fff;
  cursor: pointer;
  font-size: 14px;
}
.session-refusal__action:hover {
  border-color: #40a9ff;
  color: #40a9ff;
}
</style>
