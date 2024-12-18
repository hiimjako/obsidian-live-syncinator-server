package filestorage

import (
	"crypto/sha256"
	"fmt"

	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/diff"
)

type Storage interface {
	// PersistChunk adds a chunk to the provided filepath, it returns an error
	// if the file doesn't exists
	PersistChunk(string, diff.DiffChunk) error
	// CreateObject creates an object and returns the path
	CreateObject([]byte) (string, error)
	// DeleteObject deletes an object
	DeleteObject(string) error
	// ReadObject reads an object
	ReadObject(string) ([]byte, error)
}

func GenerateHash(content []byte) string {
	hash := sha256.New()
	hash.Write(content)
	checksum := fmt.Sprintf("%x", hash.Sum(nil))
	return checksum
}
