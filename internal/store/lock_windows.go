//go:build windows

package store

import (
	"errors"
	"os"
	"sync"
	"syscall"
)

// Windows does not provide flock, and create/delete lock files can return a
// transient access-denied error while another goroutine is releasing one. Keep
// a no-sharing handle open instead. The kernel releases it if the process
// exits, and OPEN_ALWAYS means a stale filename never becomes a stale lock.
var windowsLockHandles sync.Map

const errorSharingViolation syscall.Errno = 32

func tryLock(file *os.File) (bool, error) {
	path := file.Name() + ".held"
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err == nil {
		windowsLockHandles.Store(path, handle)
		return true, nil
	}
	if errors.Is(err, errorSharingViolation) || errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return false, nil
	}
	return false, err
}

func unlock(file *os.File) {
	path := file.Name() + ".held"
	if value, ok := windowsLockHandles.LoadAndDelete(path); ok {
		_ = syscall.CloseHandle(value.(syscall.Handle))
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err == nil {
		_ = syscall.DeleteFile(name)
	}
}
