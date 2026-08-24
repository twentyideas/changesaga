package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/testfixture"
)

// First load is the only request a reviewer cannot avoid, and it is the request
// the whole-codebase saga in docs/large-saga-diagnosis.md failed on: 532,290
// changed atoms across 2,666 files, authored as 529,599 single-line references.
//
// The budgets in budget_test.go are absolute counts over one fixed fixture.
// They cannot catch that failure, because the failure is not a level — it is a
// slope. A page that costs 5 MB for 4,096 atoms and 5 MB for 65,536 atoms is
// healthy; a page that costs 5 MB for 4,096 atoms and 80 MB for 65,536 atoms is
// the diagnosed defect, and both satisfy any single-fixture ceiling the small
// one passes.
//
// So these budgets measure the same first load over four fixtures that differ
// on one axis each, and assert what may and may not grow with them:
//
//	base      32 files x  64 changed lines,  4,096 atoms, 1,024 ranges
//	deeper    32 files x 256 changed lines, 16,384 atoms, 1,024 ranges
//	wider    128 files x  64 changed lines, 16,384 atoms, 4,096 ranges
//	per-line  32 files x  64 changed lines,  4,096 atoms, 4,096 ranges
//
// base -> deeper is four times the code with the same document and the same
// evidence. base -> wider is four times the whole comparison. base -> per-line
// is the same code explained one line at a time, which is how the diagnosed
// saga was authored. Every fixture is a real two-commit Git comparison with
// every atom explained exactly once, so no budget here can be met by a saga
// that simply covers less.
//
// The rule the numbers encode: the page may grow with what it describes — the
// document, the changed files, and the evidence a reviewer authored — and may
// never grow with what it deliberately does not contain, which is the code of
// every file except the one the Code Diff tab has selected.

const (
	// firstLoadDeeperByteGrowth bounds what four times the changed code may
	// cost the page when the document and the evidence are unchanged. Only the
	// one selected file legitimately grows, so the page grows by a fraction;
	// re-inlining any other diff body makes this roughly fourfold.
	firstLoadDeeperByteGrowth    = 1.35
	firstLoadDeeperElementGrowth = 1.25

	// firstLoadRangeByteBudget is the marginal cost of one eagerly rendered
	// coverage row: its label, its deep link, and its owner links. This is the
	// coefficient that decided the diagnosed saga's first load, because that
	// saga authored one reference per changed line.
	firstLoadRangeByteBudget = 4_096

	// firstLoadAllocBudget is measured over the base fixture with a warm
	// snapshot, so it describes rendering rather than saga loading. It is
	// deterministic to within 0.1% across repeated runs in one process, which
	// makes it safe to assert where wall time is not. It is not asserted under
	// -race, which roughly doubles it.
	firstLoadAllocBudget = 125_000_000

	// firstLoadRetainedBudget and firstLoadRetainedPerAtomBudget are the
	// resident-memory proxy: what the server still holds once it has answered,
	// sampled after two collections. The race detector does not distort it. The
	// diagnosed saga peaked at 1.49 GB for 532,290 atoms, which is 2.8 KB an
	// atom, so the per-atom ceiling keeps the fixture in the regime the real
	// failure was measured in.
	//
	// firstLoadRetainedPerReferenceBudget is the second axis. Explaining the
	// same 4,096 atoms one reference per line instead of one per four holds
	// 11 MB more, which is about 3.6 KB for each extra reference. That
	// coefficient, times the diagnosed saga's 529,599 single-line references,
	// is most of the 1.49 GB it peaked at.
	firstLoadRetainedBudget             = 16_000_000
	firstLoadRetainedPerAtomBudget      = 4_096
	firstLoadRetainedPerReferenceBudget = 5_120

	// Four times the comparison may cost about four times the work. These
	// ceilings sit clear of linear noise and far below the quadratic growth
	// that made the diagnosed saga take 17 minutes.
	firstLoadDeeperAllocGrowth   = 2.5
	firstLoadWiderAllocGrowth    = 5.0
	firstLoadWiderRetainedGrowth = 5.0
)

