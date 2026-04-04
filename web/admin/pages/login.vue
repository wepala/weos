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
      <h1 style="margin-bottom: 8px">WeOS Admin</h1>
      <p style="color: #666; margin-bottom: 24px">Sign in to manage your site</p>
      <a-button type="primary" size="large" block @click="login">
        Sign in with Google
      </a-button>
    </a-card>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: false,
})

const route = useRoute()

function login() {
  const redirect = (route.query.redirect as string) || '/'
  window.location.href = '/api/auth/login?redirect=' + encodeURIComponent(redirect)
}
</script>
