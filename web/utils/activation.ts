const activationPermissions = ['project:create', 'project:read', 'widget_rule:create']

export function canActivateWorkspace(permissions: string[]) {
  return activationPermissions.every(permission => permissions.includes(permission))
}
