package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeSlug = regexp.MustCompile(`[^a-z0-9]+`)

func WriteJSON(path string, value any, exclusive bool) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
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

func EnsureDirWithin(root, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("metadata directory escapes the saga")
	}
	return realDir, nil
}
