package requestutils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFileMimeType_ValidUTF8Text(t *testing.T) {
	textContent := []byte("Hello, this is plain text content.")
	reader := bytes.NewReader(textContent)

	mimeType := DetectFileMimeType(reader)
	assert.Equal(t, "text/plain; charset=utf-8", mimeType)
}

func TestDetectFileMimeType_EmptyFile(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	mimeType := DetectFileMimeType(reader)
	assert.Equal(t, "application/octet-stream", mimeType,
		"read failure should default to octet-stream, not text/plain")
}
