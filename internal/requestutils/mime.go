package requestutils

import (
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

func IsMultipartFormData(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "multipart/form-data" || mediaType == "multipart/mixed"
}

// DetectFileMimeType detects the MIME type of the file based on its content.
// If it cannot determine a more specific one, it returns "application/octet-stream".
func DetectFileMimeType(file io.ReadSeeker) string {
	const defaultContentType = "application/octet-stream"
	const defaultTextType = "text/plain; charset=utf-8"

	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return defaultContentType
	}

	// Use mime.TypeByExtension as a fallback for file extension MIME type
	// Try to detect the MIME type based on file content (magic number)
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil {
		return defaultTextType
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return defaultTextType
	}

	detectedContentType := http.DetectContentType(buf[:n])
	if detectedContentType == defaultContentType && utf8.Valid(buf) {
		return defaultTextType
	}

	return detectedContentType
}
