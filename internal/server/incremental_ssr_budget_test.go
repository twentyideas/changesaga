package server

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/testfixture"
)

// The incremental SSR contract is a construction boundary, not only a payload
// boundary. GET / may read the chapter outline and already-reduced summary
// metadata. It must not load the source comparison, evaluate coverage, build
// changesByTarget, or construct code, manifest, and file views that happen not
// to be rendered. Those products belong to the bounded async endpoints.
//
// This test instruments the external edge of all that work. gitdiff.Read must
// execute `git diff` before a ChangeSet can exist, so observing no such command
// is stronger and less timing-sensitive than putting a generous latency limit
// around the request. The shim exits immediately if the boundary regresses,
// keeping the failure deterministic instead of leaving a sleeping subprocess.
func TestRootShellDoesNotConstructTheSourceComparison(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the comparison command trap is a POSIX shell script")
	}
	fixture := newIncrementalSSRFixture(t, 1)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	trapDir := t.TempDir()
	marker := filepath.Join(trapDir, "comparison-started")
	shim := filepath.Join(trapDir, "git")
	script := fmt.Sprintf(`#!/bin/sh
for argument in "$@"; do
  if [ "$argument" = "diff" ]; then
    printf 'git diff was invoked\n' >> "$CHANGE_SAGA_COMPARISON_TRAP"
    exit 91
  fi
done
exec %q "$@"
`, realGit)
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHANGE_SAGA_COMPARISON_TRAP", marker)
	t.Setenv("PATH", trapDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	response := serveColdRoot(t, fixture)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("GET / started git diff; the root path constructed the ChangeSet/coverage graph instead of serving the bounded shell")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, eagerBody := range []string{`class="diff-row`, `class="manifest-range`, `data-code>`} {
		if strings.Contains(response.body, eagerBody) {
			t.Errorf("GET / rendered eager comparison content %q; code and coverage bodies belong to async endpoints", eagerBody)
		}
	}
}

const (
	// Adding diff records changes neither the chapter outline nor the summary
	// values rendered by this fixture. A few kilobytes allow unrelated header
	// formatting without permitting a record-by-record projection to return.
	rootDiffRecordByteAllowance = 4 << 10
	// Cold-root allocated bytes are stable enough for a slope budget, while wall
	// time is not. A 25% allowance absorbs fixed Git/process noise and remains
	// far below the roughly linear growth caused by loading and fingerprinting
	// every ___diffs record.
	rootDiffRecordAllocationGrowth = 1.25
	rootDiffRecordScale            = 1024
)

const (
	asyncDefaultPageSize = 50
	asyncMaxPageSize     = 200
	asyncWalkPageSize    = 2
	// The fixture exercises the largest permitted page on every async review
	// surface. This ceiling is intentionally generous for useful HTML while
	// remaining independent of the thousands of atoms behind the page.
	asyncMaxPageByteBudget = 750_000
	// An eightfold larger cached comparison must not materially change the work
	// required to construct the same two-item page. This leaves 50% for process
	// and template noise while rejecting global-project-then-slice builders.
	asyncProjectionAllocationGrowth = 1.50
)

