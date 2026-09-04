package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func BenchmarkMakeCodeReviewViewLargeSaga(b *testing.B) {
	document, changes, report, selection := largeCodeViewFixture(b, 120, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		view, selectionErr := makeCodeReviewView(document, changes, report, nil, selection)
		if selectionErr != nil {
			b.Fatal(selectionErr)
		}
		if view.SelectedFile == nil || len(view.Files) != 120 || len(view.NarrativeOwnership) != 120 {
			b.Fatalf("incomplete code view: %#v", view)
		}
	}
}

func TestCodeDiffURLPreservesPathAndQualifiedDiffURI(t *testing.T) {
	diffURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/org/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "dir/a b&c.go", Side: "new", Start: 7, End: 9,
	})
	href := CodeDiffURL("dir/a b&c.go", diffURI)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("view") != "code" || parsed.Query().Get("file") != "dir/a b&c.go" || parsed.Query().Get("diff") != diffURI {
		t.Fatalf("URL did not round trip selection: %s", href)
	}
}

func TestChangedFileTreeIsNestedAndAggregatesReviewState(t *testing.T) {
	files := []*FileDiffView{
		{Path: "README.md", Deleted: 3},
		{Path: "src/api/handler.go", Added: 2, Deleted: 1, Reviewed: true, Selected: true},
		{Path: "src/ui/view.js", Added: 4},
	}
	tree := makeChangedFileTree(files)
	if tree.FileCount != 3 || tree.ReviewedCount != 1 || tree.Added != 6 || tree.Deleted != 4 {
		t.Fatalf("unexpected root aggregates: %#v", tree)
	}
	if len(tree.Nodes) != 2 || tree.Nodes[0].Name != "src" || tree.Nodes[0].Kind != "folder" {
		t.Fatalf("expected sorted folder and file roots: %#v", tree.Nodes)
	}
	src := tree.Nodes[0]
	if !src.Selected || !src.Expanded || src.FileCount != 2 || src.ReviewedCount != 1 || len(src.Children) != 2 {
		t.Fatalf("unexpected src folder state: %#v", src)
	}
	api := src.Children[0]
	if api.Name != "api" || !api.Selected || !api.Expanded || len(api.Children) != 1 || api.Children[0].Kind != "file" || !api.Children[0].Selected {
		t.Fatalf("selected path was not propagated through folders: %#v", api)
	}
}

func TestCodeReviewViewScopesReverseOwnershipAndKeepsForwardLinks(t *testing.T) {
	document, changes, report, secondURI, staleURI := codeViewFixture(t)
	view, selectionErr := makeCodeReviewView(document, changes, report, nil, codeSelection{filePath: "src/api/handler.go"})
	if selectionErr != nil {
		t.Fatal(selectionErr)
	}
	if view.SelectedFile == nil || view.SelectedFile.Path != "src/api/handler.go" || !view.SelectedFile.Selected {
		t.Fatalf("unexpected selected file: %#v", view.SelectedFile)
	}
	if view.Tree.FileCount != 2 || len(view.RelatedSaga) != 2 || view.RelatedSaga[0].Title != "Overview" || view.RelatedSaga[1].Title != "Backend" {
		t.Fatalf("file ownership was not grouped in saga order: %#v", view.RelatedSaga)
	}
	flow := view.RelatedSaga[1].Fragments[0]
	if strings.Contains(strings.ToLower(flow.Excerpt), "script") || strings.Contains(flow.Excerpt, "alert") || !strings.Contains(flow.Excerpt, "Visible explanation") {
		t.Fatalf("excerpt was not safe visible text: %q", flow.Excerpt)
	}
	if flow.Href != sagaHref(flow.Target) || len(flow.DiffURIs) != 2 {
		t.Fatalf("fragment reverse link is not exact: %#v", flow)
	}

	view, selectionErr = makeCodeReviewView(document, changes, report, nil, codeSelection{diffURI: secondURI})
	if selectionErr != nil {
		t.Fatal(selectionErr)
	}
	if view.SelectedDiff == nil || view.SelectedDiff.URI != secondURI || len(view.RelatedSaga) != 1 || view.RelatedSaga[0].Title != "Backend" {
		t.Fatalf("exact diff selection did not narrow reverse ownership: %#v", view.RelatedSaga)
	}

	var flowOwnership *FragmentOwnershipView
	for _, ownership := range view.NarrativeOwnership {
		if ownership.FragmentID == "flow" {
			flowOwnership = ownership
		}
	}
	if flowOwnership == nil || len(flowOwnership.Diffs) != 2 || !flowOwnership.Diffs[0].Available || len(flowOwnership.Diffs[0].MatchedURIs) != 2 {
		t.Fatalf("missing forward available ownership: %#v", flowOwnership)
	}
	forwardURL, err := url.Parse(flowOwnership.Diffs[0].Href)
	if err != nil {
		t.Fatal(err)
	}
	if forwardURL.Query().Get("file") != "src/api/handler.go" || forwardURL.Query().Get("diff") != flowOwnership.Diffs[0].URI {
		t.Fatalf("forward ownership deep link lost its exact selection: %s", flowOwnership.Diffs[0].Href)
	}
	if flowOwnership.Diffs[1].Available || flowOwnership.Diffs[1].URI != staleURI || flowOwnership.Diffs[1].Reason != "diff URI does not match the current source comparison" {
		t.Fatalf("missing fully-qualified stale ownership: %#v", flowOwnership.Diffs[1])
	}
}

