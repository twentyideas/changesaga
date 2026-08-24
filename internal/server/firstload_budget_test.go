package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

// First load is a bounded shell. It may state comparison totals, but every
// diff row and every coverage row belongs to an async review endpoint. A small
// response is not sufficient proof: constructing the complete ChangeSet and
// coverage projection before discarding them still makes GET / scale with a
// review the browser has not asked to see.
//
// These fixtures isolate the two inputs that caused the Daylight failure:
//
//	base          8 files x  32 changed lines,   512 atoms,  64 ranges
//	atom-growth   8 files x 256 changed lines, 4,096 atoms,  64 ranges
//	range-growth  8 files x  32 changed lines,   512 atoms, 512 ranges
//
// The story, file count, and review overlay stay fixed. Atom-growth widens its
// authored ranges in step with the code; range-growth explains the same code
// one line at a time. The fixtures are deliberately smaller than the benchmark
// fixture so the asserted contract remains practical for ordinary CI. The
// opt-in Daylight harness at the end of this file exercises roughly 533,000
// atoms and is documented in docs/performance.md.

const (
	firstLoadScaleFactor = 8

	// Response bytes and materialized HTML nodes should move only by a few
	// digits in the totals. The additive allowance prevents tiny markup changes
	// from making a ratio noisy while still rejecting a per-atom/range surface.
	rootResponseGrowthBudget = 1.10
	rootResponseGrowthSlack  = 8 * 1024
	rootNodeGrowthBudget     = 1.05
	rootNodeGrowthSlack      = 16

	// Wall time is the median of three cold root renders over one generated
	// fixture. Ratio plus additive slack accommodates shared runners; the fixed
	// smoke ceiling catches a uniformly pathological render.
	rootWallGrowthBudget = 3.0
	rootWallGrowthSlack  = 50 * time.Millisecond
	rootWallCeiling      = 2 * time.Second

	// After two collections, a scaled root may retain a little allocator and
	// summary overhead, but not the scaled ChangeSet or coverage projection.
	rootRetainedGrowthBudget = 1.50
	rootRetainedGrowthSlack  = 2 * 1024 * 1024
	rootRetainedCeiling      = 32 * 1024 * 1024

	mutationWallCeiling             = 2 * time.Second
	reviewPageAllocationGrowth      = 1.50
	reviewPageAllocationGrowthSlack = 512 * 1024

	daylightScaleEnvironment = "CHANGE_SAGA_DAYLIGHT_SCALE"
)

type firstLoadShape struct {
	name    string
	fixture testfixture.LargeSaga

	responseBytes int64
	nodes         int64
	diffRows      int
	coverageRows  int
	wall          time.Duration
	retained      int64
	fullBuilds    int64
}

func ciFirstLoadOptions() testfixture.LargeSagaOptions {
	return testfixture.LargeSagaOptions{
		Chapters:            3,
		SectionsPerChapter:  2,
		FragmentsPerSection: 2,
		SourceFiles:         8,
		ChangedLinesPerFile: 32,
		ReviewsPerFragment:  1,
		Threads:             4,
		DiffReviews:         4,
		CoverageRangeWidth:  8,
	}
}

func atomGrowthFirstLoadOptions() testfixture.LargeSagaOptions {
	options := ciFirstLoadOptions()
	options.ChangedLinesPerFile *= firstLoadScaleFactor
	options.CoverageRangeWidth *= firstLoadScaleFactor
	return options
}

func rangeGrowthFirstLoadOptions() testfixture.LargeSagaOptions {
	options := ciFirstLoadOptions()
	options.CoverageRangeWidth = 1
	return options
}

func TestRootFirstLoadStaysBoundedAsAtomsAndRangesGrow(t *testing.T) {
	base := measureRootFirstLoad(t, "base", ciFirstLoadOptions())
	atoms := measureRootFirstLoad(t, "atom-growth", atomGrowthFirstLoadOptions())
	ranges := measureRootFirstLoad(t, "range-growth", rangeGrowthFirstLoadOptions())
	requireIsolatedScaleAxes(t, base, atoms, ranges)

	for _, shape := range []firstLoadShape{base, atoms, ranges} {
		t.Run(shape.name+"_is_a_shell", func(t *testing.T) {
			requireBoundedRoot(t, shape)
		})
	}

	for _, shape := range []firstLoadShape{atoms, ranges} {
		t.Run(shape.name+"_growth", func(t *testing.T) {
			checkBoundedGrowth(t, "root response bytes", base, shape, base.responseBytes, shape.responseBytes,
				rootResponseGrowthBudget, rootResponseGrowthSlack)
			checkBoundedGrowth(t, "root materialized nodes", base, shape, base.nodes, shape.nodes,
				rootNodeGrowthBudget, rootNodeGrowthSlack)
			checkBoundedGrowth(t, "cold root wall time", base, shape, int64(base.wall), int64(shape.wall),
				rootWallGrowthBudget, int64(rootWallGrowthSlack))
			checkBoundedGrowth(t, "retained heap after root", base, shape, base.retained, shape.retained,
				rootRetainedGrowthBudget, rootRetainedGrowthSlack)
		})
	}
}

