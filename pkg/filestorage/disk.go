package filestorage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Disk struct {
	basepath string
}

func NewDisk(basepath string) Disk {
	return Disk{
		basepath: basepath,
	}
}

func (d Disk) CreateObject(file io.Reader) (string, error) {
	const maxIterations = 100
	for i := 0; i < maxIterations; i++ {
		id := uuid.New().String()
		relativePath := filepath.Join(strings.Split(id, "-")...)
		diskPath := filepath.Join(d.basepath, relativePath)

		_, err := os.Stat(diskPath)
		if os.IsExist(err) {
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
			return "", err
		}

		dst.Close()

		return relativePath, nil
	}

	return "", fmt.Errorf("failed to generate unique path after %d attempts", maxIterations)
}

func (d Disk) DeleteObject(relativePath string) error {
	diskPath := filepath.Join(d.basepath, relativePath)

	_, err := os.Stat(diskPath)
	if os.IsNotExist(err) {
		return nil
	}

	err = os.Remove(diskPath)

	return err
}

func (d Disk) ReadObject(relativePath string) (io.ReadCloser, error) {
	diskPath := filepath.Join(d.basepath, relativePath)

	return os.Open(diskPath)
}

func (d Disk) WriteObject(relativePath string, content io.Reader) error {
	diskPath := filepath.Join(d.basepath, relativePath)

	_, err := os.Stat(diskPath)
	if os.IsNotExist(err) {
		return err
	}

	tmpPath := diskPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, content)
	if err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	// TODO: avoid to call it each time, it is a cost.
	err = file.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	err = os.Rename(tmpPath, diskPath)
	if err != nil {
		return fmt.Errorf("failed to rename tmp file: %w", err)
	}

	return nil
}
