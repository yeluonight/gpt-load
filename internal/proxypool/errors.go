package proxypool

import (
	"net/url"
	"strings"
)

func IsRecoverableErrorMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(normalized, "location is not supported") ||
		strings.Contains(normalized, "unsupported location")
}

func IsProxyTransportError(err error, proxyURL string) bool {
	if err == nil || strings.TrimSpace(proxyURL) == "" {
		return false
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "proxyconnect") ||
		strings.Contains(message, "socks connect") ||
		strings.Contains(message, "proxy authentication") ||
		strings.Contains(message, "407 proxy") {
		return true
	}

	parsed, parseErr := url.Parse(proxyURL)
	if parseErr != nil || parsed.Host == "" {
		return false
	}
	proxyHost := strings.ToLower(parsed.Host)
	if at := strings.LastIndex(proxyHost, "@"); at >= 0 {
		proxyHost = proxyHost[at+1:]
	}
	proxyHostname := strings.ToLower(parsed.Hostname())
	if strings.Contains(message, proxyHost) || (proxyHostname != "" && strings.Contains(message, proxyHostname)) {
		return strings.Contains(message, "connection refused") ||
			strings.Contains(message, "no such host") ||
			strings.Contains(message, "i/o timeout") ||
			strings.Contains(message, "tls handshake timeout") ||
			strings.Contains(message, "connection reset") ||
			strings.Contains(message, "connection closed")
	}

	return false
}