type reviewPageAllocations struct {
	name         string
	fixture      testfixture.LargeSaga
	code         int64
	coverageCode int64
	coverageSaga int64
}

// TestPaginatedReviewEndpointAllocationsStayBoundedAcrossScale is the endpoint
// half of incremental SSR. A response can contain only 50 rows and still build
// an eager all-atom projection behind them, so it budgets bytes allocated by a
// warm page request after the detailed comparison generation has been cached.
func TestPaginatedReviewEndpointAllocationsStayBoundedAcrossScale(t *testing.T) {
	base := measureReviewPageAllocations(t, "base", ciFirstLoadOptions())
	atoms := measureReviewPageAllocations(t, "atom-growth", atomGrowthFirstLoadOptions())
	ranges := measureReviewPageAllocations(t, "range-growth", rangeGrowthFirstLoadOptions())
	if atoms.fixture.Atoms != firstLoadScaleFactor*base.fixture.Atoms || atoms.fixture.References != base.fixture.References ||
		ranges.fixture.Atoms != base.fixture.Atoms || ranges.fixture.References != firstLoadScaleFactor*base.fixture.References {
		t.Fatalf("review-page fixtures no longer isolate atom and range growth: base=%d/%d, atoms=%d/%d, ranges=%d/%d",
			base.fixture.Atoms, base.fixture.References, atoms.fixture.Atoms, atoms.fixture.References,
			ranges.fixture.Atoms, ranges.fixture.References)
	}

	for _, grown := range []reviewPageAllocations{atoms, ranges} {
		context := fmt.Sprintf("%s -> %s (%d -> %d atoms, %d -> %d ranges)",
			base.name, grown.name, base.fixture.Atoms, grown.fixture.Atoms, base.fixture.References, grown.fixture.References)
		for _, metric := range []struct {
			name          string
			before, after int64
		}{
			{"code page allocated bytes", base.code, grown.code},
			{"code-first coverage page allocated bytes", base.coverageCode, grown.coverageCode},
			{"saga-first coverage page allocated bytes", base.coverageSaga, grown.coverageSaga},
		} {
			checkSimpleGrowthWithContext(t, metric.name, metric.before, metric.after,
				reviewPageAllocationGrowth, reviewPageAllocationGrowthSlack, context)
		}
	}
}

func measureReviewPageAllocations(tb testing.TB, name string, options testfixture.LargeSagaOptions) reviewPageAllocations {
	tb.Helper()
	server := newRootTestServer(tb, tb.TempDir(), options)
	shape := reviewPageAllocations{name: name, fixture: server.fixture}

	measure := func(path string) int64 {
		tb.Helper()
		requirePagedReviewResponse(tb, path, server.handler)
		values := make([]int64, 0, 3)
		for sample := 0; sample < 3; sample++ {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			requirePagedReviewResponse(tb, path, server.handler)
			runtime.ReadMemStats(&after)
			values = append(values, int64(after.TotalAlloc-before.TotalAlloc))
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		return values[len(values)/2]
	}

	shape.code = measure("/api/code?limit=50")
	shape.coverageCode = measure("/api/coverage?mode=code&limit=50")
	shape.coverageSaga = measure("/api/coverage?mode=saga&limit=50")
	if builds := server.application.cache.builds; builds != 1 {
		tb.Fatalf("%s paginated review requests built %d detailed comparison generations, want one cached generation", name, builds)
	}
	for _, path := range []string{"/api/code?limit=201", "/api/coverage?mode=code&limit=201"} {
		recorder := httptest.NewRecorder()
		server.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			tb.Errorf("GET %s = %d, want %d for the hard page limit", path, recorder.Code, http.StatusBadRequest)
		}
	}
	tb.Logf("%s review pages: code=%d B allocated, coverage/code=%d B, coverage/saga=%d B",
		name, shape.code, shape.coverageCode, shape.coverageSaga)
	return shape
}

