export interface SessionStore {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export function pageIdentity(rawURL: string): string {
  const url = new URL(rawURL)
  return `${url.pathname}${url.search}`
}

export function suppressionKey(projectID: string, ruleID: string, rawURL: string): string {
  return `fidibeki:suppressed:${projectID}:${ruleID}:${pageIdentity(rawURL)}`
}

export function isSuppressed(store: SessionStore, projectID: string, ruleID: string, rawURL: string): boolean {
  return store.getItem(suppressionKey(projectID, ruleID, rawURL)) === '1'
}

export function suppress(store: SessionStore, projectID: string, ruleID: string, rawURL: string): void {
  store.setItem(suppressionKey(projectID, ruleID, rawURL), '1')
}
