interface AuthUser {
  id: string
  name: string
  email: string
  role?: string
  avatar_url?: string
  impersonating?: boolean
  real_user?: { id: string; name?: string }
}

export function useAuth() {
  const user = useState<AuthUser | null>('auth-user', () => null)
  const loading = useState<boolean>('auth-loading', () => true)
  const isImpersonating = computed(() => !!user.value?.impersonating)

  async function fetchUser() {
    try {
      const data = await $fetch<AuthUser>('/api/auth/me')
      user.value = data
    } catch {
      user.value = null
    } finally {
      loading.value = false
    }
  }

  async function startImpersonation(agentId: string) {
    await $fetch('/api/admin/impersonate', {
      method: 'POST',
      body: { agent_id: agentId },
    })
    await fetchUser()
  }

  async function stopImpersonation() {
    await $fetch('/api/admin/stop-impersonation', { method: 'POST' })
    await fetchUser()
  }

  async function logout() {
    try {
      await $fetch('/api/auth/logout', { method: 'POST' })
    } finally {
      user.value = null
      navigateTo('/login')
    }
  }

  return { user, loading, isImpersonating, fetchUser, logout, startImpersonation, stopImpersonation }
}