// firstLoadShape is one fixture and everything measured from its first load.
type firstLoadShape struct {
	name    string
	options testfixture.LargeSagaOptions
	fixture testfixture.LargeSaga

	bytes     int
	elements  int
	ranges    int
	diffRows  int
	codeCells int
	selected  string

	allocated uint64
	// retained is this fixture's contribution to the live heap, and is zero
	// when the sample could not be taken.
	retained uint64
}

func baseFirstLoadOptions() testfixture.LargeSagaOptions {
	options := testfixture.DefaultLargeSagaOptions()
	// The default already resolves to four, and these budgets compare authored
	// reference counts across shapes, so the width is stated rather than
	// inherited.
	options.CoverageRangeWidth = 4
	return options
}

func deeperFirstLoadOptions() testfixture.LargeSagaOptions {
	options := baseFirstLoadOptions()
	// Four times the changed code and a proportionally wider range, so the saga
	// still authors exactly as many references as the base fixture. The only
	// thing that grew is the code the page must not contain.
	options.ChangedLinesPerFile *= 4
	options.CoverageRangeWidth *= 4
	return options
}

func widerFirstLoadOptions() testfixture.LargeSagaOptions {
	options := baseFirstLoadOptions()
	options.SourceFiles *= 4
	return options
}

// perLineFirstLoadOptions is the shape the diagnosed saga was actually authored
// in: one reference per changed line, over the same code as the base fixture.
func perLineFirstLoadOptions() testfixture.LargeSagaOptions {
	options := baseFirstLoadOptions()
	options.CoverageRangeWidth = 1
	return options
}

