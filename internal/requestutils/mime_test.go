package requestutils

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFileMimeType_ValidUTF8Text(t *testing.T) {
	textContent := []byte("Hello, this is plain text content.")
	reader := bytes.NewReader(textContent)

	mimeType := DetectFileMimeType(reader, "readme.txt")
	assert.Equal(t, "text/plain; charset=utf-8", mimeType)
}

func TestDetectFileMimeType_EmptyFile(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	mimeType := DetectFileMimeType(reader, "notes.md")
	assert.True(t, strings.HasPrefix(mimeType, "text/"),
		"expected a text/* MIME type for .md, got %q", mimeType)
}

func TestDetectFileMimeType_EmptyFileUnknownExtension(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	mimeType := DetectFileMimeType(reader, "data.bin")
	assert.Equal(t, "application/octet-stream", mimeType)
}

func TestDetectFileMimeType_EmptyFileNoFilename(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	mimeType := DetectFileMimeType(reader, "")
	assert.Equal(t, "application/octet-stream", mimeType)
}
