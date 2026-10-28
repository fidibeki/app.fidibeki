import type { LocationQuery } from 'vue-router'

/** The settings sections in display order. The rail and the settings page both
 *  read this list, so a new section appears in navigation and content from one
 *  edit instead of two drifting copies. */
export const settingsSectionIDs = ['workspace', 'members', 'security', 'sso', 'keys', 'integrations', 'ai', 'health', 'privacy', 'audit'] as const
export type SettingsSectionID = typeof settingsSectionIDs[number]

/** Workspace is the bare /settings URL; every other section is ?tab=. */
export function settingsSectionRoute(id: SettingsSectionID) {
  return { path: '/settings', query: id === 'workspace' ? {} : { tab: id } }
}

export function settingsSectionFromQuery(query: LocationQuery): SettingsSectionID {
  const value = typeof query.tab === 'string' ? query.tab : 'workspace'
  return (settingsSectionIDs as readonly string[]).includes(value) ? value as SettingsSectionID : 'workspace'
}
