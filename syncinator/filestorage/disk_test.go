package filestorage

import (
	"bytes"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/hiimjako/syncinator/syncinator/diff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistChunk(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		tests := []struct {
			name     string
			expected string
			diffs    [][]diff.Chunk
		}{
			{
				name:     "compute remove chunk in present file",
				expected: "hello",
				diffs: [][]diff.Chunk{
					diff.Compute("hello", ""),
					diff.Compute("", "he__llo"),
					diff.Compute("he__llo", "hello"),
				},
			},
			{
				name:     "compute add chunk in present file",
				expected: "hello world!",
				diffs: [][]diff.Chunk{
					diff.Compute("", "hello"),
					diff.Compute("hello", "hello!"),
					diff.Compute("hello!", "hello world!"),
				},
			},
		}

		dir := t.TempDir()
		d := NewDisk(dir)
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				filePath, err := d.CreateObject(strings.NewReader(""))
				assert.NoError(t, err)
				for _, di := range tt.diffs {
					for _, d2 := range di {
						assert.NoError(t, d.PersistChunk(filePath, d2))
					}
				}

				fileContent, err := d.ReadObject(filePath)
				require.NoError(t, err)
				defer fileContent.Close()

				content := make([]byte, 512)
				n, err := fileContent.Read(content)
				require.NoError(t, err)

				assert.Equal(t, tt.expected, string(content[:n]))
			})
		}
	})

	t.Run("should return error on not existing file", func(t *testing.T) {
		dir := t.TempDir()
		d := NewDisk(dir)

		assert.Error(t, d.PersistChunk("not-existing-file", diff.Compute("", "foo")[0]))
	})
}

func TestDisk(t *testing.T) {
	dir := t.TempDir()
	d := NewDisk(dir)

	// create object
	content := []byte("bar")
	p, err := d.CreateObject(bytes.NewReader(content))
	assert.NoError(t, err)

	// read object
	file, err := d.ReadObject(p)
	assert.NoError(t, err)

	fileContent := make([]byte, 512)
	n, err := file.Read(fileContent)
	require.NoError(t, err)

	assert.Equal(t, content, fileContent[:n])

	// delete object
	_, err = os.Stat(path.Join(d.basepath, p))
	assert.NoError(t, err)

	err = d.DeleteObject(p)
	assert.NoError(t, err)

	_, err = os.Stat(path.Join(d.basepath, p))
	assert.True(t, os.IsNotExist(err))
}
