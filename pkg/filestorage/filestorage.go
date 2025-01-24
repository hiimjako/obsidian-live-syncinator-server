package filestorage

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

type Storage interface {
	// CreateObject creates an object and returns the path
	CreateObject(io.Reader) (string, error)
	// WriteObject overwrites an object, it returns error if the files doesn't exists
	WriteObject(string, io.Reader) error
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