// Every comparison-scale surface is reached after the root shell and must be a
// page, never the rest of the comparison disguised as one response. These
// black-box checks deliberately share no server pagination helper: they prove
// the public HTTP contract the browser depends on.
func TestAsyncReviewSurfacesAreBoundedAndCursorPaginated(t *testing.T) {
	options := testfixture.LargeSagaOptions{
		Chapters: 4, SectionsPerChapter: 1, FragmentsPerSection: 1,
		SourceFiles: 16, ChangedLinesPerFile: 64,
		ReviewsPerFragment: 0, Threads: 0, DiffReviews: 0,
		CoverageRangeWidth: 1, CoverageTargets: 4,
	}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), t.TempDir(), options)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	handler := newMux(&app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl})
	file := url.QueryEscape("src/component-000.txt")
	surfaces := []struct {
		name string
		path string
	}{
		{name: "code files", path: "/api/code"},
		{name: "coverage by code", path: "/api/coverage?mode=code"},
		{name: "coverage by saga", path: "/api/coverage?mode=saga"},
		{name: "file diff", path: "/api/file-diff?file=" + file},
	}

	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			defaultPage := incrementalRequest(t, handler, surface.path)
			explicitDefault := incrementalRequest(t, handler, queryWith(surface.path, "limit", fmt.Sprint(asyncDefaultPageSize)))
			got, want := returnedItems(t, defaultPage), returnedItems(t, explicitDefault)
			if got < 1 || got > asyncDefaultPageSize {
				t.Fatalf("omitted limit returned %d items, want 1..%d", got, asyncDefaultPageSize)
			}
			if got != want {
				t.Fatalf("omitted limit returned %d items, explicit default %d returned %d", got, asyncDefaultPageSize, want)
			}

			maxPage := incrementalRequest(t, handler, queryWith(surface.path, "limit", fmt.Sprint(asyncMaxPageSize)))
			oversized := incrementalRequest(t, handler, queryWith(surface.path, "limit", fmt.Sprint(asyncMaxPageSize*100)))
			maxReturned, oversizedReturned := returnedItems(t, maxPage), returnedItems(t, oversized)
			if oversizedReturned != maxReturned || oversizedReturned > asyncMaxPageSize {
				t.Fatalf("an oversized limit was not capped at %d; cap returned %d items, oversized request returned %d", asyncMaxPageSize, maxReturned, oversizedReturned)
			}
			if len(maxPage) > asyncMaxPageByteBudget {
				t.Fatalf("largest async page exceeded its byte budget: %d > %d B; total atom/coverage scale leaked into one response", len(maxPage), asyncMaxPageByteBudget)
			}

			first := incrementalRequest(t, handler, queryWith(surface.path, "limit", "1"))
			if returned := returnedItems(t, first); returned != 1 {
				t.Fatalf("limit=1 returned %d items", returned)
			}
			cursor := nextCursor(t, first)
			if cursor == "" {
				t.Fatal("limit=1 returned no continuation for a surface with multiple results")
			}
			second := incrementalRequest(t, handler, queryWith(queryWith(surface.path, "limit", "1"), "cursor", cursor))
			if returned := returnedItems(t, second); returned != 1 {
				t.Fatalf("second limit=1 page returned %d items", returned)
			}
			if second == first {
				t.Fatal("the continuation repeated the first page; cursor was ignored")
			}

			cursor = ""
			seenCursors := map[string]bool{}
			pages, totalReturned := 0, 0
			for {
				path := queryWith(surface.path, "limit", fmt.Sprint(asyncWalkPageSize))
				if cursor != "" {
					path = queryWith(path, "cursor", cursor)
				}
				body := incrementalRequest(t, handler, path)
				returned := returnedItems(t, body)
				if returned < 1 || returned > asyncWalkPageSize {
					t.Fatalf("page %d returned %d items with limit %d", pages+1, returned, asyncWalkPageSize)
				}
				totalReturned += returned
				pages++
				next := nextCursor(t, body)
				if next == "" {
					break
				}
				if seenCursors[next] {
					t.Fatalf("cursor %q repeated before exhaustion", next)
				}
				seenCursors[next] = true
				cursor = next
				if pages > 4096 {
					t.Fatal("pagination did not reach exhaustion within the fixture's result count")
				}
			}
			if pages < 2 || totalReturned < 2 {
				t.Fatal("pagination never advanced")
			}
			t.Logf("%s: %d items across %d pages at limit %d; max page %d B", surface.name, totalReturned, pages, asyncWalkPageSize, len(maxPage))
		})
	}
}

// Pagination must bound server work as well as markup. A handler that calls
// makeCodeReviewView, makeCoverageManifestView, or makeFileViews over the whole
// cached comparison and slices afterward can satisfy every response-byte check
// above while still allocating O(total atoms + ownership edges) per page.
//
// The comparison snapshot is warmed before measurement on purpose. Building a
// ChangeSet and evaluating coverage is the async preparation cost; this test
// isolates the request-local projections whose work should be O(page size) (or
// O(the one requested file page)), invariant to everything outside that page.
func TestAsyncPageConstructionIsInvariantToComparisonScale(t *testing.T) {
	base := newAsyncProjectionFixture(t, 8)
	large := newAsyncProjectionFixture(t, 64)
	file := url.QueryEscape("src/component-000.txt")
	surfaces := []struct {
		name string
		path string
	}{
		{name: "code", path: "/api/code?file=" + file + "&limit=2"},
		{name: "coverage by code", path: "/api/coverage?mode=code&limit=2"},
		{name: "coverage by saga", path: "/api/coverage?mode=saga&limit=2"},
		{name: "file diff", path: "/api/file-diff?file=" + file + "&limit=2"},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			baseMeasurement := measureAsyncProjection(t, base.handler, surface.path)
			largeMeasurement := measureAsyncProjection(t, large.handler, surface.path)
			if baseMeasurement.returned != largeMeasurement.returned {
				t.Fatalf("fixed page returned %d items at base scale and %d at large scale", baseMeasurement.returned, largeMeasurement.returned)
			}
			if baseMeasurement.allocatedBytes == 0 {
				t.Fatal("async page reported zero allocated bytes")
			}
			growth := float64(largeMeasurement.allocatedBytes) / float64(baseMeasurement.allocatedBytes)
			if growth > asyncProjectionAllocationGrowth {
				t.Fatalf("constructing the same %d-item page allocated %.2fx more for an 8x larger comparison: %d -> %d B (budget %.2fx); the handler projected the whole comparison before slicing",
					baseMeasurement.returned, growth, baseMeasurement.allocatedBytes, largeMeasurement.allocatedBytes, asyncProjectionAllocationGrowth)
			}
			t.Logf("fixed %d-item page over %d -> %d atoms: %d -> %d B allocated (%.2fx), response %d -> %d B",
				baseMeasurement.returned, base.atoms, large.atoms, baseMeasurement.allocatedBytes, largeMeasurement.allocatedBytes, growth,
				baseMeasurement.bodyBytes, largeMeasurement.bodyBytes)
		})
	}
}