func requirePagedReviewResponse(tb testing.TB, path string, handler *http.ServeMux) {
	tb.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		tb.Fatalf("GET %s = %d: %s", path, recorder.Code, firstLine(recorder.Body.String()))
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `data-returned="50"`) || !strings.Contains(body, "data-next-cursor=") {
		tb.Fatalf("GET %s did not return a 50-row cursor page", path)
	}
	if recorder.Header().Get("X-Change-Saga-Next-Cursor") == "" {
		tb.Fatalf("GET %s omitted its continuation header", path)
	}
}

func requireIsolatedScaleAxes(t *testing.T, base, atoms, ranges firstLoadShape) {
	t.Helper()
	if atoms.fixture.Atoms != firstLoadScaleFactor*base.fixture.Atoms || atoms.fixture.References != base.fixture.References {
		t.Fatalf("atom-growth did not isolate atoms: base=%d atoms/%d ranges, grown=%d atoms/%d ranges",
			base.fixture.Atoms, base.fixture.References, atoms.fixture.Atoms, atoms.fixture.References)
	}
	if ranges.fixture.Atoms != base.fixture.Atoms || ranges.fixture.References != firstLoadScaleFactor*base.fixture.References {
		t.Fatalf("range-growth did not isolate ranges: base=%d atoms/%d ranges, grown=%d atoms/%d ranges",
			base.fixture.Atoms, base.fixture.References, ranges.fixture.Atoms, ranges.fixture.References)
	}
	for _, shape := range []firstLoadShape{base, atoms, ranges} {
		if shape.fixture.Mappings != shape.fixture.Atoms {
			t.Fatalf("%s fixture maps %d of %d atoms; a partially covered fixture would make the root artificially cheap",
				shape.name, shape.fixture.Mappings, shape.fixture.Atoms)
		}
	}
}

func requireBoundedRoot(t *testing.T, shape firstLoadShape) {
	t.Helper()
	if shape.diffRows != 0 {
		t.Errorf("%s root materialized %d diff rows; diff rows belong to an async review endpoint", shape.name, shape.diffRows)
	}
	if shape.coverageRows != 0 {
		t.Errorf("%s root materialized %d coverage rows; coverage rows belong to an async review endpoint", shape.name, shape.coverageRows)
	}
	if shape.fullBuilds != 0 {
		t.Errorf("%s root built the full comparison/coverage model %d times; a small response cannot hide eager server work",
			shape.name, shape.fullBuilds)
	}
	if shape.wall > rootWallCeiling {
		t.Errorf("%s cold root wall time exceeded its smoke ceiling: %s > %s", shape.name, shape.wall, rootWallCeiling)
	}
	if shape.retained > rootRetainedCeiling {
		t.Errorf("%s root retained %d B, budget %d B; the shell must not retain the full comparison/coverage model",
			shape.name, shape.retained, rootRetainedCeiling)
	}
}

// TestRootMutationStaysBoundedAndIsolated proves that the write path does not
// smuggle review-surface construction back into the following root request and
// does not perturb a separate served saga. It intentionally uses two app
// instances: cache generations and mutation visibility are per saga, not
// process-global.
func TestRootMutationStaysBoundedAndIsolated(t *testing.T) {
	first := newRootTestServer(t, t.TempDir(), ciFirstLoadOptions())
	peer := newRootTestServer(t, t.TempDir(), ciFirstLoadOptions())

	before := timedRootRequest(t, first.handler)
	peerBefore := timedRootRequest(t, peer.handler)
	requireRootMarkup(t, "mutation baseline", before.body)
	requireRootMarkup(t, "peer baseline", peerBefore.body)

	values := url.Values{
		"target": {saga.SagaTarget("large-benchmark")},
		"state":  {"approved"},
		"body":   {"Bounded mutation decision."},
	}
	started := time.Now()
	request := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	first.handler.ServeHTTP(recorder, request)
	mutationWall := time.Since(started)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("review mutation = %d: %s", recorder.Code, firstLine(recorder.Body.String()))
	}
	if mutationWall > mutationWallCeiling {
		t.Errorf("review mutation exceeded its smoke ceiling: %s > %s", mutationWall, mutationWallCeiling)
	}

	after := timedRootRequest(t, first.handler)
	peerAfter := timedRootRequest(t, peer.handler)
	requireRootMarkup(t, "root after mutation", after.body)
	if !strings.Contains(after.body, "Bounded mutation decision.") {
		t.Error("the bounded root did not expose the decision written immediately before it")
	}
	if peerAfter.body != peerBefore.body || strings.Contains(peerAfter.body, "Bounded mutation decision.") {
		t.Error("mutating one saga changed the root response of a separate served saga")
	}
	if builds := first.application.cache.builds; builds != 0 {
		t.Errorf("mutation plus following root built the full comparison/coverage model %d times", builds)
	}
	if builds := peer.application.cache.builds; builds != 0 {
		t.Errorf("unmodified peer root built the full comparison/coverage model %d times", builds)
	}
	checkMutationEnvelope(t, before, after)
	t.Logf("mutation: %s; following root: %s, %d B, %d nodes; peer remained byte-identical",
		mutationWall, after.wall, len(after.body), rootNodeCount(after.body))
}

