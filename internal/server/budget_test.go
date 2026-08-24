package server

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

// Budgets guard one architectural rule: the first load describes the whole
// comparison, and carries only the part of it a reviewer is already looking at.
// Every diff body beyond the selected file arrives from /api/file-diff when the
// reviewer opens that file.
//
// The numbers below are hard ceilings over a deterministic fixture, so they are
// exact and stable rather than timing-sensitive. Wall-clock and allocation
// figures are diagnostic, are reported by the benchmarks in this package, and
// are recorded in docs/performance.md; they are deliberately not asserted,
// because CI machines vary far more than any real regression would.
//
// Measured on the fixture these budgets use (8 chapters, 145 fragments, 144
// changed files, 4,096 changed lines, 4,096 exact mappings):
//
//	first-load HTML     45,409,451 B -> 5,260,620 B
//	HTML elements        1,869,455   ->   145,103
//	diff rows in page      139,392   ->       128
//
// A budget should only be raised when the page genuinely gained content a
// reviewer sees on first load. Growth that tracks the size of the comparison
// means a diff body has been inlined again, and is the regression these tests
// exist to catch.
const (
	// firstLoadHTMLBudget covers the navigation outline, the coverage audit, and
	// one selected file diff, with room for ordinary content growth. The saga
	// document itself is now a shell, so this no longer covers the story.
	firstLoadHTMLBudget = 3_400_000
	// firstLoadElementBudget keeps browser parse, layout, and memory bounded.
	// The baseline needed 1.87 million elements for the same fixture.
	firstLoadElementBudget = 90_000
	// sagaShellHTMLBudget covers the saga document as the shell renders it:
	// identity, coverage totals, the overview's explanations as descriptors, and
	// one summary per chapter. It is a budget over the *document*, so it must
	// track the number of chapters and never the number of explanations.
	sagaShellHTMLBudget = 60_000
	// sagaShellElementBudget is the same rule as a count the browser pays.
	sagaShellElementBudget = 1_600
	// chapterBodyBudget covers one opened chapter: its comments, its sections,
	// and its explanations as descriptors. It is what a reviewer waits for after
	// clicking a chapter open.
	chapterBodyBudget = 140_000
	// fragmentContentBudget covers one explanation with its marked places, its
	// annotations, and the summaries of the code it explains.
	fragmentContentBudget = 40_000
	// fileDiffBodyBudget covers one changed file rendered with review actions.
	fileDiffBodyBudget = 200_000
	// coverageDiffBodyBudget covers the same file rendered read-only for the
	// coverage audit, which needs no per-line review controls.
	coverageDiffBodyBudget = 45_000
	// pageDiffRowContextAllowance lets the selected file grow unchanged context
	// rows without turning a rendering change into a budget failure.
	pageDiffRowContextAllowance = 128
)

func TestLargeSagaFirstLoadStaysWithinPayloadBudgets(t *testing.T) {
	fixture, _, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	page := budgetRequest(t, handler, "/")

	checkBudget(t, "first-load HTML bytes", len(page), firstLoadHTMLBudget,
		"the page must describe the comparison, not contain it")
	checkBudget(t, "first-load HTML elements", strings.Count(page, "<"), firstLoadElementBudget,
		"every element here is parsed, laid out, and retained by the browser")

	// The Code Diff tab renders exactly one file. Every other changed file is
	// summarised and fetched on demand, so the page's diff rows must stay
	// proportional to that one file and not to the whole comparison.
	options := testfixture.DefaultLargeSagaOptions()
	rowBudget := 2*options.ChangedLinesPerFile + pageDiffRowContextAllowance
	rows := strings.Count(page, `class="diff-row`)
	if rows == 0 {
		t.Fatal("first load rendered no diff rows at all; the Code Diff tab has lost its selected file")
	}
	checkBudget(t, "diff rows in first-load HTML", rows, rowBudget,
		fmt.Sprintf("only the selected file may be inlined, and it changes %d lines", options.ChangedLinesPerFile))
	t.Logf("fixture: %d changed lines across %d files, %d mappings", fixture.Atoms, fixture.DiffFiles, fixture.Mappings)
}

