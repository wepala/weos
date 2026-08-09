export default defineNuxtRouteMiddleware(async (to) => {
  // Auth check only runs client-side — during SSR the API proxy isn't available
  if (import.meta.server) return
  if (to.path === '/login' || to.path === '/invite') return

  const { user, loading, fetchUser } = useAuth()

  if (loading.value) {
    await fetchUser()
  }

  // A coded refusal already says why the request failed, and for two of the
  // three codes signing in again cannot fix it. Redirecting anyway sends the
  // person to a login page that hands them a fresh session with the same
  // problem, and the app bounces them straight back — for as long as they
  // keep trying. So the refusal is left to explain itself instead.
  const { refusal } = useSessionRefusal()
  if (refusal.value) return

  if (!user.value) {
    return navigateTo({ path: '/login', query: { redirect: to.fullPath } })
  }
})