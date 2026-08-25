package reviewapp

import (
	"strconv"
	"testing"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

const scaleTarget = "urn:change-saga:scale:saga"

// linkedSession runs the selector-resolution half of build against a synthetic
// saga, so selector identity can be exercised without a repository or the
// review attribution pass that build performs afterwards.
func linkedSession(document *saga.Saga, changes gitdiff.ChangeSet, report coverage.Report) *session {
	service := &session{
		document: document, changes: changes, report: report,
		targets: map[string]*targetEntry{}, selectors: map[string][]selectorEntry{},
		selectorsByAtom: make(map[string][]DiffOwner, len(report.Ownership)),
		atomByURI:       make(map[string]int, len(changes.Atoms)),
		fragments:       map[string]fragmentValue{}, threads: map[string]ReviewThread{}, threadsByDiff: map[string][]ReviewThread{},
	}
	service.indexSection(document.Section, "")
	service.linkOwnership()
	service.resolveStaleSelectors()
	service.sortAtomOwners()
	return service
}

func sagaWithDiffs(target string, files ...saga.DiffFile) *saga.Saga {
	return &saga.Saga{Section: &saga.Section{Kind: "saga", ID: "scale", Target: target, Diffs: files}}
}

func atomFor(index int) gitdiff.Atom {
	suffix := strconv.Itoa(index)
	return gitdiff.Atom{Key: "key-" + suffix, URI: "uri-" + suffix, Kind: "line", Path: "app.go", Side: "new", Line: index + 1, Content: "line " + suffix}
}

func TestSelectorIdentityResolvesEveryAssignment(t *testing.T) {
	first := saga.DiffFile{Version: 2, Path: "___diffs/root.json", Diffs: []saga.DiffReference{
		{URI: "uri-0", Note: "first"}, {URI: "uri-1", Note: "second"}, {URI: "uri-2", Note: "third"},
	}}
	second := saga.DiffFile{Version: 2, Path: "___diffs/extra.json", Diffs: []saga.DiffReference{{URI: "uri-1", Note: "extra"}}}
	atoms := []gitdiff.Atom{atomFor(0), atomFor(1), atomFor(2)}
	report := coverage.Report{Ownership: map[string][]coverage.Assignment{
		"key-0": {{Target: scaleTarget, DiffFile: "___diffs/root.json", Diff: 1}},
		// The unnormalized path must still resolve to the stored evidence file.
		"key-1": {{Target: scaleTarget, DiffFile: "___diffs/../___diffs/root.json", Diff: 2}, {Target: scaleTarget, DiffFile: "___diffs/extra.json", Diff: 1}},
		"key-2": {{Target: scaleTarget, DiffFile: "___diffs/root.json", Diff: 3}},
	}}
	service := linkedSession(sagaWithDiffs(scaleTarget, first, second), gitdiff.ChangeSet{Atoms: atoms}, report)

	selectors := service.selectors[scaleTarget]
	if len(selectors) != 4 {
		t.Fatalf("selectors = %d, want 4", len(selectors))
	}
	for index, want := range []string{"uri-0", "uri-1", "uri-2", "uri-1"} {
		entry := selectors[index]
		if len(entry.selector.Atoms) != 1 || entry.selector.Status != "current" {
			t.Fatalf("selector %d (%s) = %#v, want exactly one current atom", index, want, entry.selector)
		}
		if entry.selector.Atoms[0].URI != want {
			t.Fatalf("selector %d owns %q, want %q", index, entry.selector.Atoms[0].URI, want)
		}
	}
	owners := service.selectorsByAtom["uri-1"]
	if len(owners) != 2 || owners[0].EvidenceFile != "___diffs/extra.json" || owners[0].Note != "extra" {
		t.Fatalf("owners of uri-1 = %#v, want the extra evidence file sorted first", owners)
	}
	if owners[1].EvidenceFile != "___diffs/root.json" || owners[1].Note != "second" {
		t.Fatalf("second owner of uri-1 = %#v, want the root evidence file note", owners[1])
	}
	if stored, ok := service.atomByURI["uri-2"]; !ok || service.changes.Atoms[stored].Key != "key-2" {
		t.Fatalf("atom index missed uri-2: %#v", service.atomByURI)
	}
}

func TestSelectorIdentityKeepsFirstEntryForDuplicateEvidencePaths(t *testing.T) {
	// Absolute evidence paths are redacted to the empty string, so two files
	// under one target can share a selector identity. The scan the index
	// replaced stopped at the first match, and that must not change.
	first := saga.DiffFile{Version: 2, Path: "/private/one.json", Diffs: []saga.DiffReference{{URI: "uri-0", Note: "first file"}}}
	second := saga.DiffFile{Version: 2, Path: "/private/two.json", Diffs: []saga.DiffReference{{URI: "uri-1", Note: "second file"}}}
	report := coverage.Report{Ownership: map[string][]coverage.Assignment{
		"key-0": {{Target: scaleTarget, DiffFile: "/private/two.json", Diff: 1}},
	}}
	service := linkedSession(sagaWithDiffs(scaleTarget, first, second), gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atomFor(0)}}, report)

	selectors := service.selectors[scaleTarget]
	if len(selectors[0].selector.Atoms) != 1 || selectors[0].selector.Status != "current" {
		t.Fatalf("first selector = %#v, want the atom attributed to it", selectors[0].selector)
	}
	if len(selectors[1].selector.Atoms) != 0 || selectors[1].selector.Status != "stale" {
		t.Fatalf("second selector = %#v, want no atoms", selectors[1].selector)
	}
	if owners := service.selectorsByAtom["uri-0"]; len(owners) != 1 || owners[0].Note != "first file" {
		t.Fatalf("owners = %#v, want the first duplicate entry", owners)
	}
}