// The saga document a reviewer receives on first load is a shell: saga identity,
// coverage totals, the overview's explanations as descriptors, one summary per
// chapter, and the navigation outline beside it. Chapter bodies and explanation
// content arrive from /api/section and /api/fragment as the reviewer reaches
// them, so the document describes the story rather than containing it.
//
// This is stated as content first and as size second. A byte budget can be met
// by markup that shrank for an unrelated reason; a page that still contains one
// explanation's prose has lost the boundary whatever it weighs.
func TestLargeSagaFirstLoadShipsOnlyTheChapterShell(t *testing.T) {
	fixture, _, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	page := budgetRequest(t, handler, "/")
	document := sagaDocumentOf(t, page)

	if summaries := strings.Count(document, "data-section-href="); summaries != fixture.Chapters {
		t.Fatalf("the shell rendered %d fetchable chapter summaries for %d chapters", summaries, fixture.Chapters)
	}
	if bodies := strings.Count(document, "data-chapter-body"); bodies != fixture.Chapters {
		t.Fatalf("the shell rendered %d chapter bodies for %d chapters", bodies, fixture.Chapters)
	}
	// Only the overview's own explanations are described inline; every other
	// explanation is named by a chapter summary that has not been opened.
	if descriptors := strings.Count(document, "data-fragment-href="); descriptors == 0 || descriptors >= fixture.Fragments {
		t.Fatalf("the shell described %d explanations; it must describe the overview's own and no more, of %d", descriptors, fixture.Fragments)
	}
	for _, content := range []string{`<article class="fragment"`, "fragment-markdown", "data-landmark-target", "fragment-frame"} {
		if strings.Contains(document, content) {
			t.Fatalf("the shell carried explanation content it was only asked to describe: %q", content)
		}
	}
	if !strings.Contains(document, "data-coverage-totals") {
		t.Fatal("the shell stopped stating the coverage totals it stands in for")
	}

	checkBudget(t, "saga document bytes on first load", len(document), sagaShellHTMLBudget,
		"the document must grow with the number of chapters, never with the number of explanations")
	checkBudget(t, "saga document elements on first load", strings.Count(document, "<"), sagaShellElementBudget,
		"every element here is parsed, laid out, and retained by the browser")

	// The payload must shrink by deferring the story, not by losing it. Every
	// destination is still named in the navigation outline and still counted in
	// the review progress map, both of which read the document rather than the
	// rendered page.
	destinations := fixture.Chapters + fixture.Sections + fixture.Fragments
	if segments := strings.Count(page, "data-review-progress-target="); segments != destinations+1 {
		t.Fatalf("review progress counted %d destinations, want %d including the overview", segments, destinations+1)
	}
	if links := strings.Count(page, `class="doc-link"`); links < fixture.Chapters+fixture.Sections {
		t.Fatalf("the navigation outline named %d destinations; it lost chapters or sections of %d", links, fixture.Chapters+fixture.Sections)
	}
}

// The endpoints the shell defers to are bounded by one node each: a chapter body
// carries that chapter's structure and no explanation content, and an
// explanation response carries exactly one explanation.
func TestChapterAndExplanationEndpointsStayWithinBudgets(t *testing.T) {
	fixture, _, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	page := budgetRequest(t, handler, "/")

	chapterHref := firstAttributeValue(t, page, "data-section-href")
	body := budgetRequest(t, handler, chapterHref)
	checkBudget(t, "chapter body bytes", len(body), chapterBodyBudget,
		"one opened chapter is what a reviewer waits for after clicking it open")
	perChapter := fixture.Fragments / fixture.Chapters
	if descriptors := strings.Count(body, "data-fragment-href="); descriptors != perChapter {
		t.Fatalf("chapter body described %d explanations, want %d", descriptors, perChapter)
	}
	if strings.Contains(body, `<article class="fragment"`) || strings.Contains(body, "fragment-markdown") {
		t.Fatal("a chapter body carried explanation content instead of describing it")
	}

	content := budgetRequest(t, handler, firstAttributeValue(t, body, "data-fragment-href"))
	checkBudget(t, "explanation bytes", len(content), fragmentContentBudget,
		"one explanation is what a reviewer waits for when it comes into view")
	if strings.Count(content, `<article class="fragment"`) != 1 {
		t.Fatalf("the explanation endpoint returned %d explanations, want exactly 1", strings.Count(content, `<article class="fragment"`))
	}
	if strings.Contains(content, "data-fragment-href=") {
		t.Fatal("a rendered explanation was still marked as needing to be fetched")
	}
}

