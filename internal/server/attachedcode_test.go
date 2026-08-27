package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestAttachedCodeGroupsExactChangesByFileWithAuthoredReason(t *testing.T) {
	document, changes, _, _, _ := codeViewFixture(t)
	flow := document.Section.Children[0].Fragments[0]
	view := makeAttachedCodeView(flow.Title, flow.Target, changes.Atoms[:2], flow.Diffs)

	if view == nil || view.Title != "Request flow" || view.ChangeCount != 2 || len(view.Files) != 1 {
		t.Fatalf("unexpected attached code view: %#v", view)
	}
	file := view.Files[0]
	if file.Path != "src/api/handler.go" || file.Summary != "request handling" || file.MissingSummary || file.Added != 2 || file.Changes != 2 {
		t.Fatalf("file grouping lost its narrative evidence: %#v", file)
	}
	if file.Target != flow.Target || !strings.Contains(file.Href, "view=code") {
		t.Fatalf("file did not retain exact review target or full-diff link: %#v", file)
	}
}

func TestAttachedCodeRequestsEveryHunkAndMarksContextExpandable(t *testing.T) {
	tmpl := serverTemplate(t)
	view := &attachedCodeView{Title: "Request flow", ChangeCount: 1, Files: []*attachedCodeFileView{{
		Path: "src/app.go", Target: "urn:change-saga:test:fragment:flow", Added: 1, Changes: 1,
	}}}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "attached-code", view); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, `&amp;full=true`) {
		t.Fatal("linked-code drawer did not request the complete changed-file patch")
	}

	output.Reset()
	if err := tmpl.ExecuteTemplate(&output, "attached-file-context", &FileDiffView{Lines: []*DiffLineView{{Kind: "context", OldLine: 1, NewLine: 1, Content: "package app"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `data-attached-full-diff data-diff-body`) {
		t.Fatal("linked-code rows were not exposed to unchanged-context collapsing")
	}
	if !strings.Contains(appJavaScript, "prepareContext(linkedRows)") {
		t.Fatal("linked-code hydration did not prepare expandable unchanged context")
	}
}

func TestAttachedCodeKeepsFilesSeparateAndFlagsMissingSummaries(t *testing.T) {
	atoms := []gitdiff.Atom{
		{Key: "a", Kind: "line", Path: "b.go", Side: "new", Line: 2, Content: "b", URI: "uri-b"},
		{Key: "b", Kind: "line", Path: "a.go", Side: "old", Line: 1, Content: "a", URI: "uri-a"},
	}
	view := makeAttachedCodeView("Target", "urn:target", atoms, []saga.DiffFile{})
	if len(view.Files) != 2 || view.Files[0].Path != "a.go" || view.Files[1].Path != "b.go" {
		t.Fatalf("files were not kept separate and sorted: %#v", view.Files)
	}
	for _, file := range view.Files {
		if !file.MissingSummary || file.Summary == "" {
			t.Fatalf("missing authoring guidance was hidden: %#v", file)
		}
	}
}