func TestCleanDiagnosticPathRedactsPortableAbsolutePaths(t *testing.T) {
	for _, value := range []string{
		"/private/evidence.json",
		`C:\private\evidence.json`,
		"C:/private/evidence.json",
		`\\server\share\evidence.json`,
	} {
		if got := cleanDiagnosticPath(value); got != "" {
			t.Errorf("cleanDiagnosticPath(%q) = %q, want redacted", value, got)
		}
	}
}

func TestStaleSelectorsReuseCoverageOrphanReasons(t *testing.T) {
	file := saga.DiffFile{Version: 2, Path: "___diffs/root.json", Diffs: []saga.DiffReference{
		{URI: "uri-0", Note: "matched"}, {URI: "broken", Note: "unparsable"}, {URI: "uri-missing", Note: "no orphan record"},
	}}
	report := coverage.Report{
		Ownership: map[string][]coverage.Assignment{"key-0": {{Target: scaleTarget, DiffFile: "___diffs/root.json", Diff: 1}}},
		Orphans: []coverage.Orphan{
			{Assignment: coverage.Assignment{Target: "urn:change-saga:scale:other", DiffFile: "___diffs/root.json", Diff: 2}, Reason: "wrong target"},
			{Assignment: coverage.Assignment{Target: scaleTarget, DiffFile: "___diffs/root.json", Diff: 2}, Reason: "diff URI is not a Change Saga diff URI"},
		},
	}
	service := linkedSession(sagaWithDiffs(scaleTarget, file), gitdiff.ChangeSet{Atoms: []gitdiff.Atom{atomFor(0)}}, report)

	selectors := service.selectors[scaleTarget]
	if selectors[0].stale != nil || selectors[0].selector.Status != "current" {
		t.Fatalf("matched selector = %#v, want current", selectors[0])
	}
	if selectors[1].stale == nil || selectors[1].stale.Reason != "diff URI is not a Change Saga diff URI" {
		t.Fatalf("orphan reason = %#v, want the reason coverage recorded", selectors[1].stale)
	}
	if selectors[1].stale.Target != scaleTarget || selectors[1].stale.URI != "broken" || selectors[1].stale.EvidenceFile != "___diffs/root.json" {
		t.Fatalf("stale projection = %#v", selectors[1].stale)
	}
	if selectors[2].stale == nil || selectors[2].stale.Reason != "diff URI does not match the current source comparison" {
		t.Fatalf("unrecorded orphan = %#v, want the default reason", selectors[2].stale)
	}
}

// scaleFixture holds one synthetic saga whose selectors and atoms grow
// together, which is the shape that made the former per-assignment scan
// quadratic. Only the session is rebuilt per measurement; the fixture data is
// read-only, so it is shared across iterations.
type scaleFixture struct {
	document *saga.Saga
	changes  gitdiff.ChangeSet
	report   coverage.Report
}

