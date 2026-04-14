export default defineNuxtRouteMiddleware(async (to) => {
  // Auth check only runs client-side — during SSR the API proxy isn't available
  if (import.meta.server) return
  if (to.path === '/login' || to.path === '/invite') return

  const { user, loading, fetchUser } = useAuth()

  if (loading.value) {
    await fetchUser()
  }

  if (!user.value) {
    return navigateTo({ path: '/login', query: { redirect: to.fullPath } })
  }
})