func TestCodeReviewSelectionRejectsUnknownOrMismatchedValues(t *testing.T) {
	document, changes, report, _, _ := codeViewFixture(t)
	fileURI := mustDiffURI(t, diffuri.Reference{
		Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID,
		Kind: "file", Path: "src/api/handler.go",
	})
	view, selectionErr := makeCodeReviewView(document, changes, report, nil, codeSelection{diffURI: fileURI})
	if selectionErr != nil || view.SelectedFile.Path != "src/api/handler.go" || len(view.SelectedDiffs) != 2 || len(view.RelatedSaga) != 2 {
		t.Fatalf("qualified file selection failed: view=%#v error=%v", view, selectionErr)
	}
	_, selectionErr = makeCodeReviewView(document, changes, report, nil, codeSelection{filePath: "missing.go"})
	if selectionErr == nil || selectionErr.status != http.StatusNotFound {
		t.Fatalf("unknown file error = %#v", selectionErr)
	}
	_, selectionErr = makeCodeReviewView(document, changes, report, nil, codeSelection{diffURI: "not-a-diff-uri"})
	if selectionErr == nil || selectionErr.status != http.StatusBadRequest {
		t.Fatalf("malformed diff error = %#v", selectionErr)
	}
	foreign := mustDiffURI(t, diffuri.Reference{
		Repository: "https://elsewhere.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "src/api/handler.go", Side: "new", Start: 11, End: 11,
	})
	_, selectionErr = makeCodeReviewView(document, changes, report, nil, codeSelection{diffURI: foreign})
	if selectionErr == nil || selectionErr.status != http.StatusNotFound {
		t.Fatalf("foreign diff error = %#v", selectionErr)
	}
	foreignFile := mustDiffURI(t, diffuri.Reference{
		Repository: "https://elsewhere.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "file", Path: "src/api/handler.go",
	})
	_, selectionErr = makeCodeReviewView(document, changes, report, nil, codeSelection{diffURI: foreignFile})
	if selectionErr == nil || selectionErr.status != http.StatusNotFound {
		t.Fatalf("foreign file diff error = %#v", selectionErr)
	}
	emptyDocument := &saga.Saga{Manifest: saga.Manifest{ID: "empty"}, Section: &saga.Section{Target: saga.SagaTarget("empty")}}
	_, selectionErr = makeCodeReviewView(emptyDocument, gitdiff.ChangeSet{}, coverage.Report{}, nil, codeSelection{diffURI: fileURI})
	if selectionErr == nil || selectionErr.status != http.StatusNotFound {
		t.Fatalf("diff against empty comparison error = %#v", selectionErr)
	}
}

func TestRelatedSagaEmptyStateIsExplicit(t *testing.T) {
	document, changes, report, _, _ := codeViewFixture(t)
	view, selectionErr := makeCodeReviewView(document, changes, report, nil, codeSelection{filePath: "docs/unowned.md"})
	if selectionErr != nil {
		t.Fatal(selectionErr)
	}
	if len(view.RelatedSaga) != 0 || view.RelatedEmpty == "" {
		t.Fatalf("missing empty narrative state: %#v", view)
	}
}

