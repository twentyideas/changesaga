package server

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/gitdiff"
)

func TestDiffCountsHaveOneSharedRenderer(t *testing.T) {
	addedRenderers := regexp.MustCompile(`[+]\{\{[^}]*Added\}\}`).FindAllString(pageTemplate, -1)
	deletedRenderers := regexp.MustCompile(`−\{\{[^}]*Deleted\}\}`).FindAllString(pageTemplate, -1)
	if len(addedRenderers) != 1 || len(deletedRenderers) != 1 {
		t.Fatalf("added/deleted counts must be rendered only by diff-counts: added=%v deleted=%v", addedRenderers, deletedRenderers)
	}
	if uses := strings.Count(pageTemplate, `template "diff-counts"`); uses < 9 {
		t.Fatalf("diff-counts is not shared across the review surfaces: uses=%d", uses)
	}
}

func TestFocusedCodeRendererIncludesAccessibleLocalDiffControls(t *testing.T) {
	tmpl := serverTemplate(t)
	old := &diffAtomView{Atom: gitdiff.Atom{Kind: "line", Key: "old", URI: "saga-diff://v1/line?base=a&end=7&head=b&path=src/app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=old&start=7", Path: "src/app.go", Side: "old", Line: 7, Content: "return old"}, Target: "urn:change-saga:test:saga"}
	added := &diffAtomView{Atom: gitdiff.Atom{Kind: "line", Key: "new", URI: "saga-diff://v1/line?base=a&end=7&head=b&path=src/app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=new&start=7", Path: "src/app.go", Side: "new", Line: 7, Content: "return fresh"}, Target: "urn:change-saga:test:saga", Selected: true}
	file := &FileDiffView{ID: "diff-app", Name: "app.go", Path: "src/app.go", URI: "file-uri", Added: 1, Deleted: 1, Selected: true, Atoms: []*diffAtomView{old, added}, Lines: []*DiffLineView{
		{Kind: "context", Path: "src/app.go", OldLine: 6, NewLine: 6, Content: "func value() string {"},
		{Kind: "old", Path: "src/app.go", OldLine: 7, Content: old.Content, Atom: old},
		{Kind: "new", Path: "src/app.go", NewLine: 7, Content: added.Content, Atom: added},
	}}
	code := codePageView{
		Tree:     ChangedFileTreeView{Nodes: []*ChangedFileTreeNode{{Name: "src", Kind: "folder", Expanded: true, Children: []*ChangedFileTreeNode{{Name: "app.go", Kind: "file", File: file}}}}, FileCount: 1, Added: 1, Deleted: 1},
		Selected: file, Owners: []*ManifestOwnerView{{Title: "Request flow", Chapter: "Backend", Href: "#target-flow"}},
		TotalFiles: 1, Returned: 1,
	}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "code-page", code); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.ExecuteTemplate(&output, "file-diff-page", fileDiffPageView{File: file, Returned: len(file.Lines)}); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, expected := range []string{
		`data-file-filter`, `data-hide-reviewed`, `role="tree"`, `data-tree-folder`, `class=" selected"`,
		`data-toggle-tree`, `data-toggle-related`, `data-layout="inline"`, `data-layout="split"`,
		`data-file-diff-href`, `data-diff-surface`, `data-file-diff-status`, `data-file-diff-rows`,
		`data-context-row`, `aria-label="Unchanged line 6"`,
		`aria-label="Removed old line 7"`, `aria-label="Added new line 7"`,
		`data-selection-action="comment"`, `data-selection-action="suggestion"`, `data-selection-clear`,
		`class="diff-row new selected"`, `href="#target-flow"`, `class="diff-counts"`,
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
