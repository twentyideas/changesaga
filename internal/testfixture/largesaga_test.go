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

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
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

// TestGenerateLargeSagaCoverageShapeIsSelectable covers a deliberately
// fragmented input: one reference per changed line, owned by a few narrative
// targets. The authoring API no longer emits this shape, but reader benchmarks
// retain it to prove adversarial evidence cannot restore nonlinear behavior.
func TestGenerateLargeSagaCoverageShapeIsSelectable(t *testing.T) {
	options := LargeSagaOptions{
		Chapters: 2, SectionsPerChapter: 2, FragmentsPerSection: 3,
		SourceFiles: 4, ChangedLinesPerFile: 4, ReviewsPerFragment: 1,
		Threads: 1, DiffReviews: 1, CoverageRangeWidth: 1, CoverageTargets: 2,
	}
	fixture, err := GenerateLargeSaga(context.Background(), filepath.Join(t.TempDir(), "per-line"), options)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.CoverageRangeWidth != 1 || fixture.References != fixture.Atoms {
		t.Fatalf("per-line evidence was not written: width=%d references=%d atoms=%d", fixture.CoverageRangeWidth, fixture.References, fixture.Atoms)
	}
	if fixture.CoverageTargets != 2 || fixture.DiffFiles != 2 {
		t.Fatalf("evidence reached %d targets in %d diff files, want 2 of each", fixture.CoverageTargets, fixture.DiffFiles)
	}
	if got, want := fixture.MaxTargetReferences, fixture.Atoms/2; got != want {
		t.Fatalf("the largest target owns %d references, want %d", got, want)
	}

	document, validation, err := saga.Load(fixture.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid {
		t.Fatalf("concentrated per-line saga is invalid: %#v", validation.Issues)
	}
	changes, err := gitdiff.Read(context.Background(), fixture.Repository, document.Manifest.Source.Repository, fixture.Base, fixture.Head)
	if err != nil {
		t.Fatal(err)
	}
	report := coverage.Evaluate(document, validation, changes)
	if !report.Complete || report.Summary.Covered != fixture.Atoms || report.Summary.Overlapping != 0 || report.Summary.Orphaned != 0 {
		t.Fatalf("concentrating evidence changed what it covers: %#v", report.Summary)
	}
}

// TestDefaultLargeSagaOptionsKeepTheirSpreadRangedShape pins the default,
// because the budgets in internal/server and internal/cli are byte counts over
// a saga generated from it.
func TestDefaultLargeSagaOptionsKeepTheirSpreadRangedShape(t *testing.T) {
	options := DefaultLargeSagaOptions()
	if options.CoverageRangeWidth != 0 || options.CoverageTargets != 0 {
		t.Fatalf("the default fixture no longer selects the default coverage shape: %#v", options)
	}
	fixture, err := GenerateLargeSaga(context.Background(), filepath.Join(t.TempDir(), "default"), options)
	if err != nil {
		t.Fatal(err)
	}
	fragments := options.Chapters * options.SectionsPerChapter * options.FragmentsPerSection
	if fixture.CoverageRangeWidth != coverageRangeWidth || fixture.CoverageTargets != fragments || fixture.MaxTargetReferences > coverageRangeWidth*2 {
		t.Fatalf("the default fixture stopped spreading ranged evidence over every fragment: %#v", fixture)
	}
}

func TestGenerateLargeSagaRejectsImpossibleCoverageShapes(t *testing.T) {
	base := LargeSagaOptions{
		Chapters: 1, SectionsPerChapter: 1, FragmentsPerSection: 2,
		SourceFiles: 1, ChangedLinesPerFile: 1, ReviewsPerFragment: 0,
		Threads: 0, DiffReviews: 0,
	}
	cases := map[string]LargeSagaOptions{
		"negative range width":        {CoverageRangeWidth: -1},
		"negative coverage targets":   {CoverageTargets: -1},
		"more targets than fragments": {CoverageTargets: 3},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			options := base
			options.CoverageRangeWidth = overrides.CoverageRangeWidth
			options.CoverageTargets = overrides.CoverageTargets
			if _, err := GenerateLargeSaga(context.Background(), filepath.Join(t.TempDir(), "rejected"), options); err == nil {
				t.Fatal("an impossible coverage shape was accepted")
			}
		})
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