func TestRelatedSagaLinksBackToExactLandmark(t *testing.T) {
	fragment := &saga.Fragment{ID: "flow", Title: "Flow", Target: saga.FragmentTarget("test", "flow")}
	landmarkTarget := saga.LandmarkTarget("test", "flow", "submit-action")
	location := narrativeLocation{
		fragment: fragment, target: landmarkTarget, itemID: "submit-action", title: "Submit action",
		chapterID: "backend", chapterTitle: "Backend", chapterTarget: saga.ChapterTarget("test", "backend"),
		chapterHref: sagaHref(saga.ChapterTarget("test", "backend")), fragmentHref: sagaHref(fragment.Target) + "--submit-action",
	}
	atom := &diffAtomView{Atom: gitdiff.Atom{Key: "changed", URI: "saga-diff://example"}}
	result := makeRelatedSagaViews([]narrativeLocation{location}, []*diffAtomView{atom}, map[string][]coverage.Assignment{
		"changed": {{Target: landmarkTarget}},
	})
	if len(result) != 1 || len(result[0].Fragments) != 1 || result[0].Fragments[0].Title != "Submit action" || result[0].Fragments[0].Href != location.fragmentHref || result[0].Fragments[0].Anchor != strings.TrimPrefix(location.fragmentHref, "#") {
		t.Fatalf("landmark reverse link = %#v", result)
	}
}

func TestRelatedSagaRollsItemOwnersUpToSlidesAndGroupsByDeck(t *testing.T) {
	deckTarget := saga.DeckTarget("visual", "overview")
	firstTarget := saga.SlideTarget("visual", "flow")
	secondTarget := saga.SlideTarget("visual", "failure")
	first := &saga.Fragment{
		ID: "flow", Title: "Request flow", Target: firstTarget, MediaType: "image/svg+xml", Entrypoint: "flow.svg",
		SlideMeta: &saga.SlideManifest{ID: "flow", DeckID: "overview"},
		Reviews:   []saga.Review{{State: "approved", CreatedAt: time.Now()}},
		Landmarks: []saga.Landmark{
			{ID: "client", Label: "Client", Target: saga.ItemTarget("visual", "flow", "client")},
			{ID: "server", Label: "Server", Target: saga.ItemTarget("visual", "flow", "server")},
		},
	}
	second := &saga.Fragment{
		ID: "failure", Title: "Failure path", Target: secondTarget, MediaType: "text/html", Entrypoint: "failure.html",
		SlideMeta: &saga.SlideManifest{ID: "failure", DeckID: "overview"},
		Landmarks: []saga.Landmark{{ID: "timeout", Label: "Timeout", Target: saga.ItemTarget("visual", "failure", "timeout")}},
	}
	document := &saga.Saga{
		Manifest: saga.Manifest{Version: saga.SlideSagaVersion, ID: "visual", Title: "Visual review"},
		Section: &saga.Section{Kind: "saga", ID: "visual-root", Target: saga.SagaTarget("visual"), Children: []*saga.Section{{
			Kind: "deck", ID: "overview", Title: "System tour", Target: deckTarget, Fragments: []*saga.Fragment{first, second},
		}}},
	}
	locations := indexNarrativeFragments(document)
	atoms := []*diffAtomView{
		{Atom: gitdiff.Atom{Key: "client", URI: "diff://client"}},
		{Atom: gitdiff.Atom{Key: "server", URI: "diff://server"}},
		{Atom: gitdiff.Atom{Key: "timeout", URI: "diff://timeout"}},
	}
	result := makeRelatedSagaViews(locations, atoms, map[string][]coverage.Assignment{
		"client":  {{Target: first.Landmarks[0].Target}},
		"server":  {{Target: first.Landmarks[1].Target}},
		"timeout": {{Target: second.Landmarks[0].Target}},
	})
	if len(result) != 1 || result[0].Title != "System tour" || !result[0].SlideNative || len(result[0].Fragments) != 2 {
		t.Fatalf("slide owners were not grouped by deck: %#v", result)
	}
	flow := result[0].Fragments[0].Slide
	if flow == nil || flow.Title != "Request flow" || flow.ItemCount != 2 || flow.URL != "/f/flow/flow.svg" || flow.ReviewState != "approved" || flow.Href != sagaHref(firstTarget) {
		t.Fatalf("item owners did not roll up to the visual slide reference: %#v", flow)
	}
	if failure := result[0].Fragments[1].Slide; failure == nil || failure.ItemCount != 1 || failure.MediaType != "text/html" {
		t.Fatalf("second slide reference = %#v", failure)
	}
	var rendered strings.Builder
	if err := serverTemplate(t).ExecuteTemplate(&rendered, "file-owners", fileOwnersView{Groups: result}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(rendered.String(), `class="related-slide"`); got != 2 || strings.Contains(rendered.String(), ">Client<") || strings.Count(rendered.String(), ">System tour<") != 1 {
		t.Fatalf("visual slide references were not deduplicated and grouped: %s", rendered.String())
	}
	owner := manifestOwner(first.Landmarks[0].Target, indexManifestTargets(document))
	if owner.Slide == nil || owner.Slide.Target != first.Target || owner.Title != "Client" || owner.Chapter != "System tour" {
		t.Fatalf("coverage owner lost its exact Item or parent slide: %#v", owner)
	}
	rendered.Reset()
	if err := serverTemplate(t).ExecuteTemplate(&rendered, "manifest-owner-reference", owner); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), `class="manifest-slide-owner"`) || !strings.Contains(rendered.String(), "Request flow") || !strings.Contains(rendered.String(), "Client") {
		t.Fatalf("coverage did not render its slide and exact Item: %s", rendered.String())
	}
}

