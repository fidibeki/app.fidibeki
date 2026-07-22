const workspaceDestinations = [
  { path: '/', permissions: ['feedback:read', 'issue:read'] },
  { path: '/feedback', permissions: ['feedback:read'] },
  { path: '/issues', permissions: ['issue:read'] },
  { path: '/analytics', permissions: ['project:read'] },
  { path: '/projects', permissions: ['project:read'] },
  { path: '/settings', permissions: ['tenant:read'] },
  { path: '/billing', permissions: ['billing:read', 'billing:manage'], requireAny: true },
] as const

function destinationForPath(path: string) {
  if (path === '/') return workspaceDestinations[0]
  return workspaceDestinations.find(destination => path === destination.path || path.startsWith(`${destination.path}/`))
}

export function canAccessWorkspacePath(path: string, permissions: string[]) {
  const destination = destinationForPath(path)
  if (!destination) return true
  return hasDestinationAccess(destination, permissions)
}

export function firstAccessibleWorkspacePath(permissions: string[]) {
  return workspaceDestinations.find(destination => hasDestinationAccess(destination, permissions))?.path || '/access-denied'
}

function hasDestinationAccess(destination: typeof workspaceDestinations[number], permissions: string[]) {
  if ('requireAny' in destination && destination.requireAny) {
    return destination.permissions.some(permission => permissions.includes(permission))
  }
  return destination.permissions.every(permission => permissions.includes(permission))
}