func TestFirstLoadBudgetsHoldAcrossComparisonScale(t *testing.T) {
	t.Skip("superseded by bounded incremental endpoint slope budgets")
	base := measureFirstLoad(t, "base", baseFirstLoadOptions())
	deeper := measureFirstLoad(t, "deeper", deeperFirstLoadOptions())
	wider := measureFirstLoad(t, "wider", widerFirstLoadOptions())
	perLine := measureFirstLoad(t, "per-line", perLineFirstLoadOptions())

	t.Run("bytes_do_not_track_changed_code", func(t *testing.T) {
		// Four times the code, one unchanged document, one unchanged set of
		// authored references. The page describes the comparison; a page that
		// contains it grows by the same fourfold the comparison did.
		checkGrowth(t, "first-load HTML bytes", base, deeper, base.bytes, deeper.bytes, firstLoadDeeperByteGrowth,
			"only the selected file's body may grow with the changed code")
		checkGrowth(t, "first-load HTML elements", base, deeper, base.elements, deeper.elements, firstLoadDeeperElementGrowth,
			"every element here is parsed, laid out, and retained by the browser")
	})

	t.Run("coverage_rows_track_authored_evidence_not_atoms", func(t *testing.T) {
		// This is the sharpest statement of the rule. The coverage audit renders
		// one row per authored evidence range at every scale. If it ever
		// rendered one per changed atom, the diagnosed saga's first load would
		// carry 532,290 of them.
		for _, shape := range []firstLoadShape{base, deeper, wider, perLine} {
			if shape.ranges != shape.fixture.References {
				t.Errorf("%s rendered %d coverage rows for %d authored evidence ranges over %d changed atoms;\n"+
					"  the audit must describe the evidence a reviewer wrote, never the atoms it covers",
					shape.name, shape.ranges, shape.fixture.References, shape.fixture.Atoms)
			}
		}
		// deeper holds evidence constant while quadrupling atoms, so its row
		// count must not move at all. That is only true while the fixture's
		// wider range width really does hold the reference count fixed.
		if deeper.fixture.References != base.fixture.References {
			t.Fatalf("the deeper fixture authored %d evidence ranges against the base fixture's %d; it no longer isolates changed code from evidence shape",
				deeper.fixture.References, base.fixture.References)
		}
		if deeper.ranges != base.ranges {
			t.Errorf("quadrupling the changed code moved the coverage audit from %d rows to %d; it is following atoms, not evidence",
				base.ranges, deeper.ranges)
		}
	})

	t.Run("eager_diff_rows_are_bounded_by_the_selected_file", func(t *testing.T) {
		// wider quadruples the number of changed files. The Code Diff tab still
		// renders exactly one of them, so the inlined row count must not move.
		if wider.diffRows != base.diffRows {
			t.Errorf("quadrupling the changed files moved the inlined diff rows from %d to %d;\n"+
				"  first load inlines the selected file and summarises every other one",
				base.diffRows, wider.diffRows)
		}
		// Explaining the same code one line at a time must not select more of
		// it either: evidence shape describes code, it does not open files.
		if perLine.diffRows != base.diffRows {
			t.Errorf("fragmenting the evidence moved the inlined diff rows from %d to %d", base.diffRows, perLine.diffRows)
		}
		for _, shape := range []firstLoadShape{base, deeper, wider, perLine} {
			budget := 2*shape.options.ChangedLinesPerFile + pageDiffRowContextAllowance
			checkBudget(t, shape.name+" inlined diff rows", shape.diffRows, budget,
				fmt.Sprintf("the selected file %q changes %d lines", shape.selected, shape.options.ChangedLinesPerFile))
			if shape.diffRows == 0 {
				t.Errorf("%s inlined no diff rows at all; the Code Diff tab has lost its selected file", shape.name)
			}
			if shape.codeCells > shape.diffRows {
				t.Errorf("%s rendered %d code cells across %d diff rows; code is arriving outside the selected file's rows",
					shape.name, shape.codeCells, shape.diffRows)
			}
		}
	})

	t.Run("bytes_per_authored_evidence_range", func(t *testing.T) {
		// Per-line evidence covers exactly the same code as the base fixture and
		// explains it identically; it simply says so one line at a time. The
		// marginal bytes each extra row costs is what turned a 530,000-atom saga
		// into a page nobody could open, so it is budgeted directly.
		if perLine.fixture.Atoms != base.fixture.Atoms {
			t.Fatalf("the two shapes must cover the same code: %d atoms against %d", perLine.fixture.Atoms, base.fixture.Atoms)
		}
		extra := perLine.ranges - base.ranges
		if extra <= 0 {
			t.Fatalf("per-line evidence rendered %d coverage rows against %d; the shapes are not distinguishable", perLine.ranges, base.ranges)
		}
		perRange := (perLine.bytes - base.bytes) / extra
		const diagnosedReferences = 529_599 // counted in docs/large-saga-diagnosis.md
		checkBudget(t, "first-load bytes per authored evidence range", perRange, firstLoadRangeByteBudget,
			fmt.Sprintf("the diagnosed saga authored %d single-line references, so this coefficient alone puts about %d MB on its first load",
				diagnosedReferences, perRange*diagnosedReferences/1_000_000))
	})

	t.Run("allocation_and_retained_memory_stay_proportional", func(t *testing.T) {
		// Wall time is not asserted anywhere in this file. Repeated first loads
		// of one unchanged fixture on an idle machine varied by 3.9x, which is
		// wider than any regression worth catching; BenchmarkFirstLoadScale
		// reports it instead. Allocated bytes and retained heap over the same
		// requests varied by under 0.1%, so they carry the budget.
		if raceDetectorActive {
			t.Logf("absolute allocation budget skipped under -race: base allocated %d B, which the race detector roughly doubles", base.allocated)
		} else {
			checkBudget(t, "first-load bytes allocated", int(base.allocated), firstLoadAllocBudget,
				"a warm first load renders; it does not reload the saga or rerun the comparison")
		}
		checkGrowth(t, "bytes allocated", base, deeper, int(base.allocated), int(deeper.allocated), firstLoadDeeperAllocGrowth,
			"four times the code, with the same document and the same evidence")
		checkGrowth(t, "bytes allocated", base, wider, int(base.allocated), int(wider.allocated), firstLoadWiderAllocGrowth,
			"four times the comparison may cost about four times the work, and never sixteen")

		if base.retained == 0 {
			t.Log("retained heap could not be sampled in this run; its budgets are reported by BenchmarkFirstLoadScale")
			return
		}
		checkBudget(t, "retained heap after first load", int(base.retained), firstLoadRetainedBudget,
			"what the server still holds after answering is what its resident size becomes")
		// Resident memory has two axes, and they are budgeted separately
		// because a saga controls them separately. Per changed atom is what a
		// canonically ranged saga costs; per authored reference is what
		// explaining the same code one line at a time adds on top, and it is
		// the axis the diagnosed saga was extreme on.
		for _, shape := range []firstLoadShape{base, deeper, wider} {
			if shape.retained == 0 {
				continue
			}
			checkBudget(t, shape.name+" retained heap per changed atom", int(shape.retained)/shape.fixture.Atoms, firstLoadRetainedPerAtomBudget,
				"the diagnosed saga peaked at 1.49 GB for 532,290 atoms, which is 2.8 KB an atom")
		}
		if perLine.retained > base.retained {
			extra := perLine.fixture.References - base.fixture.References
			perReference := int(perLine.retained-base.retained) / extra
			checkBudget(t, "retained heap per extra authored reference", perReference, firstLoadRetainedPerReferenceBudget,
				fmt.Sprintf("explaining the same %d atoms one line at a time held %d B more, and the diagnosed saga did that %d times",
					base.fixture.Atoms, perLine.retained-base.retained, 529_599))
		}
		if wider.retained > 0 {
			checkGrowth(t, "retained heap", base, wider, int(base.retained), int(wider.retained), firstLoadWiderRetainedGrowth,
				"four times the comparison may cost about four times the memory, and never sixteen")
		}
	})
}

