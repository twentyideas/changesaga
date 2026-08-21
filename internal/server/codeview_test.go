package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/change-saga/change-saga/internal/coverage"
	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

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
	if len(result) != 1 || len(result[0].Fragments) != 1 || result[0].Fragments[0].Title != "Submit action" || result[0].Fragments[0].Href != location.fragmentHref {
		t.Fatalf("landmark reverse link = %#v", result)
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

func mustDiffURI(t *testing.T, reference diffuri.Reference) string {
	t.Helper()
	uri, err := diffuri.Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	return uri
}
