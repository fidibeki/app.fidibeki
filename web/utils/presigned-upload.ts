export function buildPresignedPostBody(fields: Record<string, string>, file: File): FormData {
  const body = new FormData()
  for (const [name, value] of Object.entries(fields)) body.append(name, value)
  // S3-compatible POST policies require the file part to be last.
  body.append('file', file, file.name)
  return body
}