// sagaDocumentOf isolates the saga document from the tabs beside it. The Code
// Diff and Coverage tabs answer different questions and carry their own bounded
// contracts; this change is about the story.
func sagaDocumentOf(tb testing.TB, page string) string {
	tb.Helper()
	start, end := strings.Index(page, `id="view-saga"`), strings.Index(page, `id="view-code"`)
	if start < 0 || end <= start {
		tb.Fatal("the page no longer contains a saga document followed by the Code Diff tab")
	}
	return page[start:end]
}

// firstAttributeValue reads a URL the shell told the browser to fetch, so the
// budgets exercise the exact requests the page asks for.
func firstAttributeValue(tb testing.TB, markup, attribute string) string {
	tb.Helper()
	opening := attribute + `="`
	start := strings.Index(markup, opening)
	if start < 0 {
		tb.Fatalf("markup carries no %s to follow", attribute)
	}
	rest := markup[start+len(opening):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		tb.Fatalf("unterminated %s", attribute)
	}
	return stdhtml.UnescapeString(rest[:end])
}

// TestLargeSagaFirstLoadOmitsUnopenedDiffBodies states the same rule as content
// rather than as a size: a reviewer who has not opened a file must not have
// received that file's code. A byte budget can be satisfied by markup that
// shrank for unrelated reasons; this cannot.
func TestLargeSagaFirstLoadOmitsUnopenedDiffBodies(t *testing.T) {
	_, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	page := budgetRequest(t, handler, "/")

	selected := selectedFilePath(t, page)
	changes := budgetChanges(t, application)
	inlined := inlinedCodeLines(page)
	var leaked []string
	for _, atom := range changes.Atoms {
		path := effectiveAtomPath(atom)
		if path == selected || atom.Content == "" || !inlined[atom.Content] {
			continue
		}
		leaked = append(leaked, fmt.Sprintf("%s:%d %q", path, atom.Line, atom.Content))
		if len(leaked) == 3 {
			break
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("first load shipped changed lines from files the reviewer has not opened; the Code Diff tab selected %q and these arrived anyway:\n  %s",
			selected, strings.Join(leaked, "\n  "))
	}

	// Every changed file must still be reachable, or the payload only shrank by
	// losing the audit.
	surfaces := strings.Count(page, "data-manifest-diff-href")
	if surfaces < changesFileCount(changes) {
		t.Fatalf("coverage offered %d on-demand file diffs for %d changed files; some file lost its body entirely", surfaces, changesFileCount(changes))
	}
}

func TestLargeSagaFileDiffEndpointStaysWithinBudgets(t *testing.T) {
	_, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	changes := budgetChanges(t, application)
	path := effectiveAtomPath(changes.Atoms[0])
	escaped := url.QueryEscape(path)

	body := budgetRequest(t, handler, "/api/file-diff?file="+escaped)
	checkBudget(t, "file diff response bytes", len(body), fileDiffBodyBudget,
		"one file body is what a reviewer waits for after opening a file")
	if !strings.Contains(body, "data-attached-full-diff") || !strings.Contains(body, "data-code>") {
		t.Fatal("file diff response carried no reviewable rows")
	}

	coverage := budgetRequest(t, handler, "/api/file-diff?view=manifest&file="+escaped)
	checkBudget(t, "coverage diff response bytes", len(coverage), coverageDiffBodyBudget,
		"the coverage audit renders the same lines without per-line review controls")
	if !strings.Contains(coverage, "data-code>") {
		t.Fatal("coverage diff response carried no rows")
	}
	if strings.Contains(coverage, "data-diff-action") {
		t.Fatal("the read-only coverage body grew per-line review controls")
	}
}

// TestFileDiffEndpointServesCoverageAndTargetedBodies is the correctness half of
// moving diff bodies off the page: what the page stopped inlining must still
// arrive, scoped to the narrative target that asked for it.
func TestFileDiffEndpointServesCoverageAndTargetedBodies(t *testing.T) {
	_, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	snapshot := application.snapshot(context.Background())
	if snapshot == nil || snapshot.diffErr != nil {
		t.Fatal("fixture comparison could not be read")
	}
	target, owned := busiestTarget(t, snapshot)
	path := effectiveAtomPath(owned[0])
	escaped := url.QueryEscape(path)

	scoped := budgetRequest(t, handler, "/api/file-diff?file="+escaped+"&target="+url.QueryEscape(target))
	if !strings.Contains(scoped, `data-target="`+target+`"`) {
		t.Fatalf("a body requested for %s was not attributed to it, so a comment written from its drawer would land on the wrong target", target)
	}
	// The page used to inline these rows next to the whole file so the browser
	// could compare them; the server marks them now, and must mark exactly the
	// atoms this target owns in this file.
	expected := 0
	for _, atom := range owned {
		if effectiveAtomPath(atom) == path {
			expected++
		}
	}
	if marked := strings.Count(scoped, "linked-evidence"); marked != expected {
		t.Fatalf("scoped body marked %d rows as %s evidence in %s, expected %d", marked, target, path, expected)
	}

	unscoped := budgetRequest(t, handler, "/api/file-diff?file="+escaped)
	if strings.Contains(unscoped, "linked-evidence") {
		t.Fatal("an unscoped body marked evidence it has no target for")
	}
	// The saga-diff scheme must survive templating intact wherever rows are
	// produced; the page itself no longer contains any.
	if strings.Contains(scoped, "ZgotmplZ") || !strings.Contains(scoped, `data-diff-ref="saga-diff://v1/line?`) {
		t.Fatal("diff rows lost their exact saga-diff identity")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/file-diff?file="+escaped+"&target=urn:change-saga:large:fragment:not-a-real-target", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("an unknown narrative target was accepted: %d", recorder.Code)
	}
}

// busiestTarget picks the narrative target that owns the most changed lines, so
// the evidence assertions have something substantial to be exact about.
func busiestTarget(tb testing.TB, snapshot *reviewSnapshot) (string, []gitdiff.Atom) {
	tb.Helper()
	best, bestAtoms := "", []gitdiff.Atom(nil)
	for target, atoms := range snapshot.changesByTarget {
		if len(atoms) > len(bestAtoms) || (len(atoms) == len(bestAtoms) && target < best) {
			best, bestAtoms = target, atoms
		}
	}
	if best == "" {
		tb.Fatal("fixture has no narrative target owning changed lines")
	}
	return best, bestAtoms
}

// TestReviewSnapshotIsReusedUntilTheSagaChanges is a correctness budget. Serving
// diff bodies per file only stays fast because the loaded saga, the Git
// comparison, and the coverage report are reused across requests — and reuse is
// only acceptable if a mutation is visible on the very next request.
func TestReviewSnapshotIsReusedUntilTheSagaChanges(t *testing.T) {
	_, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	path := url.QueryEscape(effectiveAtomPath(budgetChanges(t, application).Atoms[0]))
	for i := 0; i < 4; i++ {
		budgetRequest(t, handler, "/")
	}
	budgetRequest(t, handler, "/api/file-diff?file="+path)
	if application.cache.builds != 1 {
		t.Fatalf("the saga was loaded and compared %d times for requests that observed identical bytes; every file a reviewer opens would pay for a full rebuild", application.cache.builds)
	}

	snapshot := application.snapshot(context.Background())
	target, _ := busiestTarget(t, snapshot)
	values := url.Values{"target": {target}, "state": {"approved"}, "body": {"Budget test decision."}}
	request := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("review decision failed: %d %s", recorder.Code, recorder.Body.String())
	}

	page := budgetRequest(t, handler, "/")
	if application.cache.builds != 2 {
		t.Fatalf("a recorded review decision did not invalidate the snapshot (builds=%d); the reviewer would keep reading the saga as it was before their own decision", application.cache.builds)
	}
	if !strings.Contains(page, "Budget test decision.") {
		t.Fatal("the page served after a review decision did not contain it")
	}
}

