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

func TestIncrementalComparisonEndpointsArePaginated(t *testing.T) {
	_, _, handler := boundedFixture(t)

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
	if got := strings.Count(coverage.Body.String(), `class="manifest-page-row"`); got < 1 || got > 3 {
		t.Fatalf("coverage page returned %d grouped audit rows, want 1..3", got)
	}
	if coverage.Header().Get("X-Change-Saga-Next-Cursor") == "" || !strings.Contains(coverage.Body.String(), `data-returned="3"`) {
		t.Fatal("coverage page did not advertise its continuation")
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
