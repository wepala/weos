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
import { forwardMessages } from '~/composables/useApi'

definePageMeta({
  layout: false,
})

const route = useRoute()
const token = computed(() => (route.query.token as string) || '')
const error = ref('')
const accepting = ref(false)
const accepted = ref(false)

async function acceptOrLogin() {
  if (!token.value) {
    error.value = 'No invite token provided.'
    return
  }
  // Redirect to OAuth login with a redirect back to this page (preserving the token).
  // After login, the auth middleware will recognize the user and onMounted will
  // auto-accept below.
  const redirectBack = `/invite?token=${encodeURIComponent(token.value)}`
  window.location.href = '/api/auth/login?redirect=' + encodeURIComponent(redirectBack)
}

onMounted(async () => {
  if (!token.value) {
    error.value = 'No invite token provided.'
    return
  }

  // Check if user is already authenticated (e.g., after OAuth redirect back).
  // If so, auto-accept the invite.
  const { user, fetchUser } = useAuth()
  await fetchUser()

  if (user.value) {
    accepting.value = true
    try {
      await $fetch('/api/invites/accept', {
        method: 'POST',
        body: {
          token: token.value,
          email: user.value.email,
          name: user.value.name || '',
        },
      })
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
})
</script>
