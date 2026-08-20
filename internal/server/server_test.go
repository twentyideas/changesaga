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
	"time"

	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitattribution"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/reviewstore"
	"github.com/review-saga/review-saga/internal/saga"
)

func TestCreateAnchoredThreadWritesOverlayRecords(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	fields := map[string]string{
		"target":    "urn:review-saga:test:fragment:overview",
		"body":      "Check this edge case.",
		"anchor":    `{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.1,"y":0.2,"width":0.3,"height":0.4,"color":"#336699"}]}`,
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
	if len(document.Threads) != 1 || document.Threads[0].Anchor.Type != "region" || document.Threads[0].Anchor.Shapes[0].Color != "#336699" || len(document.Threads[0].Messages) != 1 {
		t.Fatalf("unexpected thread: %#v", document.Threads)
	}
}

func TestCreateTextHighlightPersistsChosenColor(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	request := multipartRequest(t, "/api/thread", map[string]string{
		"target": "urn:review-saga:test:fragment:overview",
		"body":   "This phrase matters.",
		"anchor": `{"type":"text","text":{"exact":"Story","start":0,"end":5,"color":"#aa22cc"}}`,
	})
	recorder := httptest.NewRecorder()
	application.createThread(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("highlight status = %d: %s", recorder.Code, recorder.Body.String())
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid || len(document.Threads) != 1 {
		t.Fatalf("written highlight should validate: document=%#v validation=%#v err=%v", document, validation, err)
	}
	if color := document.Threads[0].Anchor.Text.Color; color != "#aa22cc" {
		t.Fatalf("highlight color = %q, want #aa22cc", color)
	}
}

func TestCreateThreadReturnsUndoHistoryMarker(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	request := multipartRequest(t, "/api/thread", map[string]string{
		"target":         "urn:review-saga:test:fragment:overview",
		"body":           "Undoable comment.",
		"anchor":         `{"type":"text","text":{"exact":"Story","start":0,"end":5}}`,
		"return_to":      "/chapters/backend?view=saga#target-overview",
		"record_history": "1",
	})
	recorder := httptest.NewRecorder()
	application.createThread(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("thread status = %d: %s", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != "/chapters/backend" || location.Fragment != "target-overview" || location.Query().Get("view") != "saga" {
		t.Fatalf("history redirect lost review location: %s", location)
	}
	if location.Query().Get("saga_action") != "thread-created" || location.Query().Get("saga_thread") == "" || location.Query().Get("saga_target") != "urn:review-saga:test:fragment:overview" || location.Query().Get("saga_label") != "highlight" {
		t.Fatalf("history redirect omitted undo metadata: %s", location)
	}
}

func TestWithdrawnThreadIsHiddenUntilReopened(t *testing.T) {
	root := validServerSaga(t)
	threadID, err := reviewstore.AddThread(root, "urn:review-saga:test:fragment:overview", "Temporarily hidden", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewstore.SetState(root, threadID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("page").Parse(`{{define "page"}}{{len (index .Root.FragmentViews 0).Threads}}{{end}}`))
	application := &app{root: root, sourceDir: root, template: tmpl}
	render := func() string {
		recorder := httptest.NewRecorder()
		application.page(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("page status = %d: %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	if got := render(); got != "0" {
		t.Fatalf("withdrawn thread count = %q, want 0", got)
	}
	if err := reviewstore.SetState(root, threadID, "open"); err != nil {
		t.Fatal(err)
	}
	if got := render(); got != "1" {
		t.Fatalf("reopened thread count = %q, want 1", got)
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
	values := url.Values{"uri": {fileURI}, "state": {"reviewed"}, "file": {"diff-app-go"}}
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
	if len(reviews) != 1 || reviews[0].State != "approved" || reviews[0].Author != "" || reviews[0].Body != "Ready to merge." {
		t.Fatalf("unexpected persisted review: %#v", reviews)
	}
}

func TestPageAttributesSagaFromItsOwnRepository(t *testing.T) {
	repo := t.TempDir()
	serverGit(t, repo, "init", "-b", "main")
	root := filepath.Join(repo, "test.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"Test","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	writeServerFile(t, filepath.Join(root, "overview.fragment", "___approvals", "review.json"), `{"version":2,"id":"review","author":"Payload Name","state":"approved","created_at":"2026-08-19T12:00:00Z"}`)
	serverGit(t, repo, "add", ".")
	serverGitEnv(t, repo, []string{
		"GIT_AUTHOR_NAME=Git Author", "GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME=Saga Reviewer", "GIT_COMMITTER_EMAIL=reviewer@example.test",
	}, "commit", "-m", "saga review")

	tmpl := template.Must(template.New("page").Parse(`{{define "page"}}{{(index .Root.FragmentViews 0).ReviewAuthor}}|{{(index .Root.FragmentViews 0).ReviewDetail}}{{end}}`))
	application := &app{root: root, sourceDir: t.TempDir(), template: tmpl}
	recorder := httptest.NewRecorder()
	application.page(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Saga Reviewer") || !strings.Contains(recorder.Body.String(), "reviewer@example.test") || strings.Contains(recorder.Body.String(), "Payload Name") {
		t.Fatalf("saga repository attribution was not canonical: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestUnavailableHistoryNeverFallsBackToPayloadIdentityOrChangesEventTime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	eventTime := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(root, "event.json")
	document := &saga.Saga{
		Section: &saga.Section{Reviews: []saga.Review{{Path: path, Author: "Payload Approval", CreatedAt: eventTime}}},
		Threads: []*saga.Thread{{
			Directory: filepath.Join(root, "thread.thread"), CreatedBy: "Payload Thread", CreatedAt: eventTime,
			Messages: []*saga.Message{{Path: path, Author: "Payload Reply", CreatedAt: eventTime}},
			Events:   []saga.ThreadEvent{{Path: path, Author: "Payload State", CreatedAt: eventTime}},
		}},
		DiffReviews: []saga.DiffReview{{Path: path, Author: "Payload Diff", CreatedAt: eventTime}},
	}
	applyGitAttribution(t.Context(), gitattribution.New(t.Context(), root), document)
	authors := []string{
		document.Section.Reviews[0].Author,
		document.Threads[0].CreatedBy,
		document.Threads[0].Messages[0].Author,
		document.Threads[0].Events[0].Author,
		document.DiffReviews[0].Author,
	}
	for _, author := range authors {
		if author != "Git history unavailable" {
			t.Fatalf("unavailable history trusted payload identity: %q", author)
		}
	}
	if !document.Section.Reviews[0].CreatedAt.Equal(eventTime) || !document.Threads[0].CreatedAt.Equal(eventTime) || !document.DiffReviews[0].CreatedAt.Equal(eventTime) {
		t.Fatal("attribution changed event ordering timestamps")
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
	writeServerFile(t, filepath.Join(fragmentDir, "content.md"), "# Story {#story}\n")
	landmarkTarget := saga.LandmarkTarget("test", "overview", "story-text")
	fragment := &saga.Fragment{ID: "overview", Title: "Overview", Target: "urn:review-saga:test:fragment:overview", Directory: fragmentDir, MediaType: "text/markdown", Entrypoint: "content.md", Landmarks: []saga.Landmark{{Version: 2, ID: "story-text", Label: "Story text", Target: landmarkTarget, Selector: saga.LandmarkSelector{Type: "text", Exact: "Story"}}}}
	emptyFragment := &saga.Fragment{ID: "empty", Title: "No changes", Target: "urn:review-saga:test:fragment:empty", Directory: fragmentDir, MediaType: "text/plain", Entrypoint: "missing.txt"}
	section := &saga.Section{Kind: "chapter", ID: "root", Title: "Test", Target: "urn:review-saga:test:saga", Path: "private/root.chapter", Fragments: []*saga.Fragment{fragment, emptyFragment}}
	thread := &saga.Thread{ID: "thread", Target: fragment.Target, Anchor: saga.Anchor{Type: "region", Coordinate: "normalized", Shapes: []saga.Shape{{Type: "rect", X: .1, Y: .2, Width: .3, Height: .4, Color: "#336699"}}}, State: "open", Messages: []*saga.Message{{ID: "message", CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}}}
	lineURI := "saga-diff://v1/line?base=aaa&end=1&head=product-bbb&path=app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=new&start=1"
	fragment.Diffs = []saga.DiffFile{{Version: 2, Diffs: []saga.DiffReference{{URI: lineURI, Note: "Adds the package entrypoint so the example compiles."}}}}
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/a.git", Base: "main", Head: "HEAD"}}, Section: section},
		Root: makeSectionView(section, map[string][]gitdiff.Atom{fragment.Target: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}}, landmarkTarget: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}}}, map[string][]*threadView{fragment.Target: {makeThreadView(thread)}}, nil), Chapter: true,
		Code: &CodeReviewView{}, Manifest: &CoverageManifestView{Complete: true, Total: 1, Covered: 1, MappingCount: 1, Files: []*ManifestFileView{{Path: "app.go", AtomCount: 1, Added: 1, Covered: 1, Chunks: []*ManifestChunkView{{Label: "+1", Path: "app.go", Excerpt: "package app", Href: CodeDiffURL("app.go", lineURI), Covered: true, Owners: []*ManifestOwnerView{{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}}}}}}, Targets: []*ManifestTargetView{{ManifestOwnerView: ManifestOwnerView{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}, AtomCount: 1, Chunks: []*ManifestChunkView{{Label: "+1", Path: "app.go", Excerpt: "package app", Href: CodeDiffURL("app.go", lineURI)}}}}},
	}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	renderedPage := output.String()
	if strings.Contains(renderedPage, "ZgotmplZ") || !strings.Contains(renderedPage, "/app.js") || !strings.Contains(renderedPage, `x="100.00"`) || !strings.Contains(renderedPage, "decision-dialog") || !strings.Contains(renderedPage, `data-diff-ref="saga-diff://v1/line?`) || !strings.Contains(renderedPage, `id="`+domID(fragment.Target)+`--story"`) {
		t.Fatalf("template produced unsafe or incomplete output")
	}
	if strings.Count(renderedPage, `class="annotation-toolbox"`) != 1 || strings.Contains(renderedPage, `class="review-form"`) {
		t.Fatal("review controls were not consolidated")
	}
	if !strings.Contains(renderedPage, `body data-saga-id="test"`) || !strings.Contains(renderedPage, `data-undo disabled`) || !strings.Contains(renderedPage, `data-redo disabled`) || strings.Count(renderedPage, `name="record_history" value="1"`) != 2 {
		t.Fatal("annotation command history controls were not rendered")
	}
	if !strings.Contains(renderedPage, `data-view-tab="manifest"`) || !strings.Contains(renderedPage, `data-manifest-panel="code"`) || !strings.Contains(renderedPage, "Everything is accounted for") || !strings.Contains(renderedPage, "Code → Saga") || !strings.Contains(renderedPage, "Saga → Code") {
		t.Fatal("bidirectional coverage manifest was not rendered")
	}
	if strings.Contains(renderedPage, "Attached code") || strings.Contains(renderedPage, "Linked diffs</h2>") || !strings.Contains(renderedPage, `<div class="drawer-head"><strong>Linked code</strong>`) {
		t.Fatal("attached-code drawer retained redundant header chrome")
	}
	if !strings.Contains(renderedPage, `class="attached-file"`) || !strings.Contains(renderedPage, "Adds the package entrypoint so the example compiles.") || !strings.Contains(renderedPage, "Open full file in Code Diff") || strings.Contains(renderedPage, `<details class="attached-file" open`) {
		t.Fatal("attached code was not presented as a collapsed, explained file list")
	}
	if !strings.Contains(renderedPage, `data-annotation-color`) || !strings.Contains(renderedPage, `stroke="#336699"`) || !strings.Contains(renderedPage, `data-copy-link="#`+domID("thread:thread")+`"`) || !strings.Contains(renderedPage, `data-copy-link="#`+domID("message:message")+`"`) {
		t.Fatal("annotation colors or committed-item permalinks were not rendered")
	}
	if !strings.Contains(renderedPage, `data-landmark-type="text"`) || !strings.Contains(renderedPage, `data-exact="Story"`) || !strings.Contains(renderedPage, `data-prefix=""`) || !strings.Contains(renderedPage, `>Story text</a>`) {
		t.Fatal("fragment landmarks were not exposed as deep links")
	}
	if !strings.Contains(renderedPage, `data-open-diffs="diffs-`+domID(fragment.Target)+`--story-text"`) {
		t.Fatal("landmark-related code was not exposed in place")
	}
	if strings.Contains(renderedPage, `name="author"`) || strings.Contains(renderedPage, "Your name") || strings.Contains(renderedPage, "reviewer-name") {
		t.Fatal("review UI asked for editable author identity")
	}
	if strings.Contains(renderedPage, "text/markdown") || strings.Contains(renderedPage, "text/plain") || strings.Contains(renderedPage, "private/root.chapter") || strings.Contains(renderedPage, "format v") || strings.Contains(renderedPage, ">Chapter<") {
		t.Fatal("reviewer-facing format metadata leaked into the page")
	}
	if strings.Contains(renderedPage, `data-open-diffs="diffs-`+domID(emptyFragment.Target)+`"`) || strings.Contains(renderedPage, `id="diffs-`+domID(emptyFragment.Target)+`"`) {
		t.Fatal("fragment without linked changes rendered a diff action")
	}
	rendered := string(markdown("# Heading {#stable-heading}\n\n- one\n- <script>bad</script>"))
	if strings.Contains(rendered, "<script>") || strings.Contains(rendered, "{#stable-heading}") || !strings.Contains(rendered, "&lt;script&gt;") || !strings.Contains(rendered, `id="heading--stable-heading"`) || !strings.Contains(rendered, `data-copy-link="#heading--stable-heading"`) {
		t.Fatalf("unexpected Markdown rendering: %s", rendered)
	}
}

func TestSVGAspectRatioKeepsHotspotsAligned(t *testing.T) {
	if got := svgAspectRatio(`<svg viewBox="0 0 1200 640"></svg>`); got != "1.87500000" {
		t.Fatalf("aspect ratio = %q", got)
	}
	if got := svgAspectRatio(`<svg></svg>`); got != "" {
		t.Fatalf("missing viewBox ratio = %q", got)
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
	if strings.Contains(overviewBody, `<details class="chapter-card`) || !strings.Contains(overviewBody, `<a class="chapter-card`) {
		t.Fatal("chapter cards must navigate directly without an intermediate open action")
	}

	chapterRequest := httptest.NewRequest(http.MethodGet, "/chapters/alpha", nil)
	chapterRequest.SetPathValue("chapter", "alpha")
	chapter := httptest.NewRecorder()
	application.page(chapter, chapterRequest)
	chapterBody := chapter.Body.String()
	if chapter.Code != http.StatusOK || !strings.Contains(chapterBody, "Alpha-exclusive narrative") || strings.Contains(chapterBody, "Beta-exclusive narrative") || strings.Contains(chapterBody, "Root-only introduction") {
		t.Fatal("chapter route did not render only the selected chapter")
	}
	if strings.Contains(chapterBody, `<h3>Alpha story</h3>`) || strings.Contains(chapterBody, `class="nav-child" href="#`+domID("urn:review-saga:test:fragment:alpha-story")+`"`) {
		t.Fatal("chapter route rendered redundant fragment labels around the narrative")
	}
	if !strings.Contains(chapterBody, `id="`+domID("urn:review-saga:test:fragment:alpha-story")+`"`) {
		t.Fatal("chapter route omitted its fragment deep-link target")
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
	if len(chapters) != 3 || chapters[0].Status != "Approved" || chapters[1].Status != "In progress" || !chapters[1].Active || chapters[2].Status != "Unreviewed" {
		t.Fatalf("unexpected chapter resume states: %#v", chapters)
	}
}

func TestDOMIDIsStableAndCollisionResistantAfterReadablePrefix(t *testing.T) {
	prefix := "urn:review-saga:test:fragment:" + strings.Repeat("shared-prefix", 10)
	first := domID(prefix + "-one")
	second := domID(prefix + "-two")
	if first == second || first != domID(prefix+"-one") {
		t.Fatalf("DOM IDs must be stable and collision resistant: %q %q", first, second)
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
		"domID":    domID,
		"annotationColor": func(value string) string {
			if validAnnotationColor(value) {
				return value
			}
			return "#d04832"
		},
		"coord":  func(value float64) string { return strconv.FormatFloat(value*1000, 'f', 2, 64) },
		"points": func([]saga.Point) string { return "" },
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

func serverGitEnv(t *testing.T, dir string, environment []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), environment...)
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
