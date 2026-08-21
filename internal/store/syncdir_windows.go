//go:build windows

package store

import "os"

// SyncDir validates that path can be opened. Windows does not support syncing
// a directory handle through os.File.Sync; file contents are synced before the
// atomic rename that publishes them.
func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return dir.Close()
}