// This is the scale half of TestRootShellDoesNotConstructTheSourceComparison.
// Both fixtures have the same saga title, four chapter summaries, four fragment
// descriptors, review state, source comparison, and coverage totals. The large
// fixture merely repeats one valid DiffReference in independent ___diffs files.
// Root construction is therefore O(chapters + summary metadata) only when its
// response and allocations stay invariant to that multiplication.
//
// This deliberately measures a cold request. Warming reviewSnapshot first, as
// the historical first-load budgets do, hides both regressions this contract is
// meant to catch: saga.Load parsing every reference and treeFingerprint walking
// every record before the root can answer.
func TestColdRootShellIsInvariantToDiffReferenceRecordCount(t *testing.T) {
	baseFixture := newIncrementalSSRFixture(t, 1)
	largeFixture := newIncrementalSSRFixture(t, rootDiffRecordScale)

	base := serveColdRoot(t, baseFixture)
	large := serveColdRoot(t, largeFixture)
	if large.bodyBytes > base.bodyBytes+rootDiffRecordByteAllowance {
		t.Fatalf("cold GET / response grew with ___diffs records: %d records produced %d B, %d records produced %d B (allowance %d B); root may carry chapter/summary metadata, not coverage or code projections",
			baseFixture.diffRecords, base.bodyBytes, largeFixture.diffRecords, large.bodyBytes, rootDiffRecordByteAllowance)
	}
	if base.allocatedBytes == 0 {
		t.Fatal("cold GET / reported zero allocated bytes")
	}
	growth := float64(large.allocatedBytes) / float64(base.allocatedBytes)
	if growth > rootDiffRecordAllocationGrowth {
		t.Fatalf("cold GET / allocated bytes grew %.2fx with ___diffs records: %d records allocated %d B, %d records allocated %d B (budget %.2fx); the root still loads/fingerprints detailed evidence or constructs atom-level views",
			growth, baseFixture.diffRecords, base.allocatedBytes, largeFixture.diffRecords, large.allocatedBytes, rootDiffRecordAllocationGrowth)
	}
	t.Logf("cold root: %d -> %d diff records | response %d -> %d B | allocated %d -> %d B (%.2fx)",
		baseFixture.diffRecords, largeFixture.diffRecords, base.bodyBytes, large.bodyBytes, base.allocatedBytes, large.allocatedBytes, growth)
}

func BenchmarkColdRootShellByDiffReferenceRecords(b *testing.B) {
	for _, records := range []int{1, 128, rootDiffRecordScale} {
		fixture := newIncrementalSSRFixture(b, records)
		b.Run(fmt.Sprintf("records_%04d", records), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(records), "diff-records")
			for b.Loop() {
				measurement := serveColdRoot(b, fixture)
				b.ReportMetric(float64(measurement.bodyBytes), "response-B")
			}
		})
	}
}

type incrementalSSRFixture struct {
	root        string
	repository  string
	diffRecords int
}

type coldRootMeasurement struct {
	body           string
	bodyBytes      int
	allocatedBytes uint64
}

type asyncProjectionFixture struct {
	handler http.Handler
	atoms   int
}

type asyncProjectionMeasurement struct {
	allocatedBytes uint64
	bodyBytes      int
	returned       int
}

