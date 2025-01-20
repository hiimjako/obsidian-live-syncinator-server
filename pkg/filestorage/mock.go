package filestorage

import (
	"bytes"
	"fmt"
	"io"

	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/stretchr/testify/mock"
)

type MockFileStorage struct {
	mock.Mock
}

func (m *MockFileStorage) PersistChunk(p string, d diff.Chunk) error {
	args := m.Called(p, d)
	return args.Error(0)
}

func (m *MockFileStorage) CreateObject(c io.Reader) (string, error) {
	args := m.Called(c)
	return args.String(0), args.Error(1)
}

func (m *MockFileStorage) DeleteObject(p string) error {
	args := m.Called(p)
	return args.Error(0)
}

func (m *MockFileStorage) ReadObject(p string) (io.ReadCloser, error) {
	args := m.Called(p)

	// Ensure the first return value is an io.ReadCloser
	data, ok := args.Get(0).([]byte)
	if !ok {
		return nil, fmt.Errorf("unexpected type for mock return value: %T", args.Get(0))
	}
	return io.NopCloser(bytes.NewReader(data)), args.Error(1)
}