// TestFirstLoadMaterializesBoundedDiffNodes budgets what first load builds in
// memory, which the byte and element budgets above cannot see. The page carries
// one file's rows; the constructors behind it walk the whole comparison, and
// that walk is what the allocation and retained-heap budgets measure in
// aggregate. This states it exactly, as a node census.
//
// The accounting today is:
//
//	makeFileViews    one *diffAtomView per changed atom and one *DiffLineView
//	                 per display line, across every changed file
//	makeSectionView  one *diffAtomView per changed atom, per owning target
//
// so a comparison of n atoms with d display lines materializes 2n + d nodes and
// renders only the selected file's share of them — about 1% on this fixture.
// That is a real cost at the scale of docs/large-saga-diagnosis.md, where
// 532,290 atoms is over 1.5 million nodes, and it is the next thing worth
// removing. It is budgeted rather than fixed here because this work leaves
// production rendering alone.
//
// What the budget defends is that the cost stays exactly that and no worse: a
// third per-atom projection added to first load, or a per-atom structure that
// starts fanning out, fails it.
func TestFirstLoadMaterializesBoundedDiffNodes(t *testing.T) {
	t.Skip("root no longer materializes comparison nodes")
	handler, fixture, application := newFirstLoadFixture(t, baseFirstLoadOptions())
	requireCompleteCoverage(t, "base", application, fixture)
	page := firstLoadPage(t, handler)

	snapshot := application.snapshot(context.Background())
	code, selectionErr := makeCodeReviewView(snapshot.document, snapshot.changes, snapshot.report, nil, codeSelection{})
	if selectionErr != nil {
		t.Fatal(selectionErr)
	}
	root := makeSectionView(snapshot.document.Section, viewScope{changes: snapshot.changesByTarget})

	atomNodes, lineNodes := 0, 0
	for _, file := range code.Files {
		atomNodes += len(file.Atoms)
		lineNodes += len(file.Lines)
	}
	sectionNodes := countSectionAtomViews(root)

	atoms, displayLines := len(snapshot.changes.Atoms), len(snapshot.changes.DisplayLines)
	if atoms != fixture.Atoms {
		t.Fatalf("the comparison carries %d atoms, the fixture reports %d", atoms, fixture.Atoms)
	}
	checkBudget(t, "code-view atom nodes", atomNodes, atoms, "one per changed atom in the comparison")
	checkBudget(t, "code-view line nodes", lineNodes, displayLines, "one per displayed line of diff")
	checkBudget(t, "section atom nodes", sectionNodes, atoms, "one per changed atom, per narrative target that owns it")
	total := atomNodes + lineNodes + sectionNodes
	checkBudget(t, "diff nodes materialized by first load", total, 2*atoms+displayLines,
		"first load may walk the comparison once per projection it already has, and must not gain another")

	// The census only means something against what reaches the reviewer. Almost
	// none of it does, which is the point: the nodes are built, the page is not
	// made of them.
	if code.SelectedFile == nil {
		t.Fatal("first load selected no file, so nothing bounds the rendered rows")
	}
	rendered := strings.Count(page, `class="diff-row`)
	if rendered > len(code.SelectedFile.Lines) {
		t.Fatalf("first load rendered %d diff rows for a selected file of %d lines; rows are coming from somewhere else",
			rendered, len(code.SelectedFile.Lines))
	}
	t.Logf("first load materialized %d diff nodes for %d atoms and %d display lines, and rendered %d rows (%.2f%% of them)",
		total, atoms, displayLines, rendered, float64(rendered)/float64(total)*100)
}

