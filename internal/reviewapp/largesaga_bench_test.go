package reviewapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

// Large-saga regression benchmark for the two calls a reviewer's first request
// makes: reviewapp.Open, which loads the saga, reads the comparison, evaluates
// coverage and builds the selector index, and Session.Overview, which answers
// `change-saga query overview` from that index.
//
// What it measures. Four fixture shapes over one comparison — 32 files, 4,096
// changed atoms, the same chapters, fragments and review overlay throughout —
// differing only in how that evidence is written:
//
//   - authored_ranges is the shape a reviewer writes with `cover --lines`:
//     1,024 four-line ranged references spread over every fragment, so no
//     target owns more than eight selectors.
//   - per_line_evidence_64/16/4_targets deliberately bypasses the authoring
//     canonicalization and stores one reference per changed line. The same
//     4,096 references are concentrated onto fewer targets at each step, which
//     defends the reader against malformed or adversarial producers.
//
// The former implementation scanned one target's selectors once per atom that
// target owned. former-scan-steps/atom reports exactly how much work that loop
// would perform for each fixture: it quadruples as targets are quartered, while
// the direct selector identity index must keep actual construction near-linear.
//
// What it does not measure. Nothing here asserts a wall-clock time or an
// allocation count: benchmark hosts vary, and docs/performance.md keeps timing
// and allocation figures as diagnostics rather than budgets. ns/op, B/op and
// allocs/op are reported for comparison against the reference results recorded
// there. The fixture is four orders of magnitude below the whole-codebase saga
// in docs/large-saga-diagnosis.md — 2.1e6 former scan steps against 1.19e10 —
// so it reproduces the pathological shape, not its absolute cost. Cold-cache
// filesystem behaviour is not represented: fixtures stay warm.

type largeSagaShape struct {
	name    string
	options testfixture.LargeSagaOptions
}

// largeSagaConcentrations quarter the targets that own the comparison at each
// step. The last is the proportion a real saga reaches: the whole-codebase saga
// in docs/large-saga-diagnosis.md has one target owning 37,700 of its 532,290
// atoms, and one of four targets here owns 1,024 of 4,096.
var largeSagaConcentrations = []int{64, 16, 4}

func largeSagaShapes() []largeSagaShape {
	shapes := []largeSagaShape{{name: "authored_ranges", options: testfixture.DefaultLargeSagaOptions()}}
	for _, targets := range largeSagaConcentrations {
		options := testfixture.DefaultLargeSagaOptions()
		options.CoverageRangeWidth = 1
		options.CoverageTargets = targets
		shapes = append(shapes, largeSagaShape{name: fmt.Sprintf("per_line_evidence_%d_targets", targets), options: options})
	}
	return shapes
}

func BenchmarkLargeSagaOpen(b *testing.B) {
	ctx := context.Background()
	for _, shape := range largeSagaShapes() {
		b.Run(shape.name, func(b *testing.B) {
			fixture := largeSagaFixture(b, shape)
			options := OpenOptions{SagaRoot: fixture.Root, SourceDir: fixture.Repository}
			steps := formerSelectorScanSteps(b, fixture)

			b.ReportAllocs()
			for b.Loop() {
				opened, err := Open(ctx, options)
				if err != nil {
					b.Fatal(err)
				}
				if opened.Snapshot() == "" {
					b.Fatal("session opened without a snapshot")
				}
			}
			reportLargeSagaMetrics(b, fixture, steps)
		})
	}
}

// BenchmarkLargeSagaOverview times the query itself against an already-open
// session, so it reports what a reviewer pays per request once the index is
// warm rather than what they pay to build it.
func BenchmarkLargeSagaOverview(b *testing.B) {
	ctx := context.Background()
	for _, shape := range largeSagaShapes() {
		b.Run(shape.name, func(b *testing.B) {
			fixture := largeSagaFixture(b, shape)
			opened := openLargeSaga(b, fixture)
			steps := formerSelectorScanSteps(b, fixture)

			b.ReportAllocs()
			for b.Loop() {
				overview, err := opened.Overview(ctx, OverviewQuery{})
				if err != nil {
					b.Fatal(err)
				}
				if len(overview.Chapters) != fixture.Chapters || !overview.Coverage.Complete {
					b.Fatalf("overview shape changed: chapters=%d coverage=%#v", len(overview.Chapters), overview.Coverage)
				}
			}
			reportLargeSagaMetrics(b, fixture, steps)
		})
	}
}

