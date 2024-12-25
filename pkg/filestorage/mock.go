package filestorage

import (
	"io"

	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/diff"
	"github.com/stretchr/testify/mock"
)

type MockFileStorage struct {
	mock.Mock
}

func (m *MockFileStorage) PersistChunk(p string, d diff.DiffChunk) error {
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

func (m *MockFileStorage) ReadObject(p string) ([]byte, error) {
	args := m.Called(p)
	return args.Get(0).([]byte), args.Error(1)
}
