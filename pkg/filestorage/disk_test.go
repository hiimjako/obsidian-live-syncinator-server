package filestorage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
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

type errReader struct{}

func (e errReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("intentional read error")
}

func TestCreateObject_CleansUpOnCopyError(t *testing.T) {
	dir := t.TempDir()
	d := NewDisk(dir)

	_, err := d.CreateObject(errReader{})
	assert.Error(t, err)

	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Empty(t, entries, "no orphaned files should remain after io.Copy failure")
}

func TestWriteObject_CleansUpTmpOnCopyError(t *testing.T) {
	dir := t.TempDir()
	d := NewDisk(dir)

	p, err := d.CreateObject(strings.NewReader("original"))
	require.NoError(t, err)

	err = d.WriteObject(p, errReader{})
	assert.Error(t, err)

	r, err := d.ReadObject(p)
	require.NoError(t, err)
	defer r.Close()
	content, _ := io.ReadAll(r)
	assert.Equal(t, "original", string(content))

	diskPath := filepath.Join(dir, p)
	_, err = os.Stat(diskPath + ".tmp")
	assert.True(t, os.IsNotExist(err), "no .tmp file should remain after io.Copy failure")
}

func TestDisk_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	d := NewDisk(dir)

	_, err := d.ReadObject("../../etc/passwd")
	assert.Error(t, err, "path traversal in ReadObject should be rejected")

	err = d.WriteObject("../../etc/evil", strings.NewReader("pwned"))
	assert.Error(t, err, "path traversal in WriteObject should be rejected")

	err = d.DeleteObject("../../etc/important")
	assert.Error(t, err, "path traversal in DeleteObject should be rejected")
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