func newScaleFixture(size int) scaleFixture {
	file := saga.DiffFile{Version: 2, Path: "___diffs/root.json", Diffs: make([]saga.DiffReference, 0, 2*size)}
	atoms := make([]gitdiff.Atom, 0, size)
	ownership := make(map[string][]coverage.Assignment, size)
	orphans := make([]coverage.Orphan, 0, size)
	for index := 0; index < size; index++ {
		atom := atomFor(index)
		file.Diffs = append(file.Diffs, saga.DiffReference{URI: atom.URI, Note: "note " + strconv.Itoa(index)})
		atoms = append(atoms, atom)
		ownership[atom.Key] = []coverage.Assignment{{Target: scaleTarget, DiffFile: file.Path, Diff: index + 1}}
	}
	for index := 0; index < size; index++ {
		suffix := strconv.Itoa(index)
		file.Diffs = append(file.Diffs, saga.DiffReference{URI: "stale-" + suffix, Note: "stale " + suffix})
		orphans = append(orphans, coverage.Orphan{
			Assignment: coverage.Assignment{Target: scaleTarget, DiffFile: file.Path, Diff: size + index + 1},
			Reason:     "diff URI does not match the current source comparison",
		})
	}
	return scaleFixture{
		document: sagaWithDiffs(scaleTarget, file),
		changes:  gitdiff.ChangeSet{Atoms: atoms},
		report:   coverage.Report{Ownership: ownership, Orphans: orphans},
	}
}

func (f scaleFixture) link() *session { return linkedSession(f.document, f.changes, f.report) }

func TestScaleFixtureResolvesEverySelector(t *testing.T) {
	const size = 64
	service := newScaleFixture(size).link()
	current, stale := 0, 0
	for _, entry := range service.selectors[scaleTarget] {
		if entry.stale != nil {
			stale++
			continue
		}
		if len(entry.selector.Atoms) != 1 || entry.selector.Atoms[0].URI != entry.selector.URI {
			t.Fatalf("selector %q owns %#v, want its own atom", entry.selector.URI, entry.selector.Atoms)
		}
		current++
	}
	if current != size || stale != size {
		t.Fatalf("current = %d, stale = %d, want %d of each", current, stale, size)
	}
}

// TestSelectorLinkingScalesLinearly is the regression for the quadratic scan.
// An eightfold saga costs about eight times as much to link when selector
// identity is a map lookup, and about sixty-four times as much when it is a
// scan, so the allowance below sits well clear of linear noise and well below
// quadratic growth.
func TestSelectorLinkingScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("scale regression measures work per saga size")
	}
	const (
		base      = 256
		growth    = 8
		allowance = 24.0
	)
	small, large := newScaleFixture(base), newScaleFixture(base*growth)

	// Allocation growth guards the hoisted path normalization; the timing
	// comparison below is what actually catches a reintroduced scan.
	smallAllocs := testing.AllocsPerRun(3, func() { small.link() })
	largeAllocs := testing.AllocsPerRun(3, func() { large.link() })
	if ratio := largeAllocs / smallAllocs; ratio > allowance {
		t.Fatalf("allocations grew %.1fx for %dx the saga (%.0f -> %.0f), want at most %.0fx", ratio, growth, smallAllocs, largeAllocs, allowance)
	}

	// A loaded machine can distort a single measurement, so the best of a few
	// rounds is taken. A reintroduced scan stays near the quadratic ratio in
	// every round, so retrying cannot hide one.
	best := 0.0
	for attempt := 0; attempt < 3; attempt++ {
		smallTime := float64(testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				small.link()
			}
		}).NsPerOp())
		largeTime := float64(testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				large.link()
			}
		}).NsPerOp())
		if smallTime <= 0 {
			t.Skip("timing resolution too coarse to compare")
		}
		ratio := largeTime / smallTime
		if best == 0 || ratio < best {
			best = ratio
		}
		if best <= allowance {
			return
		}
	}
	t.Fatalf("linking took %.1fx longer for %dx the saga, want at most %.0fx", best, growth, allowance)
}

func BenchmarkSelectorLinking(b *testing.B) {
	for _, size := range []int{256, 1024, 4096} {
		fixture := newScaleFixture(size)
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				fixture.link()
			}
		})
	}
}
