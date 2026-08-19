package coverage

import (
	"testing"

	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/saga"
)

const (
	testRepository = "https://example.test/acme/app.git"
	testBase       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testHead       = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestEvaluateFindsUncoveredOverlapAndOrphan(t *testing.T) {
	atoms := []gitdiff.Atom{
		lineAtom(t, "app.go", "new", 2),
		lineAtom(t, "app.go", "new", 3),
		eventAtom(t, "rename", "new.go", "old.go", "new.go"),
	}
	rootLine := buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "app.go", Side: "new", Start: 2, End: 2})
	missing := buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "missing.go", Side: "old", Start: 1, End: 1})
	rename := buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "event", Event: "rename", OldPath: "old.go", NewPath: "new.go"})
	document := &saga.Saga{Section: &saga.Section{
		Target: "urn:review-saga:test:saga",
		Diffs:  []saga.DiffFile{{Path: "___diffs/root.json", Diffs: []saga.DiffReference{{URI: rootLine}}}},
		Fragments: []*saga.Fragment{{
			Target: "urn:review-saga:test:fragment:flow",
			Diffs: []saga.DiffFile{{Path: "flow.fragment/___diffs/flow.json", Diffs: []saga.DiffReference{
				{URI: rootLine}, {URI: missing}, {URI: rename},
			}}},
		}},
	}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: atoms, SagaChanges: []gitdiff.Atom{{Key: "saga"}}})
	if report.Complete {
		t.Fatal("report should be incomplete")
	}
	if report.Summary.Total != 3 || report.Summary.Covered != 2 || report.Summary.Uncovered != 1 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if report.Summary.Overlapping != 1 || report.Summary.Orphaned != 1 || report.Summary.SagaChanges != 1 {
		t.Fatalf("unexpected diagnostics: %#v", report.Summary)
	}
	if got := report.Uncovered[0].Line; got != 3 {
		t.Fatalf("uncovered line = %d, want 3", got)
	}
}

func TestEvaluateComplete(t *testing.T) {
	atom := lineAtom(t, "app.go", "old", 8)
	document := &saga.Saga{Section: &saga.Section{
		Target: "urn:review-saga:test:saga",
		Diffs:  []saga.DiffFile{{Diffs: []saga.DiffReference{{URI: atom.URI}}}},
	}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atom}})
	if !report.Complete {
		t.Fatalf("report should be complete: %#v", report)
	}
}

func lineAtom(t *testing.T, path, side string, line int) gitdiff.Atom {
	t.Helper()
	atom := gitdiff.Atom{Kind: "line", Path: path, Side: side, Line: line}
	atom.Key = gitdiff.Key(atom)
	atom.URI = buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: path, Side: side, Start: line, End: line})
	return atom
}

func eventAtom(t *testing.T, event, path, oldPath, newPath string) gitdiff.Atom {
	t.Helper()
	atom := gitdiff.Atom{Kind: "event", Event: event, Path: path, OldPath: oldPath, NewPath: newPath}
	atom.Key = gitdiff.Key(atom)
	atom.URI = buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "event", Event: event, Path: path, OldPath: oldPath, NewPath: newPath})
	return atom
}

func buildURI(t *testing.T, reference diffuri.Reference) string {
	t.Helper()
	value, err := diffuri.Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