func TestFileViewsAttachRendererContextWithoutChangingAtoms(t *testing.T) {
	old := gitdiff.Atom{Key: "line:app.go:old:2", Kind: "line", Path: "app.go", Side: "old", Line: 2, Content: "old", URI: "old-uri"}
	added := gitdiff.Atom{Key: "line:app.go:new:2", Kind: "line", Path: "app.go", Side: "new", Line: 2, Content: "new", URI: "new-uri"}
	changes := gitdiff.ChangeSet{
		Repository: "https://example.test/a.git", BaseOID: "aaa", HeadOID: "bbb", Atoms: []gitdiff.Atom{old, added},
		DisplayLines: []gitdiff.DisplayLine{
			{Kind: "context", Path: "app.go", OldLine: 1, NewLine: 1, Content: "package app"},
			{Kind: "old", Path: "app.go", OldLine: 2, Content: "old", AtomKey: old.Key},
			{Kind: "new", Path: "app.go", NewLine: 2, Content: "new", AtomKey: added.Key},
		},
	}
	files := makeFileViews(changes, "urn:change-saga:test:saga", nil, nil)
	if len(files) != 1 || len(files[0].Atoms) != 2 || len(files[0].Lines) != 3 {
		if len(files) == 0 {
			t.Fatal("focused file was not built")
		}
		t.Fatalf("unexpected focused file: atoms=%d lines=%d lines=%#v", len(files[0].Atoms), len(files[0].Lines), files[0].Lines)
	}
	if files[0].Lines[0].Atom != nil || files[0].Lines[1].Atom == nil || files[0].Lines[1].Atom.URI != "old-uri" || files[0].Lines[2].Atom.URI != "new-uri" {
		t.Fatalf("display lines did not preserve atom actions: %#v", files[0].Lines)
	}
}

