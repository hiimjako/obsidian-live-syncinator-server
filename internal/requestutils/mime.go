package requestutils

import (
	"io"
	"mime"
	"net/http"
)

func IsMultipartFormData(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "multipart/form-data" || mediaType == "multipart/mixed"
}

type File interface {
	io.Reader
	io.Seeker
}

// DetectFileMimeType detects the MIME type of the file based on its content.
// If it cannot determine a more specific one, it returns "application/octet-stream".
func DetectFileMimeType(file File) string {
	const defaultContentType = "application/octet-stream"

	// Use mime.TypeByExtension as a fallback for file extension MIME type
	// Try to detect the MIME type based on file content (magic number)
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil {
		return defaultContentType
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return defaultContentType
	}

	return http.DetectContentType(buf[:n])
}
