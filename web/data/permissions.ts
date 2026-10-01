export const tenantPermissionGroups = [
  { label: 'Conversations', permissions: ['chat:read', 'chat:write', 'chat:delete'] },
  {
    label: 'Projects',
    permissions: ['project:create', 'project:read', 'project:update', 'project:delete'],
  },
  {
    label: 'Widget rules',
    permissions: ['widget_rule:create', 'widget_rule:read', 'widget_rule:update', 'widget_rule:delete'],
  },
  {
    label: 'Feedback',
    permissions: ['feedback:read', 'feedback:update', 'feedback:delete'],
  },
  {
    label: 'Issues',
    permissions: ['issue:read', 'issue:update', 'issue:delete', 'issue:merge'],
  },
  {
    label: 'Sessions',
    permissions: ['session:read', 'session:delete'],
  },
  {
    label: 'Members',
    permissions: ['member:read', 'member:invite', 'member:update', 'member:remove'],
  },
  {
    label: 'API keys',
    permissions: ['api_key:create', 'api_key:read', 'api_key:delete'],
  },
  {
    label: 'Mobile capture',
    permissions: ['mobile_capture:create'],
  },
  {
    label: 'Email feedback',
    permissions: ['email_feedback:send'],
  },
  {
    label: 'Integrations',
    permissions: ['integration:create', 'integration:read', 'integration:update', 'integration:delete'],
  },
  {
    // testimonial:publish is listed separately from testimonial:update on
    // purpose: staging a wall is an internal edit, publishing it puts a named
    // person's words and face on the open internet.
    label: 'Testimonials',
    permissions: [
      'testimonial:read', 'testimonial:create', 'testimonial:update',
      'testimonial:delete', 'testimonial:publish',
    ],
  },
  {
    label: 'Account',
    permissions: ['billing:manage', 'billing:read', 'tenant:update', 'tenant:read'],
  },
  {
    label: 'Access audit',
    permissions: ['audit:read'],
  },
] as const

export const allTenantPermissions = tenantPermissionGroups.flatMap((group) => [...group.permissions])

export const permissionPresets = {
  admin: allTenantPermissions,
  editor: [
    'chat:read', 'chat:write',
    'project:create', 'project:read', 'project:update',
    'widget_rule:create', 'widget_rule:read', 'widget_rule:update',
    'feedback:read', 'feedback:update',
    'issue:read', 'issue:update', 'issue:merge',
    'session:read',
    'integration:create', 'integration:read', 'integration:update',
    'tenant:read',
  ],
  viewer: [
    'chat:read',
    'project:read', 'widget_rule:read', 'feedback:read', 'issue:read',
    'session:read', 'member:read', 'integration:read', 'billing:read', 'tenant:read',
  ],
} as const

export type PermissionPreset = keyof typeof permissionPresets

export function permissionLabel(permission: string) {
  const [resource, action] = permission.split(':')
  return `${action?.replaceAll('_', ' ')} ${resource?.replaceAll('_', ' ')}`
}