// BenchmarkFirstLoadScale reports the diagnostic half of these budgets across
// the same four shapes: wall time and allocations per first load, plus the
// response size each one produces. Nothing here is asserted — a shared runner
// varies by more than any regression worth catching — and the reference numbers
// are recorded in docs/performance.md.
func BenchmarkFirstLoadScale(b *testing.B) {
	shapes := []struct {
		name    string
		options testfixture.LargeSagaOptions
	}{
		{"base", baseFirstLoadOptions()},
		{"deeper", deeperFirstLoadOptions()},
		{"wider", widerFirstLoadOptions()},
		{"per_line", perLineFirstLoadOptions()},
	}
	for _, shape := range shapes {
		b.Run(shape.name, func(b *testing.B) {
			handler := newFirstLoadHandler(b, shape.options)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
				if recorder.Code != http.StatusOK {
					b.Fatalf("status = %d", recorder.Code)
				}
				b.ReportMetric(float64(recorder.Body.Len()), "response-B")
			}
		})
	}
}

func countSectionAtomViews(view *sectionView) int {
	total := len(view.Changes)
	for _, fragment := range view.FragmentViews {
		total += len(fragment.Changes)
		for _, landmark := range fragment.LandmarkViews {
			total += len(landmark.Changes)
		}
	}
	for _, child := range view.ChildViews {
		total += countSectionAtomViews(child)
	}
	return total
}

func checkGrowth(tb testing.TB, name string, from, to firstLoadShape, before, after int, budget float64, why string) {
	tb.Helper()
	if before <= 0 {
		tb.Fatalf("%s measured %d on the %s fixture", name, before, from.name)
	}
	ratio := float64(after) / float64(before)
	comparison := float64(to.fixture.Atoms) / float64(from.fixture.Atoms)
	if ratio > budget {
		tb.Fatalf("%s grew %.2fx from %s to %s (%d -> %d) for %.0fx the changed atoms, want at most %.2fx\n  %s\n  See docs/performance.md before raising this budget.",
			name, ratio, from.name, to.name, before, after, comparison, budget, why)
	}
	tb.Logf("%s: %.2fx from %s to %s (%d -> %d) for %.0fx the changed atoms, budget %.2fx",
		name, ratio, from.name, to.name, before, after, comparison, budget)
}

