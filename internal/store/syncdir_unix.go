//go:build !windows

package store

import (
	"errors"
	"os"
	"syscall"
)

// SyncDir persists directory entry changes on filesystems that support it.
func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return err
	}
	return nil
}
