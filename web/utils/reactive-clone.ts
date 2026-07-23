import { toRaw } from 'vue'

export function cloneReactive<T>(value: T): T {
  return structuredClone(toRaw(value))
}
