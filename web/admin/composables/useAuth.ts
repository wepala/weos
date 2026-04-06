import { forwardMessages } from './useApi'

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
  const { request } = useApi()
  const user = useState<AuthUser | null>('auth-user', () => null)
  const loading = useState<boolean>('auth-loading', () => true)
  const isImpersonating = computed(() => !!user.value?.impersonating)

  async function fetchUser() {
    try {
      user.value = await request<AuthUser>('/api/auth/me')
    } catch (err) {
      console.error('[useAuth] fetchUser failed:', err)
      user.value = null
    } finally {
      loading.value = false
    }
  }

  async function startImpersonation(agentId: string) {
    try {
      await request('/api/admin/impersonate', {
        method: 'POST',
        body: JSON.stringify({ agent_id: agentId }),
      })
      await fetchUser()
    } catch (err) {
      console.error('[useAuth] startImpersonation failed:', err)
      throw err
    }
  }

  async function stopImpersonation() {
    try {
      await request('/api/admin/stop-impersonation', { method: 'POST' })
      await fetchUser()
    } catch (err) {
      console.error('[useAuth] stopImpersonation failed:', err)
      throw err
    }
  }

  async function logout() {
    try {
      await $fetch('/api/auth/logout', { method: 'POST' })
    } catch (err: any) {
      if (err?.data) forwardMessages(err.data)
    } finally {
      user.value = null
      navigateTo('/login')
    }
  }

  return { user, loading, isImpersonating, fetchUser, logout, startImpersonation, stopImpersonation }
}
