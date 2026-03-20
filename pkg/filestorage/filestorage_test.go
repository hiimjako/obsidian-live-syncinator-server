package filestorage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateHash(t *testing.T) {
	content := strings.NewReader("test")
	hash, err := GenerateHash(content)

	assert.NoError(t, err)
	assert.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", hash)
}

type errHashReader struct{}

func (e errHashReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

func TestGenerateHash_ReturnsErrorOnReadFailure(t *testing.T) {
	hash, err := GenerateHash(errHashReader{})
	assert.Error(t, err, "should return error when reader fails")
	assert.Empty(t, hash)
}

func TestGenerateHash_EmptyReaderNotEmpty(t *testing.T) {
	hash, err := GenerateHash(strings.NewReader(""))
	assert.NoError(t, err)
	assert.NotEmpty(t, hash, "hash of empty content should still be a valid hash")
}