func TestFragmentExcerptIsConciseAndCannotFollowEscapingSymlink(t *testing.T) {
	directory := t.TempDir()
	writeServerFile(t, filepath.Join(directory, "content.md"), "# Heading\n"+strings.Repeat("word ", 100))
	fragment := &saga.Fragment{ID: "story", Title: "Story", Directory: directory, MediaType: "text/markdown", Entrypoint: "content.md"}
	excerpt := fragmentExcerpt(fragment)
	if utf8.RuneCountInString(excerpt) > 181 || !strings.HasSuffix(excerpt, "…") {
		t.Fatalf("excerpt is not bounded: %d runes, %q", utf8.RuneCountInString(excerpt), excerpt)
	}

	outside := filepath.Join(filepath.Dir(directory), "outside.txt")
	writeServerFile(t, outside, "private outside content")
	if err := os.Symlink(outside, filepath.Join(directory, "escape.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fragment.Entrypoint = "escape.md"
	if excerpt := fragmentExcerpt(fragment); excerpt != "Story" {
		t.Fatalf("escaping symlink content was exposed: %q", excerpt)
	}
}

// The excerpt sits beside the fragment title in the explanations panel, so it
// should read as prose: no heading label running into the first sentence and no
// leftover Markdown delimiters.
func TestFragmentExcerptReadsAsProseNotMarkdownSource(t *testing.T) {
	directory := t.TempDir()
	writeServerFile(t, filepath.Join(directory, "content.md"), "## CLI surface {#cli-surface}\n\nThe CLI runs `validate` and **checks** the tree.\n")
	fragment := &saga.Fragment{ID: "cli", Title: "CLI and AI workflow", Directory: directory, MediaType: "text/markdown", Entrypoint: "content.md"}
	excerpt := fragmentExcerpt(fragment)
	if want := "The CLI runs validate and checks the tree."; excerpt != want {
		t.Fatalf("excerpt = %q, want %q", excerpt, want)
	}

	// Headings are only dropped when prose survives them. A fragment made of
	// nothing but headings keeps them rather than showing an empty excerpt.
	writeServerFile(t, filepath.Join(directory, "content.md"), "# Only a heading\n")
	if excerpt := fragmentExcerpt(fragment); excerpt != "Only a heading" {
		t.Fatalf("heading-only fragment lost its last readable text: %q", excerpt)
	}
}

func codeViewFixture(t *testing.T) (*saga.Saga, gitdiff.ChangeSet, coverage.Report, string, string) {
	t.Helper()
	root := t.TempDir()
	overviewDir := filepath.Join(root, "overview.fragment")
	flowDir := filepath.Join(root, "backend.chapter", "flow.fragment")
	writeServerFile(t, filepath.Join(overviewDir, "content.md"), "# Overview\nA concise overview.\n")
	writeServerFile(t, filepath.Join(flowDir, "index.html"), `<script>alert("not excerpted")</script><p>Visible explanation of the request flow.</p>`)

	firstURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "src/api/handler.go", Side: "new", Start: 10, End: 10,
	})
	secondURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "src/api/handler.go", Side: "new", Start: 11, End: 11,
	})
	rangeURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "src/api/handler.go", Side: "new", Start: 10, End: 11,
	})
	staleURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "src/api/handler.go", Side: "new", Start: 99, End: 99,
	})
	first := gitdiff.Atom{Kind: "line", Path: "src/api/handler.go", Side: "new", Line: 10, Content: "first", URI: firstURI}
	first.Key = gitdiff.Key(first)
	second := gitdiff.Atom{Kind: "line", Path: "src/api/handler.go", Side: "new", Line: 11, Content: "second", URI: secondURI}
	second.Key = gitdiff.Key(second)
	unownedURI := mustDiffURI(t, diffuri.Reference{
		Repository: "https://example.test/repo.git", Base: "aaa", Head: "product-bbb",
		Kind: "line", Path: "docs/unowned.md", Side: "new", Start: 1, End: 1,
	})
	unowned := gitdiff.Atom{Kind: "line", Path: "docs/unowned.md", Side: "new", Line: 1, Content: "docs", URI: unownedURI}
	unowned.Key = gitdiff.Key(unowned)

	overview := &saga.Fragment{ID: "overview", Title: "Overview", Target: saga.FragmentTarget("test", "overview"), Directory: overviewDir, MediaType: "text/markdown", Entrypoint: "content.md"}
	flowDiffPath := filepath.Join(flowDir, "___diffs", "flow.json")
	flow := &saga.Fragment{
		ID: "flow", Title: "Request flow", Target: saga.FragmentTarget("test", "flow"), Directory: flowDir, MediaType: "text/html", Entrypoint: "index.html",
		Diffs: []saga.DiffFile{{Path: flowDiffPath, Diffs: []saga.DiffReference{{URI: rangeURI, Note: "request handling"}, {URI: staleURI}}}},
	}
	chapter := &saga.Section{Kind: "chapter", ID: "backend", Title: "Backend", Target: saga.ChapterTarget("test", "backend"), Fragments: []*saga.Fragment{flow}}
	section := &saga.Section{Kind: "saga", ID: "test", Title: "Test", Target: saga.SagaTarget("test"), Fragments: []*saga.Fragment{overview}, Children: []*saga.Section{chapter}}
	document := &saga.Saga{Root: root, Manifest: saga.Manifest{ID: "test"}, Section: section}
	changes := gitdiff.ChangeSet{Repository: "https://example.test/repo.git", BaseOID: "aaa", HeadOID: "product-bbb", Atoms: []gitdiff.Atom{first, second, unowned}}
	report := coverage.Report{
		Ownership: map[string][]coverage.Assignment{
			first.Key:  {{Target: overview.Target}, {Target: flow.Target, DiffFile: flowDiffPath, Diff: 1}},
			second.Key: {{Target: flow.Target, DiffFile: flowDiffPath, Diff: 1}},
		},
		Orphans: []coverage.Orphan{{
			Assignment: coverage.Assignment{Target: flow.Target, DiffFile: flowDiffPath, Diff: 2},
			Reference:  saga.DiffReference{URI: staleURI}, Reason: "diff URI does not match the current source comparison",
		}},
	}
	document.DiffReviews = []saga.DiffReview{{URI: mustDiffURI(t, diffuri.Reference{
		Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "file", Path: "src/api/handler.go",
	}), Author: "Ada", State: "reviewed", CreatedAt: time.Now()}}
	return document, changes, report, secondURI, staleURI
}