func newIncrementalSSRFixture(tb testing.TB, diffRecords int) incrementalSSRFixture {
	tb.Helper()
	options := testfixture.LargeSagaOptions{
		Chapters: 4, SectionsPerChapter: 1, FragmentsPerSection: 1,
		SourceFiles: 1, ChangedLinesPerFile: 2,
		ReviewsPerFragment: 0, Threads: 0, DiffReviews: 0,
		CoverageRangeWidth: 4, CoverageTargets: 1,
	}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatal(err)
	}
	var original string
	err = filepath.WalkDir(fixture.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Base(path) == "coverage.json" && filepath.Base(filepath.Dir(path)) == "___diffs" {
			original = path
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	if original == "" {
		tb.Fatal("fixture generated no ___diffs/coverage.json record")
	}
	data, err := os.ReadFile(original)
	if err != nil {
		tb.Fatal(err)
	}
	for index := 1; index < diffRecords; index++ {
		path := filepath.Join(filepath.Dir(original), fmt.Sprintf("repeated-%05d.json", index))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return incrementalSSRFixture{root: fixture.Root, repository: fixture.Repository, diffRecords: diffRecords}
}

func serveColdRoot(tb testing.TB, fixture incrementalSSRFixture) coldRootMeasurement {
	tb.Helper()
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}
	application := &app{root: fixture.root, sourceDir: fixture.repository, template: tmpl}
	handler := newMux(application)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	handler.ServeHTTP(recorder, request)
	runtime.ReadMemStats(&after)
	if recorder.Code != http.StatusOK {
		tb.Fatalf("cold GET / = %d: %s", recorder.Code, firstLine(recorder.Body.String()))
	}
	body := recorder.Body.String()
	return coldRootMeasurement{body: body, bodyBytes: len(body), allocatedBytes: after.TotalAlloc - before.TotalAlloc}
}

func newAsyncProjectionFixture(tb testing.TB, sourceFiles int) asyncProjectionFixture {
	tb.Helper()
	options := testfixture.LargeSagaOptions{
		Chapters: 4, SectionsPerChapter: 1, FragmentsPerSection: 1,
		SourceFiles: sourceFiles, ChangedLinesPerFile: 16,
		ReviewsPerFragment: 0, Threads: 0, DiffReviews: 0,
		CoverageRangeWidth: 1, CoverageTargets: 4,
	}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		tb.Fatal(err)
	}
	application := &app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl}
	snapshot := application.snapshot(context.Background())
	if snapshot == nil || snapshot.diffErr != nil {
		tb.Fatal("could not warm async projection fixture comparison")
	}
	if !snapshot.report.Complete || len(snapshot.changes.Atoms) != fixture.Atoms {
		tb.Fatalf("async projection fixture is not complete: atoms=%d/%d summary=%#v", len(snapshot.changes.Atoms), fixture.Atoms, snapshot.report.Summary)
	}
	return asyncProjectionFixture{handler: newMux(application), atoms: fixture.Atoms}
}

func measureAsyncProjection(tb testing.TB, handler http.Handler, path string) asyncProjectionMeasurement {
	tb.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	body := incrementalRequest(tb, handler, path)
	runtime.ReadMemStats(&after)
	return asyncProjectionMeasurement{
		allocatedBytes: after.TotalAlloc - before.TotalAlloc,
		bodyBytes:      len(body),
		returned:       returnedItems(tb, body),
	}
}

func incrementalRequest(tb testing.TB, handler http.Handler, path string) string {
	tb.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		tb.Fatalf("GET %s = %d: %s", path, recorder.Code, firstLine(recorder.Body.String()))
	}
	return recorder.Body.String()
}

func queryWith(path, key, value string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

func nextCursor(tb testing.TB, body string) string {
	tb.Helper()
	const marker = `data-next-cursor="`
	start := strings.Index(body, marker)
	if start < 0 {
		tb.Fatal("async response root carries no data-next-cursor continuation metadata")
	}
	rest := body[start+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		tb.Fatal("async response has an unterminated data-next-cursor attribute")
	}
	return stdhtml.UnescapeString(rest[:end])
}

func returnedItems(tb testing.TB, body string) int {
	tb.Helper()
	if !strings.Contains(body, "data-page-items") {
		tb.Fatal("async response carries no data-page-items container")
	}
	const marker = `data-returned="`
	start := strings.Index(body, marker)
	if start < 0 {
		tb.Fatal("async response root carries no data-returned item count")
	}
	rest := body[start+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		tb.Fatal("async response has an unterminated data-returned attribute")
	}
	value, err := strconv.Atoi(rest[:end])
	if err != nil {
		tb.Fatalf("async response data-returned=%q: %v", rest[:end], err)
	}
	return value
}
