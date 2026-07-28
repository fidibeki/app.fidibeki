export const platformQueueRunPermission = 'platform:queue:run'

export function canRunQueueTasks(permissions: readonly string[] | undefined): boolean {
  return Boolean(permissions?.includes(platformQueueRunPermission))
}
