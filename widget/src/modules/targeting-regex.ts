import { RE2JS } from 're2js'

export function matches(pattern: string, value: string): boolean {
  return RE2JS.compile(pattern).test(value)
}
