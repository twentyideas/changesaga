package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const DefaultLockTimeout = 2 * time.Second

var (
	unsafeSlug = regexp.MustCompile(`[^a-z0-9]+`)
	faultHook  func(string, string) error
)

// WriteJSON writes one complete JSON record without exposing a partially
// written destination. The destination's parent directory must already exist.
func WriteJSON(path string, value any, exclusive bool) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(data, '\n'), 0o644, exclusive)
}

// WriteFile writes data through a same-directory temporary file. Exclusive
// writes use a hard link for an atomic create-if-absent commit; replacement
// writes use an atomic rename.
func WriteFile(path string, data []byte, perm fs.FileMode, exclusive bool) (err error) {
	dir := filepath.Dir(path)
	if info, statErr := os.Stat(dir); statErr != nil {
		return statErr
	} else if !info.IsDir() {
		return fmt.Errorf("write %s: parent is not a directory", filepath.Base(path))
	}
	temp, err := os.CreateTemp(dir, ".change-saga-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err = temp.Chmod(perm); err != nil {
		return err
	}
	if err = injectFault("before-write", path); err != nil {
		return err
	}
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = injectFault("before-file-sync", path); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = injectFault("before-commit", path); err != nil {
		return err
	}
	if exclusive {
		if err = os.Link(tempPath, path); err != nil {
			return err
		}
	} else if err = os.Rename(tempPath, path); err != nil {
		return err
	}
	if err = injectFault("before-directory-sync", path); err != nil {
		if exclusive {
			_ = os.Remove(path)
		}
		return err
	}
	if err = SyncDir(dir); err != nil {
		if exclusive {
			_ = os.Remove(path)
			_ = SyncDir(dir)
		}
		return err
	}
	return nil
}

// CommitDir builds a complete entity in a hidden same-parent staging
// directory and publishes it with one rename. Failed builds never expose the
// final entity name and temporary state is removed on every return path.
func CommitDir(root, final string, populate func(stage string) error) (err error) {
	parent := filepath.Dir(final)
	if _, err := EnsureDirWithin(root, parent); err != nil {
		return err
	}
	if _, err := os.Lstat(final); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".change-saga-stage-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := injectFault("after-stage-create", final); err != nil {
		return err
	}
	if err := populate(stage); err != nil {
		return err
	}
	if err := syncTreeDirectories(stage); err != nil {
		return err
	}
	if err := injectFault("before-stage-commit", final); err != nil {
		return err
	}
	// Saga writers hold the saga lock, so this precondition and rename provide
	// exclusive semantics between supported CLI and server writers.
	if _, err := os.Lstat(final); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	if err := SyncDir(parent); err != nil {
		_ = os.RemoveAll(final)
		_ = SyncDir(parent)
		return err
	}
	return nil
}

func syncTreeDirectories(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := SyncDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}

// WithSagaLock serializes supported writers across processes. Lock acquisition
// is bounded so a busy saga produces a clear error instead of hanging.
func WithSagaLock(root string, timeout time.Duration, operation func() error) error {
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("saga root must be a real directory")
	}
	// Advisory-lock the saga directory descriptor itself. This is process
	// scoped on supported Unix platforms and does not add an untracked lock
	// artifact to the Git-native saga.
	file, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("open saga writer lock: %w", err)
	}
	defer file.Close()
	deadline := time.Now().Add(timeout)
	for {
		locked, lockErr := tryLock(file)
		if lockErr != nil {
			return fmt.Errorf("acquire saga writer lock: %w", lockErr)
		}
		if locked {
			defer unlock(file)
			return operation()
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("saga is busy: timed out after %s waiting for the writer lock", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func EventID(now time.Time) string {
	var random [4]byte
	_, _ = rand.Read(random[:])
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random[:])
}

func Slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = unsafeSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "item"
	}
	if len(value) > 60 {
		value = strings.Trim(value[:60], "-")
	}
	return value
}

func ResolveSection(root, section string) (string, error) {
	if filepath.IsAbs(section) {
		return "", fmt.Errorf("section must be relative to the saga")
	}
	clean := filepath.Clean(section)
	if clean == "." {
		return root, nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("section escapes the saga directory")
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.HasPrefix(part, "___") {
			return "", fmt.Errorf("section cannot enter reserved ___ directories")
		}
	}
	return filepath.Join(root, clean), nil
}

// EnsureDirWithin validates every existing path component before creating
// anything, then creates missing components one at a time. Symlinks are never
// accepted, including symlinked reserved metadata directories.
func EnsureDirWithin(root, dir string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, dirAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("metadata directory escapes the saga")
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", fmt.Errorf("saga root must be a real directory")
	}
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	current := rootAbs
	firstMissing := len(parts)
	// Complete preflight happens before the first Mkdir, which guarantees that
	// a rejected existing symlink has zero filesystem side effects.
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			firstMissing = i
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("metadata path component %q must not be a symlink", part)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("metadata path component %q is not a directory", part)
		}
	}
	current = rootAbs
	for i, part := range parts {
		current = filepath.Join(current, part)
		if i < firstMissing {
			continue
		}
		if err := os.Mkdir(current, 0o755); err != nil {
			return "", err
		}
	}
	return dirAbs, nil
}

func injectFault(step, path string) error {
	if faultHook != nil {
		return faultHook(step, path)
	}
	return nil
}