type rootTestServer struct {
	handler     *http.ServeMux
	application *app
	fixture     testfixture.LargeSaga
}

func newRootTestServer(tb testing.TB, parent string, options testfixture.LargeSagaOptions) rootTestServer {
	tb.Helper()
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), parent, options)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}
	application := &app{
		root: fixture.Root, sourceDir: fixture.Repository, template: tmpl,
	}
	return rootTestServer{handler: newMux(application), application: application, fixture: fixture}
}

type timedResponse struct {
	body string
	wall time.Duration
}

func timedRootRequest(tb testing.TB, handler *http.ServeMux) timedResponse {
	tb.Helper()
	started := time.Now()
	body := firstLoadPage(tb, handler)
	return timedResponse{body: body, wall: time.Since(started)}
}

func checkMutationEnvelope(t *testing.T, before, after timedResponse) {
	t.Helper()
	checkSimpleGrowth(t, "root response bytes after mutation", int64(len(before.body)), int64(len(after.body)),
		rootResponseGrowthBudget, rootResponseGrowthSlack)
	checkSimpleGrowth(t, "root materialized nodes after mutation", rootNodeCount(before.body), rootNodeCount(after.body),
		rootNodeGrowthBudget, rootNodeGrowthSlack)
	limit := time.Duration(float64(before.wall)*rootWallGrowthBudget) + rootWallGrowthSlack
	if after.wall > limit || after.wall > rootWallCeiling {
		t.Errorf("root wall time after mutation grew from %s to %s, budget %s and ceiling %s",
			before.wall, after.wall, limit, rootWallCeiling)
	}
}

// TestDaylightRootFirstLoadScale is an opt-in local harness, never ordinary CI.
// The generated comparison closely matches the diagnosed 532,290-atom shape:
// 2,666 files with 100 replaced lines produce 533,200 old/new line atoms, all
// authored as single-line references.
func TestDaylightRootFirstLoadScale(t *testing.T) {
	if os.Getenv(daylightScaleEnvironment) != "1" {
		t.Skip("set " + daylightScaleEnvironment + "=1 to run the Daylight-scale root harness")
	}
	options := testfixture.DefaultLargeSagaOptions()
	options.SourceFiles = 2_666
	options.ChangedLinesPerFile = 100
	options.CoverageRangeWidth = 1
	options.CoverageTargets = 8
	options.ReviewsPerFragment = 0
	options.Threads = 0
	options.DiffReviews = 0

	shape := measureRootFirstLoad(t, "daylight", options)
	requireBoundedRoot(t, shape)
	t.Logf("DAYLIGHT ROOT: %d atoms, %d ranges | %d B, %d nodes, %s median cold wall, %d B retained, %d full builds",
		shape.fixture.Atoms, shape.fixture.References, shape.responseBytes, shape.nodes, shape.wall, shape.retained, shape.fullBuilds)
}

// BenchmarkRootFirstLoadScale is the diagnostic companion to the asserted
// suite. Fixture construction stays outside the timed region.
func BenchmarkRootFirstLoadScale(b *testing.B) {
	shapes := []struct {
		name    string
		options testfixture.LargeSagaOptions
	}{
		{"base", ciFirstLoadOptions()},
		{"atom_growth", atomGrowthFirstLoadOptions()},
		{"range_growth", rangeGrowthFirstLoadOptions()},
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
				b.ReportMetric(float64(rootNodeCount(recorder.Body.String())), "nodes")
			}
		})
	}
}

