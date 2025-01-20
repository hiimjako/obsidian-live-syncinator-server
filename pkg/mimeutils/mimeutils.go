package mimeutils

import "strings"

func IsText(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/")
}
