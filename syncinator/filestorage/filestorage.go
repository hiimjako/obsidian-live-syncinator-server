package filestorage

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/hiimjako/syncinator/syncinator/diff"
)

type Storage interface {
	// PersistChunk adds a chunk to the provided filepath, it returns an error
	// if the file doesn't exists
	PersistChunk(string, diff.Chunk) error
	// CreateObject creates an object and returns the path
	CreateObject(io.Reader) (string, error)
	// DeleteObject deletes an object
	DeleteObject(string) error
	// ReadObject reads an object
	ReadObject(string) (io.ReadCloser, error)
}

func GenerateHash(file io.Reader) string {
	hash := sha256.New()
	_, err := io.Copy(hash, file)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
