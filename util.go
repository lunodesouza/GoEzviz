package main

import (
	"bytes"
	"encoding/xml"
	"net"
	"net/url"
	"strings"
)

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

func xmlText(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func compactXML(value []byte) string {
	text := strings.Join(strings.Fields(string(value)), " ")
	if len(text) > 400 {
		return text[:400] + "..."
	}
	return text
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