func measureRootFirstLoad(tb testing.TB, name string, options testfixture.LargeSagaOptions) firstLoadShape {
	tb.Helper()
	shape := firstLoadShape{name: name}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatal(err)
	}
	shape.fixture = fixture
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}

	runtime.GC()
	runtime.GC()
	var idle runtime.MemStats
	runtime.ReadMemStats(&idle)

	walls := make([]time.Duration, 0, 3)
	var handler *http.ServeMux
	var page string
	for sample := 0; sample < 3; sample++ {
		application := &app{
			root: fixture.Root, sourceDir: fixture.Repository, template: tmpl,
		}
		handler = newMux(application)
		started := time.Now()
		page = firstLoadPage(tb, handler)
		walls = append(walls, time.Since(started))
		shape.fullBuilds += int64(application.cache.builds)
	}
	shape.wall = medianDuration(walls)

	runtime.GC()
	runtime.GC()
	var held runtime.MemStats
	runtime.ReadMemStats(&held)
	if held.HeapAlloc > idle.HeapAlloc {
		shape.retained = int64(held.HeapAlloc - idle.HeapAlloc)
	}

	shape.responseBytes = int64(len(page))
	shape.nodes = rootNodeCount(page)
	shape.diffRows = strings.Count(page, `class="diff-row`)
	shape.coverageRows = strings.Count(page, `class="manifest-range"`)
	tb.Logf("%s: %d atoms, %d ranges | %d B, %d nodes, %d diff rows, %d coverage rows | %s median cold wall, %d B retained, %d full builds",
		name, fixture.Atoms, fixture.References, shape.responseBytes, shape.nodes, shape.diffRows, shape.coverageRows,
		shape.wall, shape.retained, shape.fullBuilds)
	runtime.KeepAlive(handler)
	return shape
}

func requireRootMarkup(tb testing.TB, name, page string) {
	tb.Helper()
	if rows := strings.Count(page, `class="diff-row`); rows != 0 {
		tb.Errorf("%s materialized %d diff rows", name, rows)
	}
	if rows := strings.Count(page, `class="manifest-range"`); rows != 0 {
		tb.Errorf("%s materialized %d coverage rows", name, rows)
	}
}

// rootNodeCount counts opening tags in the response. It deliberately avoids a
// browser dependency while measuring the nodes an HTML parser materializes.
func rootNodeCount(page string) int64 {
	var nodes int64
	for offset := 0; offset < len(page); offset++ {
		if page[offset] != '<' || offset+1 == len(page) {
			continue
		}
		next := page[offset+1]
		if next >= 'A' && next <= 'Z' || next >= 'a' && next <= 'z' {
			nodes++
		}
	}
	return nodes
}

func medianDuration(values []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

func checkBoundedGrowth(tb testing.TB, metric string, base, grown firstLoadShape, before, after int64, ratio float64, slack int64) {
	tb.Helper()
	context := fmt.Sprintf("%s -> %s (%d -> %d atoms, %d -> %d ranges)",
		base.name, grown.name, base.fixture.Atoms, grown.fixture.Atoms, base.fixture.References, grown.fixture.References)
	checkSimpleGrowthWithContext(tb, metric, before, after, ratio, slack, context)
}

func checkSimpleGrowth(tb testing.TB, metric string, before, after int64, ratio float64, slack int64) {
	tb.Helper()
	checkSimpleGrowthWithContext(tb, metric, before, after, ratio, slack, "")
}

func checkSimpleGrowthWithContext(tb testing.TB, metric string, before, after int64, ratio float64, slack int64, context string) {
	tb.Helper()
	if before < 0 || after < 0 {
		tb.Fatalf("%s produced a negative measurement: %d -> %d", metric, before, after)
	}
	limit := int64(float64(before)*ratio) + slack
	if after > limit {
		tb.Fatalf("%s is not bounded: %d -> %d, limit %d (%.2fx + %d)%s\n  Incremental SSR must not eagerly materialize the full comparison or coverage audit. See docs/performance.md before raising this budget.",
			metric, before, after, limit, ratio, slack, formatGrowthContext(context))
	}
	tb.Logf("%s: %d -> %d, limit %d%s", metric, before, after, limit, formatGrowthContext(context))
}

func formatGrowthContext(value string) string {
	if value == "" {
		return ""
	}
	return " [" + value + "]"
}

func newFirstLoadHandler(tb testing.TB, options testfixture.LargeSagaOptions) *http.ServeMux {
	tb.Helper()
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}
	handler := newMux(&app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl})
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
