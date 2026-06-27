package proxypool

import "strings"

func IsRecoverableErrorMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(normalized, "location is not supported") ||
		strings.Contains(normalized, "unsupported location")
}
