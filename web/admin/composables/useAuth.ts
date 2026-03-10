interface AuthUser {
  id: string
  name: string
  email: string
  avatar_url?: string
}

export function useAuth() {
  const user = useState<AuthUser | null>('auth-user', () => null)
  const loading = useState<boolean>('auth-loading', () => true)

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

  async function logout() {
    try {
      await $fetch('/api/auth/logout', { method: 'POST' })
    } finally {
      user.value = null
      navigateTo('/login')
    }
  }

  return { user, loading, fetchUser, logout }
}
