import type {
  NotificationCategory,
  NotificationMode,
  NotificationPreferences,
} from '~/types/api'

const digestModes: NotificationMode[] = ['daily', 'weekly']

/** True when any category is routed to a digest, which is what makes the
 *  delivery hour meaningful. */
export function usesDigest(
  preferences: NotificationPreferences,
  categories: NotificationCategory[],
): boolean {
  return categories.some(category => digestModes.includes(preferences[category]))
}

/** True when any category is routed to the weekly digest, which is what makes
 *  the delivery weekday meaningful. */
export function usesWeeklyDigest(
  preferences: NotificationPreferences,
  categories: NotificationCategory[],
): boolean {
  return categories.some(category => preferences[category] === 'weekly')
}

/**
 * Builds the PATCH body. Only the categories the server offered are sent, so a
 * client that does not know about a category cannot silently reset it, and an
 * inherited timezone is sent as an explicit null to clear the member override.
 */
export function preferencePatch(
  preferences: NotificationPreferences,
  categories: NotificationCategory[],
  timezone: string,
  inheritTimezone: boolean,
): Record<string, unknown> {
  const patch: Record<string, unknown> = {
    digest_hour: preferences.digest_hour,
    digest_weekday: preferences.digest_weekday,
    timezone: inheritTimezone ? null : timezone.trim() || null,
  }
  for (const category of categories) {
    patch[category] = preferences[category]
  }
  return patch
}
