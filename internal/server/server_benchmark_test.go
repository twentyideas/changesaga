package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

const (
	benchmarkChapterCount        = 12
	benchmarkFragmentsPerChapter = 8
)

func BenchmarkLargeSagaHTTP(b *testing.B) {
	repo, root := largeServerSaga(b)
	tmpl, err := newPageTemplate()
	if err != nil {
		b.Fatal(err)
	}
	handler := newMux(&app{root: root, sourceDir: repo, template: tmpl, mutationToken: "benchmark-token"})

	b.Run("first_load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusOK {
				b.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			b.ReportMetric(float64(recorder.Body.Len()), "response-B")
		}
	})

	b.Run("chapter_navigation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chapters/chapter-06", nil))
			if recorder.Code != http.StatusFound || recorder.Header().Get("Location") == "" {
				b.Fatalf("status = %d location = %q", recorder.Code, recorder.Header().Get("Location"))
			}
		}
	})
}

func largeServerSaga(tb testing.TB) (repo, root string) {
	tb.Helper()
	repo = tb.TempDir()
	runBenchmarkGit(tb, repo, "init", "-b", "main")
	runBenchmarkGit(tb, repo, "config", "user.name", "Benchmark")
	runBenchmarkGit(tb, repo, "config", "user.email", "benchmark@example.test")
	repository, err := diffuri.FileRepository(repo)
	if err != nil {
		tb.Fatal(err)
	}
	root = filepath.Join(repo, "large.saga")
	writeBenchmarkFile(tb, filepath.Join(root, "saga.json"), fmt.Sprintf(`{"version":2,"id":"large","title":"Large Saga","source":{"repository":%q,"base":"HEAD","head":"HEAD"}}`, repository))
	for chapter := 0; chapter < benchmarkChapterCount; chapter++ {
		chapterID := fmt.Sprintf("chapter-%02d", chapter)
		chapterDir := filepath.Join(root, chapterID+".chapter")
		writeBenchmarkFile(tb, filepath.Join(chapterDir, "chapter.json"), fmt.Sprintf(`{"version":2,"id":%q,"title":%q,"order":%d}`, chapterID, "Chapter "+chapterID, chapter))
		for fragment := 0; fragment < benchmarkFragmentsPerChapter; fragment++ {
			fragmentID := fmt.Sprintf("%s-fragment-%02d", chapterID, fragment)
			fragmentDir := filepath.Join(chapterDir, fragmentID+".fragment")
			writeBenchmarkFile(tb, filepath.Join(fragmentDir, "fragment.json"), fmt.Sprintf(`{"version":2,"id":%q,"title":%q,"media_type":"text/markdown","entrypoint":"content.md","order":%d}`, fragmentID, "Fragment "+fragmentID, fragment))
			writeBenchmarkFile(tb, filepath.Join(fragmentDir, "content.md"), fmt.Sprintf("# %s\n\nDeterministic benchmark prose for a large saga chapter.\n", fragmentID))
			reviewID := fmt.Sprintf("20260101T000000000000000-%04d", chapter*benchmarkFragmentsPerChapter+fragment)
			writeBenchmarkFile(tb, filepath.Join(fragmentDir, "___approvals", reviewID+"-approved.json"), fmt.Sprintf(`{"version":2,"id":%q,"state":"approved","created_at":"2026-01-01T00:00:00Z"}`, reviewID))
		}
	}
	runBenchmarkGit(tb, repo, "add", ".")
	runBenchmarkGit(tb, repo, "commit", "-m", "large saga fixture")
	return repo, root
}

func writeBenchmarkFile(tb testing.TB, path, body string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		tb.Fatal(err)
	}
}

func runBenchmarkGit(tb testing.TB, dir string, args ...string) {
	tb.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
