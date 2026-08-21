package coverage

import (
	"fmt"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func BenchmarkEvaluateLargeMappedDiff(b *testing.B) {
	const (
		atomCount    = 10_000
		mappingCount = 2_000
	)
	atoms := make([]gitdiff.Atom, atomCount)
	for i := range atoms {
		line := i + 1
		reference := diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "large.go", Side: "new", Start: line, End: line}
		uri, err := diffuri.Build(reference)
		if err != nil {
			b.Fatal(err)
		}
		atoms[i] = gitdiff.Atom{Key: fmt.Sprintf("line:large.go:new:%d", line), URI: uri, Kind: "line", Path: "large.go", Side: "new", Line: line}
	}
	references := make([]saga.DiffReference, mappingCount)
	for i := range references {
		line := i*5 + 1
		uri, err := diffuri.Build(diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "large.go", Side: "new", Start: line, End: line})
		if err != nil {
			b.Fatal(err)
		}
		references[i] = saga.DiffReference{URI: uri}
	}
	document := &saga.Saga{Section: &saga.Section{
		Target: "urn:change-saga:benchmark:saga",
		Diffs:  []saga.DiffFile{{Path: "___diffs/large.json", Diffs: references}},
	}}
	changes := gitdiff.ChangeSet{Repository: testRepository, BaseOID: testBase, HeadOID: testHead, Atoms: atoms}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report := Evaluate(document, saga.Validation{Valid: true}, changes)
		if report.Summary.Covered != mappingCount {
			b.Fatalf("covered = %d, want %d", report.Summary.Covered, mappingCount)
		}
	}
}

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
		Target: "urn:change-saga:test:saga",
		Diffs:  []saga.DiffFile{{Path: "___diffs/root.json", Diffs: []saga.DiffReference{{URI: rootLine}}}},
		Fragments: []*saga.Fragment{{
			Target: "urn:change-saga:test:fragment:flow",
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
		Target: "urn:change-saga:test:saga",
		Diffs:  []saga.DiffFile{{Diffs: []saga.DiffReference{{URI: atom.URI}}}},
	}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atom}})
	if !report.Complete {
		t.Fatalf("report should be complete: %#v", report)
	}
}

func TestLifecycleAtomCannotYieldFalseCompleteCoverage(t *testing.T) {
	atom := eventAtom(t, "add", "empty.txt", "", "")
	document := &saga.Saga{Section: &saga.Section{Target: "urn:change-saga:test:saga"}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atom}})
	if report.Complete || report.Summary.Total != 1 || report.Summary.Uncovered != 1 || len(report.Uncovered) != 1 {
		t.Fatalf("uncovered empty-file lifecycle was reported complete: %#v", report)
	}
	document.Section.Diffs = []saga.DiffFile{{Diffs: []saga.DiffReference{{URI: atom.URI}}}}
	report = Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atom}})
	if !report.Complete || report.Summary.Covered != 1 {
		t.Fatalf("covered lifecycle event was not complete: %#v", report)
	}
}

func TestEvaluateLandmarkDiffs(t *testing.T) {
	atom := lineAtom(t, "app.go", "new", 12)
	landmarkTarget := saga.LandmarkTarget("test", "flow", "submit-action")
	document := &saga.Saga{Section: &saga.Section{
		Target: saga.SagaTarget("test"),
		Fragments: []*saga.Fragment{{
			Target:    saga.FragmentTarget("test", "flow"),
			Landmarks: []saga.Landmark{{Target: landmarkTarget, Diffs: []saga.DiffFile{{Path: "submit-action.landmark/___diffs/handler.json", Diffs: []saga.DiffReference{{URI: atom.URI}}}}}},
		}},
	}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atom}})
	if !report.Complete || len(report.Ownership[atom.Key]) != 1 || report.Ownership[atom.Key][0].Target != landmarkTarget {
		t.Fatalf("landmark did not own its code: %#v", report)
	}
}

func TestEvaluateLineIndexPreservesContainedRangeMatching(t *testing.T) {
	atom := func(key string, start, end int) gitdiff.Atom {
		return gitdiff.Atom{
			Key: key, Kind: "line", Path: "app.go", Side: "new", Line: start,
			URI: buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "app.go", Side: "new", Start: start, End: end}),
		}
	}
	atoms := []gitdiff.Atom{
		atom("after", 9, 9),
		atom("inside-late", 8, 8),
		atom("overlaps-end", 4, 9),
		atom("inside", 5, 6),
		atom("overlaps-start", 2, 4),
	}
	selector := buildURI(t, diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "line", Path: "app.go", Side: "new", Start: 4, End: 8})
	document := &saga.Saga{Section: &saga.Section{
		Target: "urn:change-saga:test:saga",
		Diffs:  []saga.DiffFile{{Diffs: []saga.DiffReference{{URI: selector}}}},
	}}
	report := Evaluate(document, saga.Validation{Valid: true}, gitdiff.ChangeSet{Atoms: atoms})
	if report.Summary.Covered != 2 || len(report.Ownership["inside"]) != 1 || len(report.Ownership["inside-late"]) != 1 {
		t.Fatalf("contained line atoms were not matched: %#v", report)
	}
	for _, key := range []string{"after", "overlaps-end", "overlaps-start"} {
		if len(report.Ownership[key]) != 0 {
			t.Errorf("non-contained atom %q was matched: %#v", key, report.Ownership[key])
		}
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
	reference := diffuri.Reference{Repository: testRepository, Base: testBase, Head: testHead, Kind: "event", Event: event, Path: path, OldPath: oldPath, NewPath: newPath}
	if event == "rename" {
		reference.Path = ""
	}
	atom.URI = buildURI(t, reference)
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
