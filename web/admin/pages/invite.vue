<template>
  <div
    style="
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
      background: #f0f2f5;
    "
  >
    <a-card style="width: 400px; text-align: center">
      <template v-if="error">
        <h2 style="color: #ff4d4f">Invalid Invite</h2>
        <p style="color: #666">{{ error }}</p>
        <a-button type="primary" @click="navigateTo('/login')">Go to Login</a-button>
      </template>

      <template v-else-if="accepted">
        <h2 style="color: #52c41a">Invite Accepted</h2>
        <p style="color: #666">Your account has been activated. Redirecting...</p>
      </template>

      <template v-else-if="accepting">
        <h2>Accepting Invite...</h2>
        <a-spin size="large" />
      </template>

      <template v-else-if="needsEmail">
        <h1 style="margin-bottom: 8px">You're Invited</h1>
        <p style="color: #666; margin-bottom: 24px">
          Enter your details to accept this invitation.
        </p>
        <a-form layout="vertical">
          <a-form-item label="Email" :required="true">
            <a-input v-model:value="manualEmail" placeholder="you@example.com" />
          </a-form-item>
          <a-form-item label="Name">
            <a-input v-model:value="manualName" placeholder="Your name" />
          </a-form-item>
          <a-form-item>
            <a-button type="primary" block @click="acceptWithEmail">
              Accept Invite
            </a-button>
          </a-form-item>
        </a-form>
      </template>

      <template v-else>
        <h1 style="margin-bottom: 8px">You're Invited</h1>
        <p style="color: #666; margin-bottom: 24px">
          Sign in to accept this invitation and join the team.
        </p>
        <a-button type="primary" size="large" block @click="acceptOrLogin">
          Sign in with Google
        </a-button>
      </template>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'
import { forwardMessages } from '~/composables/useApi'

definePageMeta({
  layout: false,
})

const route = useRoute()
const { user, fetchUser } = useAuth()
const token = computed(() => (route.query.token as string) || '')
const error = ref('')
const accepting = ref(false)
const accepted = ref(false)
const needsEmail = ref(false)
const manualEmail = ref('')
const manualName = ref('')

async function acceptOrLogin() {
  if (!token.value) {
    error.value = 'No invite token provided.'
    return
  }
  const redirectBack = `/invite?token=${encodeURIComponent(token.value)}`
  window.location.href = '/api/auth/login?redirect=' + encodeURIComponent(redirectBack)
}

async function submitAccept(email: string, name: string) {
  if (!email) {
    message.error('Email is required')
    return
  }
  accepting.value = true
  try {
    const raw = await $fetch<unknown>('/api/invites/accept', {
      method: 'POST',
      body: { token: token.value, email, name },
    })
    forwardMessages(raw)
    accepted.value = true
    setTimeout(() => navigateTo('/'), 1500)
  } catch (err: any) {
    if (err?.data) forwardMessages(err.data)
    error.value = err?.data?.error || 'Failed to accept invite.'
    console.error('[invite] accept failed:', err)
  } finally {
    accepting.value = false
  }
}

async function acceptWithEmail() {
  await submitAccept(manualEmail.value, manualName.value)
}

onMounted(async () => {
  if (!token.value) {
    error.value = 'No invite token provided.'
    return
  }

  // Check if user is already authenticated (e.g., after OAuth redirect back).
  // If so, auto-accept the invite using the session email.
  await fetchUser()

  if (user.value) {
    await submitAccept(user.value.email, user.value.name || '')
  } else {
    // No authenticated user — show email form so the user can accept
    // without OAuth (e.g., dev mode where SoftAuth is not applied to
    // the accept endpoint).
    needsEmail.value = true
  }
})
</script>
