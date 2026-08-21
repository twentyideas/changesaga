package server

import (
	"fmt"
	"io"
	"net/url"
	"testing"

	"github.com/change-saga/change-saga/internal/coverage"
	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

func TestLargeSagaCoverageAndDrawerNavigationContracts(t *testing.T) {
	document, changes, report, changesByTarget := benchmarkCoverageFixture()
	coverageView := makeCoverageManifestView(document, changes, report)
	if len(coverageView.Files) != benchmarkCoverageFiles || len(coverageView.Targets) != benchmarkCoverageChapters*benchmarkCoverageFragmentsPerChapter {
		t.Fatalf("large coverage shape changed: files=%d targets=%d", len(coverageView.Files), len(coverageView.Targets))
	}
	firstTarget := coverageView.Targets[0]
	if len(firstTarget.Files) != 1 || len(firstTarget.Files[0].Chunks) != 1 || firstTarget.Files[0].Chunks[0].Label != "+1–16" {
		t.Fatalf("large target range was not grouped exactly: %#v", firstTarget.Files)
	}
	chunkURL, err := url.Parse(firstTarget.Files[0].Chunks[0].Href)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := diffuri.Parse(chunkURL.Query().Get("diff"))
	if err != nil || chunkURL.Query().Get("view") != "code" || selection.Start != 1 || selection.End != benchmarkCoverageLinesPerFragment {
		t.Fatalf("large target range lost its exact deep link: url=%q selection=%#v error=%v", chunkURL, selection, err)
	}

	rootView := makeSectionView(document.Section, changesByTarget, nil, nil)
	nav := makeNavTree(document.Manifest.Title, rootView)
	if len(nav) != benchmarkCoverageChapters+1 {
		t.Fatalf("large navigation lost chapters: %d", len(nav))
	}
	for _, chapter := range nav[1:] {
		if chapter.Expanded {
			t.Fatalf("chapter navigation did not remain collapsed: %#v", chapter)
		}
	}
	attached := rootView.ChildViews[0].FragmentViews[0].Attached
	if attached == nil || len(attached.Files) != 1 {
		t.Fatalf("linked drawer was not constructed: %#v", attached)
	}
	fileURL, err := url.Parse(attached.Files[0].Href)
	if err != nil || fileURL.Query().Get("view") != "code" || fileURL.Query().Get("file") != attached.Files[0].Path {
		t.Fatalf("drawer file navigation changed: url=%q error=%v", attached.Files[0].Href, err)
	}
}

const (
	benchmarkCoverageChapters            = 16
	benchmarkCoverageFragmentsPerChapter = 16
	benchmarkCoverageFiles               = 32
	benchmarkCoverageLinesPerFragment    = 16
)

func BenchmarkLargeSagaCoverageView(b *testing.B) {
	document, changes, report, _ := benchmarkCoverageFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		view := makeCoverageManifestView(document, changes, report)
		if view.MappingCount != len(changes.Atoms) {
			b.Fatal("coverage view lost mappings")
		}
	}
}

func BenchmarkLargeSagaCoverageRender(b *testing.B) {
	document, changes, report, _ := benchmarkCoverageFixture()
	view := makeCoverageManifestView(document, changes, report)
	tmpl, err := newPageTemplate()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := tmpl.ExecuteTemplate(io.Discard, "manifest-view", view); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeSagaLinkedDrawerConstruction(b *testing.B) {
	document, _, _, changesByTarget := benchmarkCoverageFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		view := makeSectionView(document.Section, changesByTarget, nil, nil)
		if len(view.ChildViews) != benchmarkCoverageChapters {
			b.Fatal("drawer view lost chapters")
		}
	}
}

// benchmarkCoverageFixture models a large but predictable review: every
// narrative target owns one contiguous range, eight targets share each file,
// and the comparison contains 4,096 changed lines in total. Keeping it wholly
// in memory makes allocation changes attributable to the view code under test.
func benchmarkCoverageFixture() (*saga.Saga, gitdiff.ChangeSet, coverage.Report, map[string][]gitdiff.Atom) {
	const (
		repository = "https://example.test/large.git"
		base       = "base-oid"
		head       = "head-oid"
	)
	root := &saga.Section{Kind: "saga", ID: "large", Title: "Large saga", Target: saga.SagaTarget("large")}
	document := &saga.Saga{
		Manifest: saga.Manifest{ID: "large", Title: "Large saga", Source: saga.Source{Repository: repository, Base: base, Head: head}},
		Section:  root,
	}
	changes := gitdiff.ChangeSet{Repository: repository, BaseOID: base, HeadOID: head}
	report := coverage.Report{Complete: true, Ownership: make(map[string][]coverage.Assignment)}
	changesByTarget := make(map[string][]gitdiff.Atom)

	targetIndex := 0
	for chapterIndex := 0; chapterIndex < benchmarkCoverageChapters; chapterIndex++ {
		chapterID := fmt.Sprintf("chapter-%02d", chapterIndex)
		chapter := &saga.Section{Kind: "chapter", ID: chapterID, Title: "Chapter " + chapterID, Target: saga.ChapterTarget("large", chapterID)}
		root.Children = append(root.Children, chapter)
		for fragmentIndex := 0; fragmentIndex < benchmarkCoverageFragmentsPerChapter; fragmentIndex++ {
			fragmentID := fmt.Sprintf("fragment-%03d", targetIndex)
			target := saga.FragmentTarget("large", fragmentID)
			fileIndex := targetIndex % benchmarkCoverageFiles
			fileTargetIndex := targetIndex / benchmarkCoverageFiles
			path := fmt.Sprintf("internal/area-%02d/file-%02d.go", fileIndex/8, fileIndex)
			start := fileTargetIndex*benchmarkCoverageLinesPerFragment + 1
			end := start + benchmarkCoverageLinesPerFragment - 1
			rangeURI := benchmarkDiffURI(diffuri.Reference{
				Repository: repository, Base: base, Head: head, Kind: "line", Path: path, Side: "new", Start: start, End: end,
			})
			fragment := &saga.Fragment{
				ID: fragmentID, Title: "Fragment " + fragmentID, Target: target, MediaType: "text/html", Entrypoint: "index.html",
				Diffs: []saga.DiffFile{{Diffs: []saga.DiffReference{{URI: rangeURI, Note: "Explains " + fragmentID}}}},
			}
			chapter.Fragments = append(chapter.Fragments, fragment)
			for line := start; line <= end; line++ {
				uri := benchmarkDiffURI(diffuri.Reference{
					Repository: repository, Base: base, Head: head, Kind: "line", Path: path, Side: "new", Start: line, End: line,
				})
				atom := gitdiff.Atom{Kind: "line", Path: path, Side: "new", Line: line, Content: fmt.Sprintf("value_%04d := %d", len(changes.Atoms), line), URI: uri}
				atom.Key = gitdiff.Key(atom)
				changes.Atoms = append(changes.Atoms, atom)
				changesByTarget[target] = append(changesByTarget[target], atom)
				report.Ownership[atom.Key] = []coverage.Assignment{{Target: target}}
			}
			targetIndex++
		}
	}
	report.Summary = coverage.Summary{Total: len(changes.Atoms), Covered: len(changes.Atoms)}
	return document, changes, report, changesByTarget
}

func benchmarkDiffURI(reference diffuri.Reference) string {
	uri, err := diffuri.Build(reference)
	if err != nil {
		panic(err)
	}
	return uri
}
