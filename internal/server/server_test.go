package server

import (
	"bytes"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/saga"
)

func TestCreateAnchoredThreadWritesOverlayRecords(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	fields := map[string]string{
		"target":    "urn:review-saga:test:fragment:overview",
		"author":    "Ada",
		"body":      "Check this edge case.",
		"anchor":    `{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.1,"y":0.2,"width":0.3,"height":0.4}]}`,
		"return_to": "/chapters/backend#target-overview",
	}
	request := multipartRequest(t, "/api/thread", fields)
	recorder := httptest.NewRecorder()
	application.createThread(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("thread status = %d, want %d: %s", recorder.Code, http.StatusSeeOther, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/chapters/backend#target-overview" {
		t.Fatalf("thread redirect = %q, want chapter deep link", location)
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("written overlay should validate: validation=%#v err=%v", validation, err)
	}
	if len(document.Threads) != 1 || document.Threads[0].Anchor.Type != "region" || len(document.Threads[0].Messages) != 1 {
		t.Fatalf("unexpected thread: %#v", document.Threads)
	}
}

func TestCreateDiffSuggestionAndMarkFileReviewed(t *testing.T) {
	root := validServerSaga(t)
	lineURI, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/a.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "app.go", Side: "new", Start: 4, End: 4})
	if err != nil {
		t.Fatal(err)
	}
	application := &app{root: root}
	request := multipartRequest(t, "/api/thread", map[string]string{
		"target":      "urn:review-saga:test:fragment:overview",
		"author":      "Ada",
		"body":        "Prefer the guarded form.",
		"kind":        "suggestion",
		"replacement": "if ready { return nil }",
		"anchor":      `{"type":"diff","diff":{"uri":"` + lineURI + `"}}`,
	})
	recorder := httptest.NewRecorder()
	application.createThread(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("suggestion status = %d: %s", recorder.Code, recorder.Body.String())
	}
	fileURI, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/a.git", Base: "aaa", Head: "bbb", Kind: "file", Path: "app.go"})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"uri": {fileURI}, "author": {"Grace"}, "state": {"reviewed"}, "file": {"diff-app-go"}}
	request = httptest.NewRequest(http.MethodPost, "/api/diff-review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	application.diffReview(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("diff review status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != CodeDiffURL("app.go", "") {
		t.Fatalf("diff review redirect = %q, want focused file", location)
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("review data should validate: validation=%#v err=%v", validation, err)
	}
	if len(document.Threads) != 1 || document.Threads[0].Kind != "suggestion" || document.Threads[0].Suggestion == nil || len(document.DiffReviews) != 1 || document.DiffReviews[0].State != "reviewed" {
		t.Fatalf("unexpected persisted diff review: threads=%#v reviews=%#v", document.Threads, document.DiffReviews)
	}
}

