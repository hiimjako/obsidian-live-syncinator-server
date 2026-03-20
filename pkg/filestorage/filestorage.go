package filestorage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

type Storage interface {
	// CreateObject creates an object and returns the path
	CreateObject(io.Reader) (string, error)
	// WriteObject writes an object, it returns error if the files doesn't exists
	WriteObject(string, io.Reader) error
	// DeleteObject deletes an object
	DeleteObject(string) error
	// ReadObject reads an object
	ReadObject(string) (io.ReadCloser, error)
}

func GenerateHash(file io.Reader) (string, error) {
	hash := sha256.New()
	_, err := io.Copy(hash, file)
	if err != nil {
		return "", fmt.Errorf("failed to generate hash: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
