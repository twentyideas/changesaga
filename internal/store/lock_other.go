//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package store

import (
	"errors"
	"io/fs"
	"os"
)

func tryLock(file *os.File) (bool, error) {
	path := file.Name() + ".held"
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return true, lock.Close()
	}
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	return false, err
}

func unlock(file *os.File) { _ = os.Remove(file.Name() + ".held") }