// BenchmarkLargeSagaSelectorConstruction isolates the part of Open that grows
// nonlinearly. Loading the saga, reading the comparison and evaluating
// coverage all happen once, outside the timed region, so what remains is the
// selector index build. It constructs the same session value Open does; if
// Open gains state, this fails rather than silently measuring less.
func BenchmarkLargeSagaSelectorConstruction(b *testing.B) {
	ctx := context.Background()
	for _, shape := range largeSagaShapes() {
		b.Run(shape.name, func(b *testing.B) {
			fixture := largeSagaFixture(b, shape)
			document, changes, report := largeSagaInputs(b, fixture)
			steps := formerSelectorScanSteps(b, fixture)

			b.ReportAllocs()
			for b.Loop() {
				if err := newLargeSagaSession(document, changes, report).build(ctx); err != nil {
					b.Fatal(err)
				}
			}
			reportLargeSagaMetrics(b, fixture, steps)
		})
	}
}

// TestLargeSagaBenchmarkShapesRemainRealistic guards the benchmark rather than
// the product. Every shape must be a valid, completely covered saga that Open
// accepts and Overview answers, the per-line shapes must keep their evidence
// concentrated, and the scan cost of the implementation this benchmark guards
// against must keep growing faster than atom count. If a fixture change
// flattens that hypothetical cost, the benchmark stops exercising its intended
// regression shape and this fails first.
func TestLargeSagaBenchmarkShapesRemainRealistic(t *testing.T) {
	ctx := context.Background()
	stepsByTargets := map[int]int64{}
	var rangedSteps, rangedAtoms int64
	for _, shape := range largeSagaShapes() {
		t.Run(shape.name, func(t *testing.T) {
			fixture := largeSagaFixture(t, shape)
			if fixture.Mappings != fixture.Atoms {
				t.Fatalf("fixture does not map every atom exactly once: atoms=%d mappings=%d", fixture.Atoms, fixture.Mappings)
			}
			overview, err := openLargeSaga(t, fixture).Overview(ctx, OverviewQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if !overview.Coverage.Complete || overview.Coverage.Covered != fixture.Atoms || overview.Coverage.Stale != 0 || overview.Coverage.Overlapping != 0 {
				t.Fatalf("benchmarked saga is not completely covered: %#v", overview.Coverage)
			}
			if len(overview.Chapters) != fixture.Chapters {
				t.Fatalf("overview reported %d chapters, want %d", len(overview.Chapters), fixture.Chapters)
			}

			steps := formerSelectorScanSteps(t, fixture)
			t.Logf("atoms=%d references=%d targets=%d max-target-refs=%d former-scan-steps=%d (%.1f per atom)",
				fixture.Atoms, fixture.References, fixture.CoverageTargets, fixture.MaxTargetReferences, steps, float64(steps)/float64(fixture.Atoms))
			if shape.options.CoverageRangeWidth != 1 {
				rangedSteps, rangedAtoms = steps, int64(fixture.Atoms)
				return
			}
			if fixture.References != fixture.Atoms {
				t.Fatalf("per-line evidence stopped writing a reference per atom: references=%d atoms=%d", fixture.References, fixture.Atoms)
			}
			if fixture.CoverageTargets != shape.options.CoverageTargets || fixture.MaxTargetReferences < fixture.Atoms/shape.options.CoverageTargets {
				t.Fatalf("per-line evidence stopped concentrating: targets=%d max-target-refs=%d atoms=%d",
					fixture.CoverageTargets, fixture.MaxTargetReferences, fixture.Atoms)
			}
			stepsByTargets[fixture.CoverageTargets] = steps
		})
	}

	for targets, steps := range stepsByTargets {
		spread, ok := stepsByTargets[targets*4]
		if !ok {
			continue
		}
		// A quarter of the targets own four times the selectors each and the
		// same atoms each scan them, so the sum is four times larger. Three is
		// the floor, leaving room for the half-selector the average scan stops
		// short of.
		if steps < 3*spread {
			t.Fatalf("selector scan cost stopped growing nonlinearly: %d targets cost %d steps against %d for %d targets",
				targets, steps, spread, targets*4)
		}
	}
	concentrated := stepsByTargets[largeSagaConcentrations[len(largeSagaConcentrations)-1]]
	if rangedAtoms == 0 || concentrated == 0 {
		t.Fatal("the sweep no longer contains both a ranged control and a concentrated per-line shape")
	}
	// Ranged authoring is the comparison the benchmark exists to make: the same
	// atoms, written as `cover --lines` would write them, cost two orders of
	// magnitude less to resolve.
	if concentrated < 10*rangedSteps {
		t.Fatalf("per-line evidence no longer costs materially more than ranged evidence: %d against %d steps over %d atoms",
			concentrated, rangedSteps, rangedAtoms)
	}
}

// formerSelectorScanSteps counts the iterations the removed linear selector
// scan would perform. It is a deterministic property of the saga and makes the
// adversarial fixture shape assertable without a wall-clock threshold.
func formerSelectorScanSteps(tb testing.TB, fixture testfixture.LargeSaga) int64 {
	tb.Helper()
	largeSagaFixtures.Lock()
	defer largeSagaFixtures.Unlock()
	if steps, ok := largeSagaFixtures.steps[fixture.Root]; ok {
		return steps
	}
	opened, ok := openLargeSaga(tb, fixture).(*session)
	if !ok {
		tb.Fatal("Open no longer returns the session this benchmark measures")
	}
	var steps int64
	for _, atom := range opened.changes.Atoms {
		for _, assignment := range opened.report.Ownership[atom.Key] {
			evidence := cleanDiagnosticPath(assignment.DiffFile)
			for _, entry := range opened.selectors[assignment.Target] {
				steps++
				if entry.selector.EvidenceFile == evidence && entry.diff == assignment.Diff {
					break
				}
			}
		}
	}
	if largeSagaFixtures.steps == nil {
		largeSagaFixtures.steps = map[string]int64{}
	}
	largeSagaFixtures.steps[fixture.Root] = steps
	return steps
}

func reportLargeSagaMetrics(b *testing.B, fixture testfixture.LargeSaga, steps int64) {
	b.Helper()
	b.ReportMetric(float64(fixture.Atoms), "atoms/op")
	b.ReportMetric(float64(fixture.References), "references/op")
	b.ReportMetric(float64(fixture.MaxTargetReferences), "max-target-refs/op")
	b.ReportMetric(float64(steps), "former-scan-steps/op")
	b.ReportMetric(float64(steps)/float64(fixture.Atoms), "former-scan-steps/atom")
}

func openLargeSaga(tb testing.TB, fixture testfixture.LargeSaga) Session {
	tb.Helper()
	session, err := Open(context.Background(), OpenOptions{SagaRoot: fixture.Root, SourceDir: fixture.Repository})
	if err != nil {
		tb.Fatal(err)
	}
	return session
}

func largeSagaInputs(tb testing.TB, fixture testfixture.LargeSaga) (*saga.Saga, gitdiff.ChangeSet, coverage.Report) {
	tb.Helper()
	document, validation, err := saga.Load(fixture.Root)
	if err != nil {
		tb.Fatal(err)
	}
	if !validation.Valid {
		tb.Fatalf("benchmark fixture is invalid: %#v", validation.Issues)
	}
	source := document.Manifest.Source
	changes, err := gitdiff.Read(context.Background(), fixture.Repository, source.Repository, source.Base, source.Head)
	if err != nil {
		tb.Fatal(err)
	}
	return document, changes, coverage.Evaluate(document, validation, changes)
}

// newLargeSagaSession mirrors the session value Open builds before it calls
// build. It exists so the construction can be timed without the load, the Git
// read and the coverage evaluation that precede it.
func newLargeSagaSession(document *saga.Saga, changes gitdiff.ChangeSet, report coverage.Report) *session {
	return &session{
		document: document, changes: changes, report: report,
		targets: map[string]*targetEntry{}, selectors: map[string][]selectorEntry{},
		selectorsByAtom: map[string][]DiffOwner{}, atomByURI: map[string]int{},
		fragments: map[string]fragmentValue{}, threads: map[string]ReviewThread{}, threadsByDiff: map[string][]ReviewThread{},
	}
}

// Fixture generation runs Git and writes thousands of files, and the testing
// package re-enters a benchmark body once per iteration count. Fixtures are
// therefore generated once per process and shared, which also lets the guard
// test above measure exactly what the benchmarks measure.
var largeSagaFixtures struct {
	sync.Mutex
	root  string
	built map[string]testfixture.LargeSaga
	steps map[string]int64
}

func largeSagaFixture(tb testing.TB, shape largeSagaShape) testfixture.LargeSaga {
	tb.Helper()
	largeSagaFixtures.Lock()
	defer largeSagaFixtures.Unlock()
	if fixture, ok := largeSagaFixtures.built[shape.name]; ok {
		return fixture
	}
	if largeSagaFixtures.root == "" {
		root, err := os.MkdirTemp("", "reviewapp-large-saga-")
		if err != nil {
			tb.Fatal(err)
		}
		largeSagaFixtures.root = root
	}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), filepath.Join(largeSagaFixtures.root, shape.name), shape.options)
	if err != nil {
		tb.Fatal(err)
	}
	if largeSagaFixtures.built == nil {
		largeSagaFixtures.built = map[string]testfixture.LargeSaga{}
	}
	largeSagaFixtures.built[shape.name] = fixture
	return fixture
}

func TestMain(m *testing.M) {
	code := m.Run()
	if largeSagaFixtures.root != "" {
		os.RemoveAll(largeSagaFixtures.root)
	}
	os.Exit(code)
}