func BenchmarkLargeSagaRealisticHTTP(b *testing.B) {
	options := testfixture.DefaultLargeSagaOptions()
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), b.TempDir(), options)
	if err != nil {
		b.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		b.Fatal(err)
	}
	handler := newMux(&app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl, mutationToken: "benchmark-token"})
	path := ""
	{
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK {
			b.Fatalf("warm-up status = %d", recorder.Code)
		}
		path = url.QueryEscape(selectedFilePath(b, recorder.Body.String()))
	}

	b.Run("first_load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusOK {
				b.Fatalf("status = %d", recorder.Code)
			}
			b.ReportMetric(float64(recorder.Body.Len()), "response-B")
		}
	})

	b.Run("file_diff", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/file-diff?file="+path, nil))
			if recorder.Code != http.StatusOK {
				b.Fatalf("status = %d", recorder.Code)
			}
			b.ReportMetric(float64(recorder.Body.Len()), "response-B")
		}
	})

	b.Run("coverage_diff", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/file-diff?view=manifest&file="+path, nil))
			if recorder.Code != http.StatusOK {
				b.Fatalf("status = %d", recorder.Code)
			}
			b.ReportMetric(float64(recorder.Body.Len()), "response-B")
		}
	})
}

func checkBudget(tb testing.TB, name string, measured, budget int, why string) {
	tb.Helper()
	if measured > budget {
		tb.Fatalf("%s exceeded its budget: %d > %d (%+d, %.1f×)\n  %s\n  See docs/performance.md before raising this budget.",
			name, measured, budget, measured-budget, float64(measured)/float64(budget), why)
	}
	tb.Logf("%s: %d of %d budget (%.0f%%)", name, measured, budget, float64(measured)/float64(budget)*100)
}

