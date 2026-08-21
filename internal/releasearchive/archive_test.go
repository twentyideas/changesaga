package releasearchive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestWriteIsDeterministicAndCanonical(t *testing.T) {
	stage := t.TempDir()
	files := map[string]string{
		"change-saga": "binary bytes",
		"LICENSE":     "license bytes",
		"README.md":   "readme bytes",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	const epoch = int64(1_700_000_000)
	for _, suffix := range []string{".tar.gz", ".zip"} {
		t.Run(suffix, func(t *testing.T) {
			first := filepath.Join(t.TempDir(), "release"+suffix)
			second := filepath.Join(t.TempDir(), "release"+suffix)
			if err := Write(first, epoch, stage, "change-saga"); err != nil {
				t.Fatal(err)
			}
			future := time.Unix(epoch+1000, 0)
			for name := range files {
				if err := os.Chtimes(filepath.Join(stage, name), future, future); err != nil {
					t.Fatal(err)
				}
			}
			if err := Write(second, epoch, stage, "change-saga"); err != nil {
				t.Fatal(err)
			}
			if fileHash(t, first) != fileHash(t, second) {
				t.Fatal("archives differ after source mtimes changed")
			}
			info, err := os.Stat(first)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o644 {
				t.Fatalf("archive mode = %o, want 644", got)
			}

			got := inspectArchive(t, first)
			want := []archiveEntry{
				{name: "change-saga", mode: 0o755, stamp: epoch, body: files["change-saga"]},
				{name: "LICENSE", mode: 0o644, stamp: epoch, body: files["LICENSE"]},
				{name: "README.md", mode: 0o644, stamp: epoch, body: files["README.md"]},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("archive entries = %#v, want %#v", got, want)
			}
		})
	}
}

type archiveEntry struct {
	name  string
	mode  int64
	stamp int64
	body  string
}

func inspectArchive(t *testing.T, path string) []archiveEntry {
	t.Helper()
	if filepath.Ext(path) == ".zip" {
		reader, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		var entries []archiveEntry
		for _, item := range reader.File {
			body, err := item.Open()
			if err != nil {
				t.Fatal(err)
			}
			contents, err := io.ReadAll(body)
			body.Close()
			if err != nil {
				t.Fatal(err)
			}
			entries = append(entries, archiveEntry{item.Name, int64(item.Mode().Perm()), item.Modified.Unix(), string(contents)})
		}
		return entries
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var entries []archiveEntry
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, archiveEntry{header.Name, header.Mode, header.ModTime.Unix(), string(contents)})
	}
}

func fileHash(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(contents)
}