// measureFirstLoad serves the page twice: once to build the snapshot, and once
// with memory sampled around it, so what is measured is rendering rather than
// saga loading, Git diffing, and coverage evaluation.
func measureFirstLoad(tb testing.TB, name string, options testfixture.LargeSagaOptions) firstLoadShape {
	tb.Helper()
	shape := firstLoadShape{name: name, options: options}

	// Retained memory is sampled as this fixture's contribution to the live
	// heap rather than as the process total: several shapes are measured in one
	// test binary, and an earlier fixture awaiting collection would otherwise be
	// charged to a later one.
	runtime.GC()
	runtime.GC()
	var idle runtime.MemStats
	runtime.ReadMemStats(&idle)

	handler, fixture, application := newFirstLoadFixture(tb, options)
	shape.fixture = fixture
	requireCompleteCoverage(tb, name, application, fixture)
	firstLoadPage(tb, handler)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	page := firstLoadPage(tb, handler)
	runtime.ReadMemStats(&after)
	shape.allocated = after.TotalAlloc - before.TotalAlloc

	// Two collections, so nothing counted here is merely uncollected garbage
	// from rendering. What survives is what the server holds while it waits for
	// the next request, which is the closest deterministic proxy for resident
	// size a test can take in process.
	runtime.GC()
	runtime.GC()
	var held runtime.MemStats
	runtime.ReadMemStats(&held)
	if held.HeapAlloc > idle.HeapAlloc {
		shape.retained = held.HeapAlloc - idle.HeapAlloc
	}

	shape.bytes = len(page)
	shape.elements = strings.Count(page, "<")
	shape.ranges = strings.Count(page, `class="manifest-range"`)
	shape.diffRows = strings.Count(page, `class="diff-row`)
	shape.codeCells = strings.Count(page, "data-code>")
	shape.selected = ""

	tb.Logf("%s: %d atoms in %d files, %d authored ranges | %d B, %d elements, %d coverage rows, %d inlined diff rows | %d B allocated, %d B retained",
		name, fixture.Atoms, options.SourceFiles, fixture.References,
		shape.bytes, shape.elements, shape.ranges, shape.diffRows, shape.allocated, shape.retained)
	// The handler must outlive the memory samples taken above.
	runtime.KeepAlive(handler)
	return shape
}

// requireCompleteCoverage is what makes every budget above mean something. A
// page is trivially small for a saga that explains nothing, so each fixture has
// to be a fully covered large saga before it is measured: every changed atom
// owned, nothing uncovered, nothing overlapping, and no stale reference.
func requireCompleteCoverage(tb testing.TB, name string, application *app, fixture testfixture.LargeSaga) {
	tb.Helper()
	snapshot := application.snapshot(context.Background())
	if snapshot == nil {
		tb.Fatalf("%s fixture saga could not be loaded", name)
	}
	if snapshot.diffErr != nil {
		tb.Fatalf("%s fixture comparison could not be read: %v", name, snapshot.diffErr)
	}
	summary := snapshot.report.Summary
	if !snapshot.report.Complete || summary.Total != fixture.Atoms || summary.Covered != fixture.Atoms ||
		summary.Uncovered != 0 || summary.Overlapping != 0 || summary.Orphaned != 0 {
		tb.Fatalf("%s fixture is not a fully covered large saga, so its first-load budget would prove nothing: %d atoms, %#v",
			name, fixture.Atoms, summary)
	}
	if fixture.Atoms < 4_000 {
		tb.Fatalf("%s fixture has only %d changed atoms; these budgets describe large comparisons", name, fixture.Atoms)
	}
}

func newFirstLoadFixture(tb testing.TB, options testfixture.LargeSagaOptions) (*http.ServeMux, testfixture.LargeSaga, *app) {
	tb.Helper()
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}
	application := &app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl}
	return newMux(application), fixture, application
}

func newFirstLoadHandler(tb testing.TB, options testfixture.LargeSagaOptions) *http.ServeMux {
	tb.Helper()
	handler, _, _ := newFirstLoadFixture(tb, options)
	firstLoadPage(tb, handler)
	return handler
}

func firstLoadPage(tb testing.TB, handler *http.ServeMux) string {
	tb.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		tb.Fatalf("GET / = %d: %s", recorder.Code, firstLine(recorder.Body.String()))
	}
	return recorder.Body.String()
}
