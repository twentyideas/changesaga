package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/review-saga/review-saga/internal/coverage"
)

func TestCoverageManifestProvesBothMappingDirections(t *testing.T) {
	document, changes, report, _, staleURI := codeViewFixture(t)
	report.Summary = coverage.Summary{Total: 3, Covered: 2, Uncovered: 1, Overlapping: 1, Orphaned: 1}
	report.Complete = false

	view := makeCoverageManifestView(document, changes, report)
	if view.Total != 3 || view.Covered != 2 || view.Uncovered != 1 || view.MappingCount != 3 || len(view.Files) != 2 {
		t.Fatalf("unexpected manifest summary: %#v", view)
	}
	if view.Files[0].Path != "docs/unowned.md" || view.Files[0].Uncovered != 1 || len(view.Files[0].Chunks) != 1 || view.Files[0].Chunks[0].Covered {
		t.Fatalf("unmapped file was not explicit: %#v", view.Files[0])
	}
	handler := view.Files[1]
	if handler.Path != "src/api/handler.go" || len(handler.Chunks) != 2 || len(handler.Chunks[0].Owners) != 2 || len(handler.Chunks[1].Owners) != 1 {
		t.Fatalf("code-to-saga ownership was not grouped exactly: %#v", handler)
	}
	if handler.Diff == nil || handler.Diff.Path != handler.Path || len(handler.Diff.Lines) != 2 || handler.Diff.Lines[0].Content != "first" || handler.Diff.Lines[1].Content != "second" {
		t.Fatalf("coverage file did not retain its inspectable diff: %#v", handler.Diff)
	}
	if handler.Chunks[0].Label != "+10" || handler.Chunks[1].Label != "+11" {
		t.Fatalf("ranges with different owners must stay separate: %#v", handler.Chunks)
	}

	if len(view.Targets) != 2 || view.Targets[0].Title != "Overview" || view.Targets[1].Title != "Request flow" {
		t.Fatalf("saga-to-code targets were not ordered in narrative order: %#v", view.Targets)
	}
	flow := view.Targets[1]
	if flow.AtomCount != 2 || len(flow.Chunks) != 1 || flow.Chunks[0].Label != "+10–11" || flow.Chunks[0].Path != "src/api/handler.go" {
		t.Fatalf("reverse mapping did not preserve its exact range: %#v", flow)
	}
	if len(flow.Files) != 1 || flow.Files[0].Path != "src/api/handler.go" || flow.Files[0].AtomCount != 2 || flow.Files[0].Diff == nil || len(flow.Files[0].Diff.Lines) != 2 {
		t.Fatalf("reverse mapping did not retain an expandable file diff: %#v", flow.Files)
	}
	parsed, err := url.Parse(flow.Chunks[0].Href)
	if err != nil || parsed.Query().Get("view") != "code" || !strings.Contains(parsed.Query().Get("diff"), "end=11") {
		t.Fatalf("reverse mapping did not deep-link to the full code range: %q (%v)", flow.Chunks[0].Href, err)
	}
	if len(view.Orphans) != 1 || view.Orphans[0].URI != staleURI || view.Orphans[0].Owner.Title != "Request flow" {
		t.Fatalf("stale narrative evidence was not auditable: %#v", view.Orphans)
	}
}

func TestManifestChunksNeverMergeAcrossFiles(t *testing.T) {
	document, changes, report, _, _ := codeViewFixture(t)
	// Give the unowned line the same owner and adjacent-looking line number as
	// the handler range. Reverse grouping must still retain the file boundary.
	unowned := changes.Atoms[2]
	unowned.Line = 12
	changes.Atoms[2] = unowned
	report.Ownership[unowned.Key] = []coverage.Assignment{{Target: document.Section.Children[0].Fragments[0].Target}}

	view := makeCoverageManifestView(document, changes, report)
	flow := view.Targets[1]
	if len(flow.Chunks) != 2 || flow.Chunks[0].Path == flow.Chunks[1].Path {
		t.Fatalf("reverse ranges crossed a file boundary: %#v", flow.Chunks)
	}
}
