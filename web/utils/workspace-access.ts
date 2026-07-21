const workspaceDestinations = [
  { path: '/', permissions: ['feedback:read', 'issue:read'] },
  { path: '/feedback', permissions: ['feedback:read'] },
  { path: '/issues', permissions: ['issue:read'] },
  { path: '/projects', permissions: ['project:read'] },
  { path: '/settings', permissions: ['tenant:read'] },
  { path: '/billing', permissions: ['billing:read'] },
] as const

function destinationForPath(path: string) {
  if (path === '/') return workspaceDestinations[0]
  return workspaceDestinations.find(destination => path === destination.path || path.startsWith(`${destination.path}/`))
}

export function canAccessWorkspacePath(path: string, permissions: string[]) {
  const destination = destinationForPath(path)
  if (!destination) return true
  return destination.permissions.every(permission => permissions.includes(permission))
}

export function firstAccessibleWorkspacePath(permissions: string[]) {
  return workspaceDestinations.find(destination =>
    destination.permissions.every(permission => permissions.includes(permission)),
  )?.path || '/access-denied'
}
