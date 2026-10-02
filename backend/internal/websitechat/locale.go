package websitechat

import (
	"regexp"
	"strings"
)

// Store only the browser's language, script and region, never private extensions.
var visitorLocalePattern = regexp.MustCompile(`^([A-Za-z]{2,3})(-[A-Za-z]{4})?(-(?:[A-Za-z]{2}|[0-9]{3}))?$`)

func normalizeVisitorLocale(value string) (string, bool) {
	if value == "" {
		return "", true
	}
	if len(value) > 35 {
		return "", false
	}
	parts := visitorLocalePattern.FindStringSubmatch(value)
	if parts == nil {
		return "", false
	}
	result := strings.ToLower(parts[1])
	if parts[2] != "" {
		result += "-" + strings.ToUpper(parts[2][1:2]) + strings.ToLower(parts[2][2:])
	}
	return result + strings.ToUpper(parts[3]), true
}
