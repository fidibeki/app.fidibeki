import { normalizePageLanguage } from './localization'

type PageLanguageListener = (language: string | null) => void

interface DocumentLanguageState {
  language: string | null
  listeners: Set<PageLanguageListener>
  observer: MutationObserver
}

const documentStates = new WeakMap<Document, DocumentLanguageState>()

export function readPageLanguage(documentObject: Document = document): string | null {
  return normalizePageLanguage(documentObject.documentElement?.lang)
}

export function subscribeToPageLanguage(
  listener: PageLanguageListener,
  documentObject: Document = document,
): () => void {
  const root = documentObject.documentElement
  if (!root) return () => {}

  let state = documentStates.get(documentObject)
  if (!state) {
    const listeners = new Set<PageLanguageListener>()
    state = {
      language: readPageLanguage(documentObject),
      listeners,
      observer: new MutationObserver(() => {
        const current = documentStates.get(documentObject)
        if (!current) return
        const language = readPageLanguage(documentObject)
        if (language === current.language) return
        current.language = language
        for (const subscriber of current.listeners) subscriber(language)
      }),
    }
    state.observer.observe(root, { attributes: true, attributeFilter: ['lang'] })
    documentStates.set(documentObject, state)
  }

  state.listeners.add(listener)
  let subscribed = true
  return () => {
    if (!subscribed) return
    subscribed = false
    const current = documentStates.get(documentObject)
    if (!current) return
    current.listeners.delete(listener)
    if (current.listeners.size > 0) return
    current.observer.disconnect()
    documentStates.delete(documentObject)
  }
}
