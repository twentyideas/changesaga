package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

func boundedFixture(t *testing.T) (testfixture.LargeSaga, *app, *http.ServeMux) {
	t.Helper()
	options := testfixture.LargeSagaOptions{
		Chapters: 1, SectionsPerChapter: 1, FragmentsPerSection: 1,
		SourceFiles: 4, ChangedLinesPerFile: 8, CoverageRangeWidth: 1,
	}
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), t.TempDir(), options)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	application := &app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl}
	return fixture, application, newMux(application)
}

func TestRootNeverCallsTheComparisonLoaderOrFingerprintsEvidence(t *testing.T) {
	fixture, application, handler := boundedFixture(t)
	var calls atomic.Int32
	release := make(chan struct{})
	application.comparisonLoader = func(ctx context.Context) (*reviewSnapshot, error) {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		done <- recorder
	}()
	select {
	case recorder := <-done:
		if recorder.Code != http.StatusOK {
			t.Fatalf("root status = %d: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), `class="diff-row`) {
			t.Fatal("root materialized diff rows")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("root blocked behind the comparison loader")
	}
	close(release)
	if calls.Load() != 0 || application.cache.builds != 0 {
		t.Fatalf("root touched comparison path: loader calls=%d builds=%d", calls.Load(), application.cache.builds)
	}
	if application.outline.builds != 1 {
		t.Fatalf("outline builds = %d, want 1", application.outline.builds)
	}

	var diffDir string
	err := filepath.WalkDir(fixture.Root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == "___diffs" && diffDir == "" {
			diffDir = path
			return filepath.SkipDir
		}
		return err
	})
	if err != nil || diffDir == "" {
		t.Fatalf("find evidence directory: path=%q err=%v", diffDir, err)
	}
	writeServerFile(t, filepath.Join(diffDir, "root-must-ignore.json"), `{not valid JSON`)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || application.outline.builds != 1 {
		t.Fatalf("evidence-only edit invalidated root: status=%d outline builds=%d", recorder.Code, application.outline.builds)
	}
}

func TestCodeCatalogNeverCallsTheFullComparisonLoader(t *testing.T) {
	_, application, handler := boundedFixture(t)
	application.comparisonLoader = func(context.Context) (*reviewSnapshot, error) {
		t.Fatal("changed-file catalog requested the full source comparison")
		return nil, nil
	}

	first := getPage(t, handler, "/api/code?limit=2")
	if got := strings.Count(first.Body.String(), "data-tree-file"); got != 2 {
		t.Fatalf("catalog returned %d files, want 2", got)
	}
	if strings.Contains(first.Body.String(), "Explanations load separately from this file.") || !strings.Contains(first.Body.String(), "Loading explanations…") {
		t.Fatal("code catalog did not expose the live explanations state")
	}
	marker := `data-tree-path="`
	start := strings.Index(first.Body.String(), marker)
	if start < 0 {
		t.Fatal("code catalog named no changed file")
	}
	rest := first.Body.String()[start+len(marker):]
	filePath := rest[:strings.Index(rest, `"`)]
	owners := getPage(t, handler, "/api/file-owners?file="+url.QueryEscape(filePath))
	if !strings.Contains(owners.Body.String(), `class="related-fragment"`) {
		t.Fatal("selected file did not expose its narrative owners")
	}
	file := getPage(t, handler, "/api/file-diff?limit=2&file="+url.QueryEscape(filePath))
	if got := strings.Count(file.Body.String(), `class="diff-row`); got != 2 {
		t.Fatalf("selected file returned %d rows, want 2", got)
	}
	if application.cache.builds != 0 || application.catalog.builds != 1 {
		t.Fatalf("code and selected-file builds: full=%d catalog=%d", application.cache.builds, application.catalog.builds)
	}
	reviewFile, ok := catalogFile(application.catalog.value, filePath)
	if !ok {
		t.Fatalf("selected path %q was absent from its catalog", filePath)
	}
	reviewValues := url.Values{
		"uri":   {catalogFileView(application.catalog.value, reviewFile, saga.DiffReview{}).URI},
		"state": {"reviewed"},
		"file":  {filePath},
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/diff-review", strings.NewReader(reviewValues.Encode()))
	reviewRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reviewResult := httptest.NewRecorder()
	handler.ServeHTTP(reviewResult, reviewRequest)
	if reviewResult.Code != http.StatusSeeOther {
		t.Fatalf("file review = %d: %s", reviewResult.Code, reviewResult.Body.String())
	}
	warm := getPage(t, handler, "/api/code?limit=2")
	if !strings.Contains(warm.Body.String(), "Reviewed") {
		t.Fatal("bounded code catalog omitted the disk-backed file-review overlay")
	}
	if application.cache.builds != 0 || application.catalog.builds != 1 {
		t.Fatalf("warm code catalog rebuilt: full=%d catalog=%d", application.cache.builds, application.catalog.builds)
	}
}

func TestCodeCatalogListsEveryExplanationThatUsesTheSelectedFile(t *testing.T) {
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), t.TempDir(), testfixture.LargeSagaOptions{
		Chapters: 1, SectionsPerChapter: 1, FragmentsPerSection: 3,
		SourceFiles: 1, ChangedLinesPerFile: 9, CoverageRangeWidth: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	application := &app{root: fixture.Root, sourceDir: fixture.Repository, template: tmpl}
	application.comparisonLoader = func(context.Context) (*reviewSnapshot, error) {
		t.Fatal("reverse evidence links requested the full source comparison")
		return nil, nil
	}
	handler := newMux(application)
	page := getPage(t, handler, "/api/file-owners?file=src%2Fcomponent-000.txt")
	if got := strings.Count(page.Body.String(), `class="related-fragment"`); got != 3 {
		t.Fatalf("selected file listed %d explanations, want all 3", got)
	}
	for _, title := range []string{"Fragment 00.00.00", "Fragment 00.00.01", "Fragment 00.00.02"} {
		if !strings.Contains(page.Body.String(), title) {
			t.Errorf("selected file omitted explanation %q", title)
		}
	}
	getPage(t, handler, "/api/file-owners?file=src%2Fcomponent-000.txt")
	if application.evidence.builds != 1 {
		t.Fatalf("reverse evidence index built %d times, want one reusable generation", application.evidence.builds)
	}
	if application.cache.builds != 0 {
		t.Fatalf("reverse evidence links built the global comparison %d times", application.cache.builds)
	}
}

func TestIncrementalComparisonEndpointsArePaginated(t *testing.T) {
	_, application, handler := boundedFixture(t)

	code := getPage(t, handler, "/api/code?limit=2")
	if got := strings.Count(code.Body.String(), "data-tree-file"); got != 2 {
		t.Fatalf("code page returned %d files, want 2", got)
	}
	codeCursor := code.Header().Get("X-Change-Saga-Next-Cursor")
	if codeCursor == "" || !strings.Contains(code.Body.String(), `data-has-more="true"`) {
		t.Fatal("code page did not advertise its continuation")
	}
	getPage(t, handler, "/api/code?limit=2&cursor="+url.QueryEscape(codeCursor))

	coverage := getPage(t, handler, "/api/coverage?mode=code&limit=3")
	if got := strings.Count(coverage.Body.String(), `manifest-page-row`); got != 3 {
		t.Fatalf("coverage page returned %d file summaries, want 3", got)
	}
	if coverage.Header().Get("X-Change-Saga-Next-Cursor") == "" || !strings.Contains(coverage.Body.String(), `data-returned="3"`) {
		t.Fatal("coverage page did not advertise its continuation")
	}
	if strings.Contains(coverage.Body.String(), `class="manifest-row`) || !strings.Contains(coverage.Body.String(), `data-coverage-file-href=`) {
		t.Fatal("coverage file summaries eagerly rendered their ownership ranges")
	}
	current := application.snapshot(context.Background())
	if current == nil || len(current.fileOrder) == 0 || len(current.targetOrder) == 0 {
		t.Fatal("coverage snapshot did not expose indexed files and targets")
	}
	fileCoverage := getPage(t, handler, "/api/coverage-file?file="+url.QueryEscape(current.fileOrder[0]))
	if !strings.Contains(fileCoverage.Body.String(), `data-coverage-file-response`) || !strings.Contains(fileCoverage.Body.String(), `class="manifest-row`) {
		t.Fatal("opening a coverage file did not load its ownership ranges")
	}
	targetCoverage := getPage(t, handler, "/api/coverage-target?limit=2&target="+url.QueryEscape(current.targetOrder[0]))
	if !strings.Contains(targetCoverage.Body.String(), `data-coverage-target-response`) || !strings.Contains(targetCoverage.Body.String(), `class="manifest-target-file`) {
		t.Fatal("opening a coverage target did not load its linked files")
	}
	sagaCoverage := getPage(t, handler, "/api/coverage?mode=saga&limit=1")
	if strings.Count(sagaCoverage.Body.String(), `data-coverage-target-href=`) != 1 || strings.Contains(sagaCoverage.Body.String(), `class="manifest-target-file"`) {
		t.Fatal("Saga-first coverage did not defer the selected target's linked files")
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/coverage?mode=code&limit=2&cursor="+url.QueryEscape(codeCursor), nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("cursor from another endpoint returned %d, want 400", invalid.Code)
	}

	oneFile := getPage(t, handler, "/api/code?limit=1")
	body := oneFile.Body.String()
	marker := `data-tree-path="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("code page named no changed file")
	}
	rest := body[start+len(marker):]
	path := rest[:strings.Index(rest, `"`)]
	diff := getPage(t, handler, "/api/file-diff?limit=3&file="+url.QueryEscape(path))
	if got := strings.Count(diff.Body.String(), `class="diff-row`); got != 3 {
		t.Fatalf("file diff returned %d rows, want 3", got)
	}
	if diff.Header().Get("X-Change-Saga-Next-Cursor") == "" || !strings.Contains(diff.Body.String(), `data-file-path="`+path+`"`) {
		t.Fatal("file diff omitted its path or continuation")
	}
}

func TestHTTPPageLimitCapsAtHardMaximum(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/code?limit=100000", nil)
	window, err := pageRequest(request, "code", 1000, defaultSurfacePageLimit, maxSurfacePageLimit)
	if err != nil {
		t.Fatal(err)
	}
	if returned := window.end - window.start; returned != 200 || maxDiffPageLimit != maxSurfacePageLimit {
		t.Fatalf("hard page cap = %d, diff max = %d, want 200 across surfaces", returned, maxDiffPageLimit)
	}
}

func getPage(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET %s did not disable caching live review state", path)
	}
	return recorder
}