func largeCodeViewFixture(tb testing.TB, fileCount, linesPerFile int) (*saga.Saga, gitdiff.ChangeSet, coverage.Report, codeSelection) {
	tb.Helper()
	const (
		repository = "https://example.test/org/large.git"
		base       = "base-oid"
		head       = "product-head-oid"
	)
	document := &saga.Saga{
		Manifest: saga.Manifest{ID: "large"},
		Section:  &saga.Section{Kind: "saga", ID: "large", Title: "Large", Target: saga.SagaTarget("large")},
	}
	chapter := &saga.Section{Kind: "chapter", ID: "implementation", Title: "Implementation", Target: saga.ChapterTarget("large", "implementation")}
	document.Section.Children = []*saga.Section{chapter}
	changes := gitdiff.ChangeSet{Repository: repository, BaseOID: base, HeadOID: head}
	report := coverage.Report{Ownership: make(map[string][]coverage.Assignment, fileCount*linesPerFile)}

	for fileIndex := range fileCount {
		filePath := fmt.Sprintf("internal/component%03d/handler.go", fileIndex)
		fragmentID := fmt.Sprintf("component-%03d", fileIndex)
		fragment := &saga.Fragment{
			ID: fragmentID, Title: fmt.Sprintf("Component %03d", fileIndex),
			Target: saga.FragmentTarget("large", fragmentID), MediaType: "application/octet-stream",
		}
		diffPath := fmt.Sprintf("implementation.chapter/%s.fragment/___diffs/implementation.json", fragmentID)
		rangeURI := mustDiffURIForTB(tb, diffuri.Reference{
			Repository: repository, Base: base, Head: head, Kind: "line", Path: filePath,
			Side: "new", Start: 1, End: linesPerFile,
		})
		fragment.Diffs = []saga.DiffFile{{Path: diffPath, Diffs: []saga.DiffReference{{URI: rangeURI, Note: "Implements the component."}}}}
		chapter.Fragments = append(chapter.Fragments, fragment)
		for line := 1; line <= linesPerFile; line++ {
			atom := gitdiff.Atom{Kind: "line", Path: filePath, Side: "new", Line: line, Content: "changed line"}
			atom.Key = gitdiff.Key(atom)
			atom.URI = mustDiffURIForTB(tb, diffuri.Reference{
				Repository: repository, Base: base, Head: head, Kind: "line", Path: filePath,
				Side: "new", Start: line, End: line,
			})
			changes.Atoms = append(changes.Atoms, atom)
			report.Ownership[atom.Key] = []coverage.Assignment{{Target: fragment.Target, DiffFile: diffPath, Diff: 1}}
		}
	}
	selectedPath := fmt.Sprintf("internal/component%03d/handler.go", fileCount-1)
	return document, changes, report, codeSelection{filePath: selectedPath}
}

func mustDiffURIForTB(tb testing.TB, reference diffuri.Reference) string {
	tb.Helper()
	uri, err := diffuri.Build(reference)
	if err != nil {
		tb.Fatal(err)
	}
	return uri
}

func mustDiffURI(t *testing.T, reference diffuri.Reference) string {
	t.Helper()
	uri, err := diffuri.Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	return uri
}
