package testfixture

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/change-saga/change-saga/internal/coverage"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

func TestGenerateLargeSagaIsDeterministicAndValid(t *testing.T) {
	options := LargeSagaOptions{
		Chapters: 2, SectionsPerChapter: 2, FragmentsPerSection: 3,
		SourceFiles: 4, ChangedLinesPerFile: 4, ReviewsPerFragment: 2,
		Threads: 3, DiffReviews: 4,
	}
	first, err := GenerateLargeSaga(context.Background(), filepath.Join(t.TempDir(), "first"), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateLargeSaga(context.Background(), filepath.Join(t.TempDir(), "second"), options)
	if err != nil {
		t.Fatal(err)
	}

	if first.Base != second.Base {
		t.Fatalf("base commit is not deterministic: %q != %q", first.Base, second.Base)
	}
	firstDigest := treeDigest(t, first.Root)
	secondDigest := treeDigest(t, second.Root)
	if firstDigest != secondDigest {
		t.Fatalf("generated saga bytes are not deterministic: %x != %x", firstDigest, secondDigest)
	}

	document, validation, err := saga.Load(first.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid {
		t.Fatalf("large saga is invalid: %#v", validation.Issues)
	}
	if got, want := first.Atoms, options.SourceFiles*options.ChangedLinesPerFile*2; got != want {
		t.Fatalf("atoms = %d, want %d", got, want)
	}
	if first.Mappings != first.Atoms {
		t.Fatalf("mappings = %d, want one per atom (%d)", first.Mappings, first.Atoms)
	}
	if got, want := first.References, first.Atoms/coverageRangeWidth; got != want {
		t.Fatalf("range references = %d, want %d", got, want)
	}
	if first.DiffFiles > first.Fragments-1 {
		t.Fatalf("diff files = %d, want at most one per section fragment", first.DiffFiles)
	}
	if got, want := first.Fragments, 1+options.Chapters*options.SectionsPerChapter*options.FragmentsPerSection; got != want {
		t.Fatalf("fragments = %d, want %d", got, want)
	}
	if first.Markdown == 0 || first.SVG == 0 || first.HTML == 0 {
		t.Fatalf("fixture omitted a required asset type: %#v", first)
	}
	if got, want := first.Reviews, (first.Fragments-1)*options.ReviewsPerFragment; got != want {
		t.Fatalf("reviews = %d, want %d", got, want)
	}
	if len(document.Threads) != options.Threads || len(document.DiffReviews) != options.DiffReviews {
		t.Fatalf("review overlay counts differ: threads=%d diff reviews=%d", len(document.Threads), len(document.DiffReviews))
	}
	changes, err := gitdiff.Read(context.Background(), first.Repository, document.Manifest.Source.Repository, first.Base, first.Head)
	if err != nil {
		t.Fatal(err)
	}
	report := coverage.Evaluate(document, validation, changes)
	if !report.Complete || report.Summary.Covered != first.Atoms || report.Summary.Overlapping != 0 || report.Summary.Orphaned != 0 {
		t.Fatalf("ranged mappings do not cover the fixture exactly once: %#v", report.Summary)
	}
}

func TestDefaultLargeSagaScaleBudget(t *testing.T) {
	options := DefaultLargeSagaOptions()
	atoms := options.SourceFiles * options.ChangedLinesPerFile * 2
	fragments := options.Chapters * options.SectionsPerChapter * options.FragmentsPerSection
	if atoms < 4_000 {
		t.Fatalf("default fixture has only %d atoms; keep the benchmark workload in the thousands", atoms)
	}
	if references := atoms / coverageRangeWidth; references < 1_000 {
		t.Fatalf("default fixture has only %d ranged references; keep URI matching representative", references)
	}
	if fragments < 100 {
		t.Fatalf("default fixture has only %d section fragments; keep hierarchy traversal representative", fragments)
	}
	if options.ReviewsPerFragment*fragments+options.Threads+options.DiffReviews < 300 {
		t.Fatal("default fixture review history is too small to exercise large overlay loading")
	}
}

func treeDigest(t *testing.T, root string) [sha256.Size]byte {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(hash, filepath.ToSlash(relative))
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(hash, file); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}
