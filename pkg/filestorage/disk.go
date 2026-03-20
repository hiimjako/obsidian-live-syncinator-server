package filestorage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var _ Storage = Disk{}

type Disk struct {
	basepath string
}

func NewDisk(basepath string) Disk {
	return Disk{
		basepath: basepath,
	}
}

func (d Disk) resolvePath(relativePath string) (string, error) {
	diskPath := filepath.Join(d.basepath, relativePath)
	absPath, err := filepath.Abs(diskPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absBase, err := filepath.Abs(d.basepath)
	if err != nil {
		return "", fmt.Errorf("invalid basepath: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return "", fmt.Errorf("path traversal detected: %s escapes basepath", relativePath)
	}
	return diskPath, nil
}

func (d Disk) CreateObject(file io.Reader) (string, error) {
	const maxIterations = 100
	for i := 0; i < maxIterations; i++ {
		id := uuid.New().String()
		relativePath := filepath.Join(strings.Split(id, "-")...)
		diskPath := filepath.Join(d.basepath, relativePath)

		_, err := os.Stat(diskPath)
		if err == nil {
			// file already exists, retry with a new UUID
			continue
		}

		dir := filepath.Dir(diskPath)
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return "", err
		}

		dst, err := os.Create(diskPath)
		if err != nil {
			return "", err
		}

		if _, err = io.Copy(dst, file); err != nil {
			dst.Close()
			os.Remove(diskPath)
			// best-effort cleanup of created parent directories
			for dir := filepath.Dir(diskPath); dir != d.basepath; dir = filepath.Dir(dir) {
				if rmErr := os.Remove(dir); rmErr != nil {
					break
				}
			}
			return "", err
		}

		dst.Close()

		return relativePath, nil
	}

	return "", fmt.Errorf("failed to generate unique path after %d attempts", maxIterations)
}

func (d Disk) DeleteObject(relativePath string) error {
	diskPath, err := d.resolvePath(relativePath)
	if err != nil {
		return err
	}

	_, err = os.Stat(diskPath)
	if os.IsNotExist(err) {
		return nil
	}

	err = os.Remove(diskPath)

	return err
}

func (d Disk) ReadObject(relativePath string) (io.ReadCloser, error) {
	diskPath, err := d.resolvePath(relativePath)
	if err != nil {
		return nil, err
	}

	return os.Open(diskPath)
}

func (d Disk) WriteObject(relativePath string, content io.Reader) error {
	diskPath, err := d.resolvePath(relativePath)
	if err != nil {
		return err
	}

	_, err = os.Stat(diskPath)
	if os.IsNotExist(err) {
		return err
	}

	tmpPath := diskPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, content)
	if err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write content: %w", err)
	}
	file.Close()

	err = os.Rename(tmpPath, diskPath)
	if err != nil {
		return fmt.Errorf("failed to rename tmp file: %w", err)
	}

	return nil
}