func budgetFixture(tb testing.TB, options testfixture.LargeSagaOptions) (testfixture.LargeSaga, *app, *http.ServeMux) {
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
	return fixture, application, newMux(application)
}

func budgetRequest(tb testing.TB, handler *http.ServeMux, path string) string {
	tb.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		tb.Fatalf("GET %s = %d: %s", path, recorder.Code, firstLine(recorder.Body.String()))
	}
	return recorder.Body.String()
}

// budgetChanges reads the comparison through the same snapshot the page used,
// so a test never disagrees with the page about what changed.
func budgetChanges(tb testing.TB, application *app) gitdiff.ChangeSet {
	tb.Helper()
	snapshot := application.snapshot(context.Background())
	if snapshot == nil {
		tb.Fatal("fixture saga could not be loaded")
	}
	if snapshot.diffErr != nil {
		tb.Fatalf("fixture comparison could not be read: %v", snapshot.diffErr)
	}
	return snapshot.changes
}

// inlinedCodeLines is every line of code the page already contains. Matching on
// the rendered cells rather than searching the whole document keeps the check
// exact: authored prose that happens to quote a changed line is not evidence
// that the diff body was inlined.
func inlinedCodeLines(page string) map[string]bool {
	lines := map[string]bool{}
	const open = "data-code>"
	for index := strings.Index(page, open); index >= 0; {
		rest := page[index+len(open):]
		end := strings.Index(rest, "</code>")
		if end < 0 {
			break
		}
		lines[stdhtml.UnescapeString(rest[:end])] = true
		next := strings.Index(rest[end:], open)
		if next < 0 {
			break
		}
		index = index + len(open) + end + next
	}
	return lines
}

func changesFileCount(changes gitdiff.ChangeSet) int {
	paths := map[string]bool{}
	for _, atom := range changes.Atoms {
		paths[effectiveAtomPath(atom)] = true
	}
	return len(paths)
}

func selectedFilePath(tb testing.TB, page string) string {
	tb.Helper()
	const marker = `<article class="file-diff" id="`
	index := strings.Index(page, marker)
	if index < 0 {
		tb.Fatal("first load rendered no selected file in the Code Diff tab")
	}
	rest := page[index:]
	const pathMarker = `data-file-path="`
	start := strings.Index(rest, pathMarker)
	if start < 0 {
		tb.Fatal("selected file carried no path")
	}
	rest = rest[start+len(pathMarker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		tb.Fatal("selected file path was unterminated")
	}
	return rest[:end]
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	if len(value) > 200 {
		return value[:200]
	}
	return value
}

// TestConcurrentRequestsShareOneSnapshotSafely covers the cost of reuse: one
// loaded saga, change set, and coverage report are now read by every in-flight
// request instead of being rebuilt per request. Run under -race, this fails if
// any handler writes to the shared model while another reads it.
func TestConcurrentRequestsShareOneSnapshotSafely(t *testing.T) {
	_, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	changes := budgetChanges(t, application)
	paths := []string{"/", "/"}
	seen := map[string]bool{}
	for _, atom := range changes.Atoms {
		path := effectiveAtomPath(atom)
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths,
			"/api/file-diff?file="+url.QueryEscape(path),
			"/api/file-diff?view=manifest&file="+url.QueryEscape(path))
		if len(paths) >= 16 {
			break
		}
	}

	var group sync.WaitGroup
	failures := make(chan string, len(paths))
	for _, path := range paths {
		group.Add(1)
		go func() {
			defer group.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				failures <- fmt.Sprintf("GET %s = %d", path, recorder.Code)
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if application.cache.builds != 1 {
		t.Fatalf("concurrent requests rebuilt the snapshot %d times; the lock is not covering the rebuild", application.cache.builds)
	}
}
