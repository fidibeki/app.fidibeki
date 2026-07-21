import { feedbackItems } from '~/data/design-fixtures'
import { defaultFilters, filterFeedback } from '~/utils/design'

export function usePrototypeState() {
  const filters = reactive(defaultFilters())
  const selectedId = ref(feedbackItems[0]!.id)
  const selectedIds = ref<string[]>([])
  const notice = ref('')

  const filteredItems = computed(() => filterFeedback(feedbackItems, filters))
  const selected = computed(
    () => feedbackItems.find((item) => item.id === selectedId.value) ?? filteredItems.value[0] ?? feedbackItems[0]!,
  )

  function select(id: string) {
    selectedId.value = id
  }

  function toggleSelected(id: string) {
    selectedIds.value = selectedIds.value.includes(id)
      ? selectedIds.value.filter((item) => item !== id)
      : [...selectedIds.value, id]
  }

  function confirm(message: string) {
    notice.value = message
    window.setTimeout(() => {
      if (notice.value === message) notice.value = ''
    }, 2400)
  }

  return {
    filters,
    filteredItems,
    selected,
    selectedId,
    selectedIds,
    notice,
    select,
    toggleSelected,
    confirm,
  }
}