func TestReviewDecisionPersistsAndReturnsToChapter(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	values := url.Values{
		"target":    {"urn:review-saga:test:fragment:overview"},
		"author":    {"Ada"},
		"state":     {"approved"},
		"body":      {"Ready to merge."},
		"return_to": {"/chapters/backend#target-overview"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	application.review(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/chapters/backend#target-overview" {
		t.Fatalf("review response = %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("written review should validate: validation=%#v err=%v", validation, err)
	}
	reviews := document.Section.Fragments[0].Reviews
	if len(reviews) != 1 || reviews[0].State != "approved" || reviews[0].Author != "Ada" || reviews[0].Body != "Ready to merge." {
		t.Fatalf("unexpected persisted review: %#v", reviews)
	}
}

func TestFragmentFileRejectsSymlinkOutsidePackage(t *testing.T) {
	root := validServerSaga(t)
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	writeServerFile(t, outside, "secret")
	fragmentDir := filepath.Join(root, "overview.fragment")
	if err := os.Symlink(outside, filepath.Join(fragmentDir, "secret.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	application := &app{root: root}
	request := httptest.NewRequest(http.MethodGet, "/f/overview/secret.txt", nil)
	request.SetPathValue("id", "overview")
	request.SetPathValue("path", "secret.txt")
	recorder := httptest.NewRecorder()
	application.fragmentFile(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("fragment status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestInteractiveFragmentIsServedWithSandboxCSP(t *testing.T) {
	root := validServerSaga(t)
	writeServerFile(t, filepath.Join(root, "demo.fragment", "fragment.json"), `{"version":2,"id":"demo","media_type":"text/html","entrypoint":"index.html"}`)
	writeServerFile(t, filepath.Join(root, "demo.fragment", "index.html"), `<button onclick="this.textContent='ok'">Run</button>`)
	application := &app{root: root}
	request := httptest.NewRequest(http.MethodGet, "/f/demo/index.html", nil)
	request.SetPathValue("id", "demo")
	request.SetPathValue("path", "index.html")
	recorder := httptest.NewRecorder()
	application.fragmentFile(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "onclick") {
		t.Fatalf("interactive fragment was not served: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	csp := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src") || !strings.Contains(csp, "connect-src 'none'") {
		t.Fatalf("unexpected fragment CSP: %s", csp)
	}
}

func TestPageTemplateAndMarkdown(t *testing.T) {
	tmpl := serverTemplate(t)
	fragmentDir := t.TempDir()
	writeServerFile(t, filepath.Join(fragmentDir, "content.md"), "# Story\n")
	fragment := &saga.Fragment{ID: "overview", Title: "Overview", Target: "urn:review-saga:test:fragment:overview", Directory: fragmentDir, MediaType: "text/markdown", Entrypoint: "content.md"}
	emptyFragment := &saga.Fragment{ID: "empty", Title: "No changes", Target: "urn:review-saga:test:fragment:empty", Directory: fragmentDir, MediaType: "text/plain", Entrypoint: "missing.txt"}
	section := &saga.Section{Kind: "chapter", ID: "root", Title: "Test", Target: "urn:review-saga:test:saga", Path: "private/root.chapter", Fragments: []*saga.Fragment{fragment, emptyFragment}}
	thread := &saga.Thread{ID: "thread", Target: fragment.Target, Anchor: saga.Anchor{Type: "region", Coordinate: "normalized", Shapes: []saga.Shape{{Type: "rect", X: .1, Y: .2, Width: .3, Height: .4}}}, State: "open"}
	lineURI := "saga-diff://v1/line?base=aaa&end=1&head=product-bbb&path=app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=new&start=1"
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/a.git", Base: "main", Head: "HEAD"}}, Section: section},
		Root: makeSectionView(section, map[string][]gitdiff.Atom{fragment.Target: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}}}, map[string][]*threadView{fragment.Target: {makeThreadView(thread)}}, nil), Chapter: true,
		Code: &CodeReviewView{},
	}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	renderedPage := output.String()
	if strings.Contains(renderedPage, "ZgotmplZ") || !strings.Contains(renderedPage, "/app.js") || !strings.Contains(renderedPage, `x="100.00"`) || !strings.Contains(renderedPage, "decision-dialog") || !strings.Contains(renderedPage, `data-diff-ref="saga-diff://v1/line?`) {
		t.Fatalf("template produced unsafe or incomplete output")
	}
	if strings.Count(renderedPage, `class="annotation-toolbox"`) != 1 || strings.Contains(renderedPage, `class="review-form"`) {
		t.Fatal("review controls were not consolidated")
	}
	if strings.Contains(renderedPage, "text/markdown") || strings.Contains(renderedPage, "text/plain") || strings.Contains(renderedPage, "private/root.chapter") || strings.Contains(renderedPage, "format v") || strings.Contains(renderedPage, ">Chapter<") {
		t.Fatal("reviewer-facing format metadata leaked into the page")
	}
	if strings.Contains(renderedPage, `data-open-diffs="diffs-`+domID(emptyFragment.Target)+`"`) || strings.Contains(renderedPage, `id="diffs-`+domID(emptyFragment.Target)+`"`) {
		t.Fatal("fragment without linked changes rendered a diff action")
	}
	rendered := string(markdown("# Heading\n\n- one\n- <script>bad</script>"))
	if strings.Contains(rendered, "<script>") || !strings.Contains(rendered, "&lt;script&gt;") {
		t.Fatalf("unexpected Markdown rendering: %s", rendered)
	}
}

func TestPageHandlerIsolatesOverviewAndChapterRoutes(t *testing.T) {
	root := validServerSaga(t)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "Root-only introduction\n")
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "chapter.json"), `{"version":2,"id":"alpha","title":"Alpha"}`)
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "alpha.fragment", "fragment.json"), `{"version":2,"id":"alpha-story","title":"Alpha story","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "alpha.fragment", "content.md"), "Alpha-exclusive narrative\n")
	writeServerFile(t, filepath.Join(root, "beta.chapter", "chapter.json"), `{"version":2,"id":"beta","title":"Beta"}`)
	writeServerFile(t, filepath.Join(root, "beta.chapter", "beta.fragment", "fragment.json"), `{"version":2,"id":"beta-story","title":"Beta story","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "beta.chapter", "beta.fragment", "content.md"), "Beta-exclusive narrative\n")
	application := &app{root: root, sourceDir: root, template: serverTemplate(t)}

	overview := httptest.NewRecorder()
	application.page(overview, httptest.NewRequest(http.MethodGet, "/", nil))
	if overview.Code != http.StatusOK {
		t.Fatalf("overview status = %d: %s", overview.Code, overview.Body.String())
	}
	overviewBody := overview.Body.String()
	if !strings.Contains(overviewBody, "Root-only introduction") || !strings.Contains(overviewBody, `href="/chapters/alpha"`) || strings.Contains(overviewBody, "Alpha-exclusive narrative") || strings.Contains(overviewBody, "Beta-exclusive narrative") {
		t.Fatal("overview did not remain isolated from chapter content")
	}

	chapterRequest := httptest.NewRequest(http.MethodGet, "/chapters/alpha", nil)
	chapterRequest.SetPathValue("chapter", "alpha")
	chapter := httptest.NewRecorder()
	application.page(chapter, chapterRequest)
	chapterBody := chapter.Body.String()
	if chapter.Code != http.StatusOK || !strings.Contains(chapterBody, "Alpha-exclusive narrative") || strings.Contains(chapterBody, "Beta-exclusive narrative") || strings.Contains(chapterBody, "Root-only introduction") {
		t.Fatal("chapter route did not render only the selected chapter")
	}
	if !strings.Contains(chapterBody, `href="#`+domID("urn:review-saga:test:fragment:alpha-story")+`"`) {
		t.Fatal("chapter route omitted its fragment deep link")
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/chapters/missing", nil)
	missingRequest.SetPathValue("chapter", "missing")
	missing := httptest.NewRecorder()
	application.page(missing, missingRequest)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing chapter status = %d, want 404", missing.Code)
	}
}

func TestPageHandlerRendersRealGitComparison(t *testing.T) {
	repo := t.TempDir()
	serverGit(t, repo, "init", "-b", "main")
	serverGit(t, repo, "config", "user.name", "Test")
	serverGit(t, repo, "config", "user.email", "test@example.test")
	writeServerFile(t, filepath.Join(repo, "base.txt"), "base\n")
	serverGit(t, repo, "add", "base.txt")
	serverGit(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(serverGit(t, repo, "rev-parse", "HEAD"))
	writeServerFile(t, filepath.Join(repo, "app.go"), "package app\n")
	writeServerFile(t, filepath.Join(repo, "web", "view.js"), "export const ready = true\n")
	serverGit(t, repo, "add", "app.go")
	serverGit(t, repo, "add", "web/view.js")
	serverGit(t, repo, "commit", "-m", "feature")
	root := filepath.Join(repo, "pr-1.saga")
	repository := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repo)}).String()
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"Test","source":{"repository":"`+repository+`","base":"`+base+`","head":"HEAD"}}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	application := &app{root: root, sourceDir: repo, template: serverTemplate(t)}
	request := httptest.NewRequest(http.MethodGet, CodeDiffURL("web/view.js", ""), nil)
	recorder := httptest.NewRecorder()
	application.page(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Review readiness check failed") || !strings.Contains(recorder.Body.String(), "Overview") || !strings.Contains(recorder.Body.String(), "Code Diff") || !strings.Contains(recorder.Body.String(), "app.go") || strings.Contains(recorder.Body.String(), "%</") || strings.Count(recorder.Body.String(), `<article class="file-diff"`) != 1 || !strings.Contains(recorder.Body.String(), `<code>web/view.js</code>`) {
		t.Fatalf("real page did not render expected diff state: status=%d", recorder.Code)
	}
	changes, err := gitdiff.Read(t.Context(), repo, repository, base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var selectedURI string
	for _, atom := range changes.Atoms {
		if atom.Path == "web/view.js" {
			selectedURI = atom.URI
			break
		}
	}
	if selectedURI == "" {
		t.Fatal("missing web/view.js atom")
	}
	application.template = template.Must(template.New("page").Parse(`{{define "page"}}{{.Code.SelectedFile.Path}}|{{.Code.SelectedDiff.URI}}|{{len .Code.SelectedDiffs}}{{end}}`))
	request = httptest.NewRequest(http.MethodGet, CodeDiffURL("", selectedURI), nil)
	recorder = httptest.NewRecorder()
	application.page(recorder, request)
	expectedSelection := "web/view.js|" + strings.ReplaceAll(selectedURI, "&", "&amp;") + "|1"
	if recorder.Code != http.StatusOK || recorder.Body.String() != expectedSelection {
		t.Fatalf("exact diff handler selection: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, CodeDiffURL("missing.go", ""), nil)
	recorder = httptest.NewRecorder()
	application.page(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown focused file status=%d", recorder.Code)
	}
}

func TestChapterIndexReportsResumeState(t *testing.T) {
	root := &sectionView{ChildViews: []*sectionView{
		{Section: &saga.Section{Kind: "chapter", ID: "done", Title: "Done"}, ReviewState: "approved"},
		{Section: &saga.Section{Kind: "chapter", ID: "started", Title: "Started"}, FragmentViews: []*fragmentView{{ReviewState: "rejected"}}},
		{Section: &saga.Section{Kind: "chapter", ID: "new", Title: "New"}},
	}}
	chapters := makeChapterIndex(root, "started")
	if len(chapters) != 3 || chapters[0].Status != "Approved" || chapters[1].Status != "In progress" || chapters[1].Action != "Resume" || !chapters[1].Active || chapters[2].Status != "Unreviewed" {
		t.Fatalf("unexpected chapter resume states: %#v", chapters)
	}
}

func TestFileViewsGroupRenameAndUseDistinctAnchors(t *testing.T) {
	changes := gitdiff.ChangeSet{
		Repository: "https://example.test/a.git", BaseOID: "aaa", HeadOID: "bbb",
		Atoms: []gitdiff.Atom{
			{Kind: "event", Event: "rename", OldPath: "old.go", NewPath: "new.go", Path: "new.go"},
			{Kind: "line", Path: "old.go", Side: "old", Line: 1, Content: "old"},
			{Kind: "line", Path: "new.go", Side: "new", Line: 1, Content: "new"},
			{Kind: "line", Path: "another.go", Side: "new", Line: 1, Content: "new"},
		},
	}
	files := makeFileViews(changes, "urn:review-saga:test:saga", nil, nil)
	if len(files) != 2 {
		t.Fatalf("files = %d, want renamed file plus another file", len(files))
	}
	if files[0].ID == files[1].ID {
		t.Fatal("file DOM anchors must be collision resistant")
	}
	for _, file := range files {
		if file.Path == "new.go" && len(file.Atoms) != 3 {
			t.Fatalf("renamed file has %d atoms, want 3", len(file.Atoms))
		}
	}
}

func validServerSaga(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "test.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"Test","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	return root
}

func multipartRequest(t *testing.T, path string, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func serverTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"markdown": markdown,
		"coord":    func(value float64) string { return strconv.FormatFloat(value*1000, 'f', 2, 64) },
		"points":   func([]saga.Point) string { return "" },
	}).Parse(pageTemplate)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func serverGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeServerFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
