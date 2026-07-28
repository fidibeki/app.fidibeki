export function canForceDelete(typedSlug: string, workspaceSlug: string) {
  return typedSlug.trim() === workspaceSlug
}
