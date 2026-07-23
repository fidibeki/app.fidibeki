package moderation

import (
	"sort"
	"strings"
)

const Placeholder = "[MODERATED]"

func RedactCopies(value string, sources ...string) string {
	unique := make(map[string]struct{}, len(sources))
	ordered := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, exists := unique[source]; exists {
			continue
		}
		unique[source] = struct{}{}
		ordered = append(ordered, source)
	}
	sort.Slice(ordered, func(left, right int) bool { return len(ordered[left]) > len(ordered[right]) })
	for _, source := range ordered {
		value = strings.ReplaceAll(value, source, Placeholder)
	}
	return value
}

func IsDerived(value string, sources ...string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source != "" && (strings.Contains(value, source) || strings.Contains(source, value)) {
			return true
		}
	}
	return false
}

func RedactDerived(value string, sources ...string) string {
	redacted := RedactCopies(value, sources...)
	if redacted != value {
		return redacted
	}
	if IsDerived(value, sources...) {
		return Placeholder
	}
	return value
}
