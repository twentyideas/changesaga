package server

import (
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestAttachedCodeGroupsExactChangesByFileWithAuthoredReason(t *testing.T) {
	document, changes, _, _, _ := codeViewFixture(t)
	flow := document.Section.Children[0].Fragments[0]
	linked := changes.Atoms[:2]
	full := append(append([]gitdiff.Atom{}, linked...), gitdiff.Atom{Key: "extra", Kind: "line", Path: "src/api/handler.go", Side: "old", Line: 9, Content: "old", URI: "uri-extra"})
	view := makeAttachedCodeView(flow.Title, flow.Target, linked, full, flow.Diffs)

	if view == nil || view.Title != "Request flow" || view.ChangeCount != 3 || view.LineCount != 3 || view.Added != 2 || view.Deleted != 1 || view.LinkedLineCount != 2 || len(view.Files) != 1 {
		t.Fatalf("unexpected attached code view: %#v", view)
	}
	file := view.Files[0]
	if file.Path != "src/api/handler.go" || file.Summary != "request handling" || file.MissingSummary || file.Added != 2 || file.Deleted != 1 || file.Changes != 3 || file.LinkedLines != 2 {
		t.Fatalf("file grouping lost its narrative evidence: %#v", file)
	}
	if file.Target != flow.Target || !strings.Contains(file.Href, "view=code") {
		t.Fatalf("file did not retain exact review target or full-diff link: %#v", file)
	}
}

func TestAttachedCodeKeepsFilesSeparateAndFlagsMissingSummaries(t *testing.T) {
	atoms := []gitdiff.Atom{
		{Key: "a", Kind: "line", Path: "b.go", Side: "new", Line: 2, Content: "b", URI: "uri-b"},
		{Key: "b", Kind: "line", Path: "a.go", Side: "old", Line: 1, Content: "a", URI: "uri-a"},
	}
	view := makeAttachedCodeView("Target", "urn:target", atoms, atoms, []saga.DiffFile{})
	if len(view.Files) != 2 || view.Files[0].Path != "a.go" || view.Files[1].Path != "b.go" {
		t.Fatalf("files were not kept separate and sorted: %#v", view.Files)
	}
	for _, file := range view.Files {
		if !file.MissingSummary || file.Summary == "" {
			t.Fatalf("missing authoring guidance was hidden: %#v", file)
		}
	}
}
