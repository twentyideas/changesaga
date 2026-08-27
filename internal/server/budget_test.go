package server

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/snapshotcache"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

// Budgets guard one architectural rule: the first load describes the whole
// comparison, and carries only the part of it a reviewer is already looking at.
// Every diff body beyond the selected file arrives from /api/file-diff when the
// reviewer opens that file.
//
// The numbers below are hard ceilings over a deterministic fixture, so they are
// exact and stable rather than timing-sensitive. Wall-clock figures are
// diagnostic, are reported by the benchmarks in this package, and are recorded
// in docs/performance.md; they are deliberately not asserted, because CI
// machines vary far more than any real regression would.
//
// A ceiling over one fixture cannot see a payload that grows with the size of
// the comparison, which is the failure diagnosed in docs/large-saga-diagnosis.md.
// firstload_budget_test.go measures the same first load across fixtures that
// move one axis each and budgets the slopes, including the allocation and
// retained-heap figures that do reproduce.
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

	rows := strings.Count(page, `class="diff-row`)
	if rows != 0 {
		t.Fatalf("first load rendered %d diff rows; all comparison data must arrive incrementally", rows)
	}
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
	if !strings.Contains(page, "data-coverage-loading") {
		t.Fatal("the shell stopped exposing the incremental coverage surface")
	}

	checkBudget(t, "saga document bytes on first load", len(document), sagaShellHTMLBudget,
		"the document must grow with the number of chapters, never with the number of explanations")
	checkBudget(t, "saga document elements on first load", strings.Count(document, "<"), sagaShellElementBudget,
		"every element here is parsed, laid out, and retained by the browser")

	// The payload must shrink by deferring the story, not by losing it. Every
	// destination is still named in the navigation outline. Every approval-
	// bearing destination is counted in the review progress map; chapters are
	// containers now and deliberately do not add decisions.
	destinations := fixture.Sections + fixture.Fragments
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

	changes := budgetChanges(t, application)
	inlined := inlinedCodeLines(page)
	if len(inlined) != 0 {
		t.Fatalf("first load shipped %d changed code lines before a file was requested", len(inlined))
	}

	code := budgetRequest(t, handler, "/api/code?limit=1")
	if !strings.Contains(code, "data-tree-file") || changesFileCount(changes) == 0 {
		t.Fatal("incremental code navigation did not preserve access to changed files")
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
	if !strings.Contains(body, `data-page-items="lines"`) || !strings.Contains(body, "data-code>") {
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
	for target, indexes := range snapshot.targetAtoms {
		if len(indexes) > len(bestAtoms) || (len(indexes) == len(bestAtoms) && target < best) {
			best, bestAtoms = target, atomsForIndexes(snapshot, indexes)
		}
	}
	if best == "" {
		tb.Fatal("fixture has no narrative target owning changed lines")
	}
	return best, bestAtoms
}

// TestReviewMutationAdvancesOnlyTheOverlayGeneration is a correctness budget.
// A review write must be visible on the next request without rebuilding the
// structural saga, Git comparison, or coverage report.
func TestReviewMutationAdvancesOnlyTheOverlayGeneration(t *testing.T) {
	fixture, application, handler := budgetFixture(t, testfixture.DefaultLargeSagaOptions())
	generations, err := snapshotcache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application.generations = generations
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
	fullLoadsBeforeMutations := saga.FullLoadCount()
	if _, validation, err := saga.LoadMutationIndex(fixture.Root); err != nil || !validation.Valid {
		t.Fatalf("large fixture mutation index is invalid: validation=%#v err=%v", validation, err)
	}
	values := url.Values{"target": {target}, "state": {"approved"}, "body": {"Budget test decision."}}
	request := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("review decision failed: %d %s", recorder.Code, recorder.Body.String())
	}

	page := budgetRequest(t, handler, "/")
	if application.cache.builds != 1 {
		t.Fatalf("a review-only mutation rebuilt structural/source state (builds=%d); comments must not rerun coverage or Git diffs", application.cache.builds)
	}
	if !strings.Contains(page, "Budget test decision.") {
		t.Fatal("the page served after a review decision did not contain it")
	}
	current := application.snapshot(context.Background())
	initialDiffReviews := len(current.document.DiffReviews)
	filePath := effectiveAtomPath(current.changes.Atoms[0])
	fileURI, err := diffuri.Build(diffuri.Reference{
		Repository: current.changes.Repository, Base: current.changes.BaseOID, Head: current.changes.HeadOID,
		Kind: "file", Path: filePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	diffValues := url.Values{"uri": {fileURI}, "state": {"reviewed"}, "file": {filePath}}
	diffRequest := httptest.NewRequest(http.MethodPost, "/api/diff-review", strings.NewReader(diffValues.Encode()))
	diffRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	diffResult := httptest.NewRecorder()
	handler.ServeHTTP(diffResult, diffRequest)
	current = application.snapshot(context.Background())
	if diffResult.Code != http.StatusSeeOther || application.cache.builds != 1 || len(current.document.DiffReviews) != initialDiffReviews+1 {
		t.Fatalf("diff-review mutation crossed the structural boundary: status=%d builds=%d reviews=%d", diffResult.Code, application.cache.builds, len(current.document.DiffReviews))
	}

	initialThreadCount := len(application.snapshot(context.Background()).document.Threads)
	comment := multipartRequest(t, "/api/thread", map[string]string{
		"target": target, "body": "Keep this comment across generations.",
		"anchor": `{"type":"target"}`,
	})
	commentResult := httptest.NewRecorder()
	handler.ServeHTTP(commentResult, comment)
	if commentResult.Code != http.StatusSeeOther {
		t.Fatalf("comment mutation failed: %d %s", commentResult.Code, commentResult.Body.String())
	}
	current = application.snapshot(context.Background())
	if application.cache.builds != 1 || len(current.document.Threads) != initialThreadCount+1 {
		t.Fatalf("adding a comment rebuilt structure or missed memory update: builds=%d threads=%d", application.cache.builds, len(current.document.Threads))
	}
	threadID := current.document.Threads[len(current.document.Threads)-1].ID

	anchorValues := url.Values{
		"thread": {threadID},
		"anchor": {`{"type":"note","coordinate_space":"normalized","note":{"text":"Edited","x":0.25,"y":0.5}}`},
	}
	anchorRequest := httptest.NewRequest(http.MethodPost, "/api/thread-anchor", strings.NewReader(anchorValues.Encode()))
	anchorRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	anchorResult := httptest.NewRecorder()
	handler.ServeHTTP(anchorResult, anchorRequest)
	if anchorResult.Code != http.StatusNoContent {
		t.Fatalf("comment edit failed: %d %s", anchorResult.Code, anchorResult.Body.String())
	}
	current = application.snapshot(context.Background())
	edited := current.document.Threads[len(current.document.Threads)-1]
	if application.cache.builds != 1 || edited.Anchor.Note == nil || edited.Anchor.Note.Text != "Edited" {
		t.Fatalf("editing a comment rebuilt structure or missed memory update: builds=%d thread=%#v", application.cache.builds, edited)
	}

	stateValues := url.Values{"thread": {threadID}, "target": {target}, "state": {"withdrawn"}}
	stateRequest := httptest.NewRequest(http.MethodPost, "/api/thread-state", strings.NewReader(stateValues.Encode()))
	stateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	stateResult := httptest.NewRecorder()
	handler.ServeHTTP(stateResult, stateRequest)
	if stateResult.Code != http.StatusSeeOther {
		t.Fatalf("comment removal failed: %d %s", stateResult.Code, stateResult.Body.String())
	}
	current = application.snapshot(context.Background())
	removed := current.document.Threads[len(current.document.Threads)-1]
	if application.cache.builds != 1 || removed.State != "withdrawn" {
		t.Fatalf("removing a comment rebuilt structure or missed memory update: builds=%d state=%q", application.cache.builds, removed.State)
	}

	application.reviewRefreshHook = func() error { return errors.New("injected post-commit refresh failure") }
	reply := multipartRequest(t, "/api/reply", map[string]string{
		"thread": threadID, "target": target, "body": "Durable even if memory refresh fails.",
	})
	replyResult := httptest.NewRecorder()
	handler.ServeHTTP(replyResult, reply)
	if replyResult.Code != http.StatusSeeOther || replyResult.Header().Get("X-Change-Saga-Review-State") != "reload-pending" {
		t.Fatalf("durable reply was reported as failed after refresh error: %d headers=%v body=%s", replyResult.Code, replyResult.Header(), replyResult.Body.String())
	}
	application.cache.mutex.Lock()
	cachedMessages := len(application.cache.review.document.Threads[len(application.cache.review.document.Threads)-1].Messages)
	application.cache.mutex.Unlock()
	if cachedMessages != 1 {
		t.Fatalf("failed refresh published speculative memory state with %d messages", cachedMessages)
	}
	persisted, persistedValidation, err := saga.LoadReviewState(application.cache.current.mutationIndex)
	if err != nil || !persistedValidation.Valid || len(persisted.Threads[len(persisted.Threads)-1].Messages) != 2 {
		t.Fatalf("acknowledged reply was not durable: validation=%#v err=%v", persistedValidation, err)
	}
	application.reviewRefreshHook = nil
	if refreshed := application.snapshot(context.Background()); len(refreshed.document.Threads[len(refreshed.document.Threads)-1].Messages) != 2 || application.cache.builds != 1 {
		t.Fatalf("pending overlay did not recover without structural rebuild: builds=%d", application.cache.builds)
	}
	if fullLoads := saga.FullLoadCount(); fullLoads != fullLoadsBeforeMutations {
		t.Fatalf("review mutations performed %d full saga loads; want 0 (before=%d after=%d)", fullLoads-fullLoadsBeforeMutations, fullLoadsBeforeMutations, fullLoads)
	}

	// A fresh server owns no prior memory generation. Its first load must replay
	// the atomic saga records and recover the final edited/withdrawn state.
	restarted := &app{root: fixture.Root, sourceDir: fixture.Repository, template: application.template, generations: generations}
	restartedSnapshot := restarted.snapshot(context.Background())
	if restartedSnapshot == nil || len(restartedSnapshot.document.Threads) != initialThreadCount+1 {
		t.Fatalf("restart did not recover the persisted comment: %#v", restartedSnapshot)
	}
	recovered := restartedSnapshot.document.Threads[len(restartedSnapshot.document.Threads)-1]
	if recovered.ID != threadID || recovered.State != "withdrawn" || recovered.Anchor.Note == nil || recovered.Anchor.Note.Text != "Edited" {
		t.Fatalf("restart recovered the wrong review generation: %#v", recovered)
	}
	if restarted.cache.builds != 0 {
		t.Fatalf("restart rebuilt cached coverage/diffs %d times; the structural generation should have been reused", restarted.cache.builds)
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
