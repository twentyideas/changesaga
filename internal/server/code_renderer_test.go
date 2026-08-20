package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/saga"
)

func TestFocusedCodeRendererIncludesAccessibleLocalDiffControls(t *testing.T) {
	tmpl := serverTemplate(t)
	old := &diffAtomView{Atom: gitdiff.Atom{Kind: "line", Key: "old", URI: "saga-diff://v1/line?base=a&end=7&head=b&path=src/app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=old&start=7", Path: "src/app.go", Side: "old", Line: 7, Content: "return old"}, Target: "urn:review-saga:test:saga"}
	added := &diffAtomView{Atom: gitdiff.Atom{Kind: "line", Key: "new", URI: "saga-diff://v1/line?base=a&end=7&head=b&path=src/app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=new&start=7", Path: "src/app.go", Side: "new", Line: 7, Content: "return fresh"}, Target: "urn:review-saga:test:saga", Selected: true}
	file := &FileDiffView{ID: "diff-app", Name: "app.go", Path: "src/app.go", URI: "file-uri", Added: 1, Deleted: 1, Selected: true, Atoms: []*diffAtomView{old, added}, Lines: []*DiffLineView{
		{Kind: "context", Path: "src/app.go", OldLine: 6, NewLine: 6, Content: "func value() string {"},
		{Kind: "old", Path: "src/app.go", OldLine: 7, Content: old.Content, Atom: old},
		{Kind: "new", Path: "src/app.go", NewLine: 7, Content: added.Content, Atom: added},
	}}
	code := &CodeReviewView{
		Files: []*FileDiffView{file}, SelectedFile: file,
		Tree:        ChangedFileTreeView{Nodes: []*ChangedFileTreeNode{{Name: "src", Kind: "folder", Expanded: true, Children: []*ChangedFileTreeNode{{Name: "app.go", Kind: "file", File: file}}}}, FileCount: 1, Added: 1, Deleted: 1},
		RelatedSaga: []*RelatedSagaChapterView{{Title: "Backend", Href: "#target-backend", Fragments: []*RelatedSagaFragmentView{{Title: "Request flow", Excerpt: "Explains the new branch.", Href: "#target-flow"}}}},
	}
	data := pageData{Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test"}}, Root: &sectionView{Section: &saga.Section{}}, Code: code, Files: code.Files}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, expected := range []string{
		`data-file-filter`, `data-hide-reviewed`, `role="tree"`, `data-tree-folder`, `class=" selected"`,
		`data-toggle-tree`, `data-toggle-related`, `data-layout="inline"`, `data-layout="split"`,
		`data-diff-surface`, `data-context-row`, `aria-label="Unchanged line 6"`,
		`aria-label="Removed old line 7"`, `aria-label="Added new line 7"`,
		`data-selection-action="comment"`, `data-selection-action="suggestion"`,
		`class="diff-row new selected"`, `href="#target-flow"`, `Explains the new branch.`,
		`<script src="/app.js" defer></script>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("renderer omitted %q", expected)
		}
	}
	if strings.Contains(body, "cdn.") || strings.Contains(body, "unpkg.") || strings.Contains(body, "jsdelivr.") {
		t.Fatal("renderer added a runtime network dependency")
	}
	if strings.Contains(body, `class="file-review" method="post" action="/api/diff-review"><input required`) {
		t.Fatal("mark-reviewed control regressed to a visible header identity form")
	}
}
