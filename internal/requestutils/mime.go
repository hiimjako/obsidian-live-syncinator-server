package requestutils

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
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
// If the file is empty, it falls back to extension-based detection.
// If it cannot determine a more specific type, it returns "application/octet-stream".
func DetectFileMimeType(file io.ReadSeeker, filename string) string {
	const defaultContentType = "application/octet-stream"
	const defaultTextType = "text/plain; charset=utf-8"

	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return defaultContentType
	}

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil {
		_, _ = file.Seek(0, io.SeekStart)
		return mimeFromExtension(filename, defaultContentType)
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return defaultContentType
	}

	detectedContentType := http.DetectContentType(buf[:n])
	if detectedContentType == defaultContentType && utf8.Valid(buf[:n]) {
		return defaultTextType
	}

	return detectedContentType
}

var textExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".txt":      true,
	".csv":      true,
	".json":     true,
	".xml":      true,
	".yaml":     true,
	".yml":      true,
	".toml":     true,
	".html":     true,
	".css":      true,
	".js":       true,
	".ts":       true,
	".go":       true,
	".py":       true,
	".sh":       true,
	".svg":      true,
}

func mimeFromExtension(filename, fallback string) string {
	if filename == "" {
		return fallback
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		return fallback
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		return mimeType
	}
	if textExtensions[ext] {
		return "text/plain; charset=utf-8"
	}
	return fallback
}
