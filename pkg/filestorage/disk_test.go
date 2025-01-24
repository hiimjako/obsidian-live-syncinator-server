package filestorage

import (
	"bytes"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteObject(t *testing.T) {
	t.Run("should overwrite an existing file", func(t *testing.T) {
		dir := t.TempDir()
		d := NewDisk(dir)

		filePath, err := d.CreateObject(strings.NewReader("foo"))
		assert.NoError(t, err)

		err = d.WriteObject(filePath, strings.NewReader("bar"))
		assert.NoError(t, err)

		fileContent, err := d.ReadObject(filePath)
		require.NoError(t, err)
		defer fileContent.Close()

		content := make([]byte, 512)
		n, err := fileContent.Read(content)
		require.NoError(t, err)

		assert.Equal(t, "bar", string(content[:n]))
	})

	t.Run("should return error on not existing file", func(t *testing.T) {
		dir := t.TempDir()
		d := NewDisk(dir)

		assert.Error(t, d.WriteObject("not-existing-file", strings.NewReader("foo")))
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
