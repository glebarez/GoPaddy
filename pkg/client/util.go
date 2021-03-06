package client

import (
	"net/url"
	"strings"
)

// replace all occurrences of $ placeholder in a string, url-encoded if desired
func replacePlaceholder(s, placeholder, replacement string) string {
	replacement = url.QueryEscape(replacement)
	return strings.Replace(s, placeholder, replacement, -1)
}

// creates copy of a slice
func copySlice(slice []byte) []byte {
	sliceCopy := make([]byte, len(slice))
	copy(sliceCopy, slice)
	return sliceCopy
}
