package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/reviewstore"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestSecureHandlerRejectsCrossOriginFetchSiteAndHost(t *testing.T) {
	called := 0
	handler := secureHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}), "127.0.0.1:7342")
	tests := []struct {
		name   string
		method string
		host   string
		header map[string]string
	}{
		{name: "origin", method: http.MethodPost, host: "127.0.0.1:7342", header: map[string]string{"Origin": "http://evil.test"}},
		{name: "fetch metadata", method: http.MethodPost, host: "127.0.0.1:7342", header: map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{name: "host", method: http.MethodGet, host: "attacker.test:7342"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://127.0.0.1:7342/", nil)
			request.Host = test.host
			for key, value := range test.header {
				request.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want forbidden", recorder.Code)
			}
		})
	}
	if called != 0 {
		t.Fatalf("rejected requests reached handler %d times", called)
	}

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7342/", nil)
	request.Host = "127.0.0.1:7342"
	request.Header.Set("Origin", "http://127.0.0.1:7342")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("same-origin request status=%d called=%d", recorder.Code, called)
	}
}

func TestEveryMutationEndpointRequiresSessionToken(t *testing.T) {
	application := &app{root: validServerSaga(t), mutationToken: "correct-token"}
	handler := secureHandler(newMux(application), "127.0.0.1:7342")
	for _, path := range []string{"/api/thread", "/api/reply", "/api/thread-state", "/api/thread-anchor", "/api/review", "/api/diff-review"} {
		t.Run(path, func(t *testing.T) {
			var request *http.Request
			if path == "/api/thread" || path == "/api/reply" {
				request = multipartRequest(t, path, map[string]string{})
			} else {
				request = httptest.NewRequest(http.MethodPost, path, strings.NewReader("state=open"))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			request.Host = "127.0.0.1:7342"
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "mutation token") {
				t.Fatalf("status=%d body=%q, want missing-token rejection", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestValidMutationTokenPassesSecurityGate(t *testing.T) {
	application := &app{root: validServerSaga(t), mutationToken: "correct-token"}
	values := url.Values{"thread": {"missing"}, "state": {"open"}}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7342/api/thread-state", strings.NewReader(values.Encode()))
	request.Host = "127.0.0.1:7342"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Change-Saga-Mutation-Token", "correct-token")
	recorder := httptest.NewRecorder()
	secureHandler(newMux(application), "127.0.0.1:7342").ServeHTTP(recorder, request)
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("valid token was rejected: %s", recorder.Body.String())
	}
}

func TestHTTPServerHasBoundedResourceSettings(t *testing.T) {
	server := newHTTPServer(http.NotFoundHandler())
	if server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 || server.ReadHeaderTimeout <= 0 || server.MaxHeaderBytes <= 0 {
		t.Fatalf("server limits are incomplete: %#v", server)
	}
}

func TestMutationTokensAreRandomAndPlumbedIntoBrowserRequests(t *testing.T) {
	first, err := newMutationToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMutationToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) < 40 {
		t.Fatalf("mutation tokens are not independent 256-bit values: %q %q", first, second)
	}
	if !strings.Contains(pageTemplate, `name="change-saga-mutation-token"`) || !strings.Contains(appJavaScript, `X-Change-Saga-Mutation-Token`) || !strings.Contains(appJavaScript, `mutation_token:mutationToken`) {
		t.Fatal("browser mutation token plumbing is incomplete")
	}
}

// The browser suite proves the behavior; this pins the markup contract the
// behavior depends on so a template edit cannot quietly drop it.
func TestWorkspaceTabsAndClosedDrawerCarryAccessibleSemantics(t *testing.T) {
	for _, fragment := range []string{
		`role="tablist"`,
		`role="tab" id="view-tab-saga"`,
		`aria-controls="view-saga" aria-selected="true" tabindex="0"`,
		`aria-controls="view-code" aria-selected="false" tabindex="-1"`,
		`id="view-saga" role="tabpanel" aria-labelledby="view-tab-saga"`,
		`id="view-code" role="tabpanel" aria-labelledby="view-tab-code"`,
		`id="view-manifest" role="tabpanel" aria-labelledby="view-tab-manifest"`,
		`<aside class="diff-drawer" aria-hidden="true" inert`,
		`data-open-fragment="{{.Anchor}}"`,
	} {
		if !strings.Contains(pageTemplate, fragment) {
			t.Errorf("page template is missing accessible chrome markup %q", fragment)
		}
	}
	for _, fragment := range []string{
		"tab.setAttribute('aria-selected', String(selected))",
		"tab.tabIndex = selected ? 0 : -1",
		"drawer.setAttribute('inert', '')",
		"removeAttribute('inert')",
		"openDrawer(drawerButton.dataset.openDiffs, drawerButton)",
		"openFragmentDrawer(fragmentDrawerLink.dataset.openFragment, fragmentDrawerLink)",
		"hydrateTargetCode(targetCodeButton)",
		"data-target-code-response",
		"drawer.setAttribute('aria-label', mode === 'fragment' ? 'Related explanation' : 'Linked code')",
	} {
		if !strings.Contains(appJavaScript, fragment) {
			t.Errorf("browser script no longer maintains %q", fragment)
		}
	}
}

func TestListenRefusesNonLoopbackAddressBeforeServing(t *testing.T) {
	err := Listen(context.Background(), filepath.Join(t.TempDir(), "missing.saga"), "", "0.0.0.0:0", false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("Listen error = %v, want explicit non-loopback refusal", err)
	}
}

func TestManagedRuntimeEndpointsRequireTokenAndSignalShutdown(t *testing.T) {
	stopped := make(chan struct{}, 1)
	application := &app{shutdownToken: "private-token", shutdown: func() { stopped <- struct{}{} }}
	handler := newMux(application)

	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/runtime", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"ok":true`) {
		t.Fatalf("runtime status = %d %q", status.Code, status.Body.String())
	}

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/api/runtime-stop", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated stop = %d", denied.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/runtime-stop", nil)
	request.Header.Set("X-Change-Saga-Shutdown", "private-token")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusOK {
		t.Fatalf("authenticated stop = %d %q", accepted.Code, accepted.Body.String())
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("authenticated stop did not signal shutdown")
	}
}

func TestColdComparisonEndpointReportsBuildingCacheWithoutMaterializingReviewData(t *testing.T) {
	application := &app{}
	application.cache.building = true
	handler := newMux(application)

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/api/coverage", nil))
	if page.Code != http.StatusAccepted || !strings.Contains(page.Body.String(), "Building review cache") {
		t.Fatalf("cold comparison endpoint = %d %q, want explicit building-cache response", page.Code, page.Body.String())
	}
	if page.Header().Get("Retry-After") == "" {
		t.Fatal("building-cache response did not tell the browser when to retry")
	}

	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/runtime", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"cache":"building"`) {
		t.Fatalf("cold runtime status = %d %q", status.Code, status.Body.String())
	}
}

func TestMultipartLimitsSniffingAndCleanup(t *testing.T) {
	tempDir := t.TempDir()
	attachmentTempDir = tempDir
	t.Cleanup(func() { attachmentTempDir = "" })

	oversized := multipartFileRequest(t, "/api/thread", "large.txt", bytes.Repeat([]byte("x"), maxAttachmentBytes+1))
	if _, _, err := parseMultipart(oversized, httptest.NewRecorder()); !errors.Is(err, errUploadTooLarge) {
		t.Fatalf("oversized attachment error = %v", err)
	}
	assertEmptyDirectory(t, tempDir)

	totalOversized := multipartFilesRequest(t, "/api/thread", map[string][]byte{
		"one.txt":   bytes.Repeat([]byte("a"), 9<<20),
		"two.txt":   bytes.Repeat([]byte("b"), 9<<20),
		"three.txt": bytes.Repeat([]byte("c"), 9<<20),
		"four.txt":  bytes.Repeat([]byte("d"), 9<<20),
	})
	if _, _, err := parseMultipart(totalOversized, httptest.NewRecorder()); !errors.Is(err, errUploadTooLarge) {
		t.Fatalf("total oversized multipart error = %v", err)
	}
	assertEmptyDirectory(t, tempDir)

	hiddenExecutable := multipartFileRequest(t, "/api/thread", "fake.png", []byte("#!/bin/sh\necho nope\n"))
	if _, _, err := parseMultipart(hiddenExecutable, httptest.NewRecorder()); !errors.Is(err, errInvalidUpload) {
		t.Fatalf("content mismatch error = %v", err)
	}
	assertEmptyDirectory(t, tempDir)

	valid := multipartFileRequest(t, "/api/thread", "note.txt", []byte("hello\n"))
	paths, cleanup, err := parseMultipart(valid, httptest.NewRecorder())
	if err != nil || len(paths) != 1 {
		t.Fatalf("valid upload paths=%v err=%v", paths, err)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("staged upload missing before cleanup: %v", err)
	}
	cleanup()
	assertEmptyDirectory(t, tempDir)
}

func TestBrowserErrorsDoNotExposeFilesystemPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private", "missing.saga")
	recorder := httptest.NewRecorder()
	(&app{root: root}).page(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("page status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), root) || strings.Contains(recorder.Body.String(), "no such file") {
		t.Fatalf("browser error exposed an internal path: %q", recorder.Body.String())
	}
}

func TestCreateAnchoredThreadWritesOverlayRecords(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	fields := map[string]string{
		"target":    "urn:change-saga:test:fragment:overview",
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
		"target": "urn:change-saga:test:fragment:overview",
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

func TestWithdrawnThreadIsHiddenUntilReopened(t *testing.T) {
	root := validServerSaga(t)
	threadID, err := reviewstore.AddThread(root, "urn:change-saga:test:fragment:overview", "Temporarily hidden", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewstore.SetState(root, threadID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("page").Parse(`{{define "fragment"}}{{len .Threads}}{{end}}`))
	application := &app{root: root, sourceDir: root, template: tmpl}
	render := func() string {
		recorder := httptest.NewRecorder()
		application.fragmentContent(recorder, fragmentRequest("urn:change-saga:test:fragment:overview"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("explanation status = %d: %s", recorder.Code, recorder.Body.String())
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

func TestAnnotationCommentsBecomeBubblesAndOtherCommentsKeepTheirList(t *testing.T) {
	root := validServerSaga(t)
	target := "urn:change-saga:test:fragment:overview"
	fragmentThread, err := reviewstore.AddThread(root, target, "Comment on the whole explanation", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rectangleThread, err := reviewstore.AddThread(root, target, "Comment on the rectangle", saga.Anchor{
		Type: "region", Coordinate: "normalized",
		Shapes: []saga.Shape{{Type: "rect", X: .2, Y: .3, Width: .25, Height: .1}},
	}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	noteThread, err := reviewstore.AddThread(root, target, "Comment on the sticky note", saga.Anchor{
		Type: "note", Coordinate: "normalized", Note: &saga.NoteSelector{Text: "Placed", X: .6, Y: .4},
	}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	highlightThread, err := reviewstore.AddThread(root, target, "Comment on the highlight", saga.Anchor{
		Type: "text", Text: &saga.TextSelector{Exact: "Story"},
	}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	(&app{root: root, sourceDir: root, template: tmpl}).fragmentContent(recorder, fragmentRequest(target))
	if recorder.Code != http.StatusOK {
		t.Fatalf("explanation status = %d: %s", recorder.Code, recorder.Body.String())
	}
	page := recorder.Body.String()

	for _, thread := range []string{rectangleThread, noteThread, highlightThread} {
		if !strings.Contains(page, `data-annotation-bubble data-thread-id="`+thread+`"`) {
			t.Fatalf("annotation comment %s did not render as a bubble", thread)
		}
		panel := `id="` + domID("thread:"+thread) + `--bubble" data-annotation-bubble-panel hidden`
		if !strings.Contains(page, panel) {
			t.Fatalf("bubble for %s did not render a hidden comment panel", thread)
		}
	}
	if strings.Contains(page, `data-annotation-bubble data-thread-id="`+fragmentThread+`"`) {
		t.Fatal("a comment on the whole explanation must not become a bubble")
	}
	if !strings.Contains(page, "Comment on the whole explanation") {
		t.Fatal("the whole-explanation comment disappeared from the page")
	}

	// The bubble sits at the top-right corner of the rectangle it belongs to.
	if !strings.Contains(page, `style="left:45.0000%;top:30.0000%"`) {
		t.Fatal("the rectangle bubble was not placed on its rectangle")
	}
	// A highlight has no stored geometry, so it carries no server placement and
	// the browser measures the rendered mark instead.
	highlight := page[strings.Index(page, `data-thread-id="`+highlightThread+`" data-anchor-type="text"`):]
	if strings.Contains(highlight[:strings.Index(highlight, ">")], "style=") {
		t.Fatal("a highlight bubble must be placed by the browser, not by the server")
	}

	// Every comment list on the page is either inside a bubble panel or below
	// the content, and exactly one is below the content: the comment that was
	// never drawn onto anything.
	lists := strings.Count(page, `<div class="threads">`)
	inBubbles := strings.Count(page, `data-annotation-bubble-panel hidden><div class="threads">`)
	if inBubbles != 3 || lists-inBubbles != 1 {
		t.Fatalf("comment lists = %d with %d in bubbles, want 4 with 3 in bubbles", lists, inBubbles)
	}
}

// A permalink can name a heading, a marked place, or a comment inside a chapter
// that has not been fetched yet. The browser cannot scroll to what is not there,
// so the server answers where one anchor lives — and answers it for the derived
// anchors too, because a heading id and a landmark id are both suffixes of the
// explanation that owns them.
func TestDeferredAnchorsResolveToTheirChapterAndExplanation(t *testing.T) {
	root := validServerSaga(t)
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "chapter.json"), `{"version":2,"id":"alpha","title":"Alpha"}`)
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "story.fragment", "fragment.json"), `{"version":2,"id":"alpha-story","title":"Alpha story","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "story.fragment", "content.md"), "# Deep heading {#deep}\n\nAlpha narrative.\n")
	writeServerFile(t, filepath.Join(root, "alpha.chapter", "story.fragment", "___landmarks", "place.landmark", "landmark.json"),
		`{"version":2,"id":"place","label":"A marked place","selector":{"type":"text","exact":"Alpha"},"target":""}`)
	chapterTarget := saga.ChapterTarget("test", "alpha")
	fragmentTarget := "urn:change-saga:test:fragment:alpha-story"
	threadID, err := reviewstore.AddThread(root, fragmentTarget, "A comment inside a closed chapter", saga.Anchor{Type: "target"}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	application := &app{root: root, sourceDir: root, template: serverTemplate(t)}

	place := func(anchor string) (map[string]string, int) {
		recorder := httptest.NewRecorder()
		application.locateAnchor(recorder, httptest.NewRequest(http.MethodGet, "/api/locate?anchor="+url.QueryEscape(anchor), nil))
		found := map[string]string{}
		if recorder.Code == http.StatusOK {
			if err := json.Unmarshal(recorder.Body.Bytes(), &found); err != nil {
				t.Fatalf("the anchor response was not JSON: %v", err)
			}
		}
		return found, recorder.Code
	}

	fragmentID := domID(fragmentTarget)
	for name, anchor := range map[string]string{
		"the explanation itself": fragmentID,
		"a heading inside it":    fragmentID + "--deep",
		"a marked place":         fragmentID + "--place",
		"a comment on it":        domID("thread:" + threadID),
	} {
		found, status := place(anchor)
		if status != http.StatusOK {
			t.Fatalf("%s did not resolve: status=%d", name, status)
		}
		if found["chapter"] != domID(chapterTarget) || found["fragment"] != fragmentID {
			t.Fatalf("%s resolved to %#v, want chapter %s and explanation %s", name, found, domID(chapterTarget), fragmentID)
		}
	}

	// A chapter's own anchor needs no explanation fetched, and the overview
	// belongs to no chapter at all.
	if found, status := place(domID(chapterTarget)); status != http.StatusOK || found["chapter"] != domID(chapterTarget) || found["fragment"] != "" {
		t.Fatalf("a chapter anchor resolved to %#v (status %d)", found, status)
	}
	if found, status := place(domID(saga.SagaTarget("test"))); status != http.StatusOK || found["chapter"] != "" {
		t.Fatalf("the overview was placed inside a chapter: %#v (status %d)", found, status)
	}
	if _, status := place("target-not-a-real-anchor-abcdef"); status != http.StatusNotFound {
		t.Fatalf("an unknown anchor returned %d, want 404", status)
	}
	recorder := httptest.NewRecorder()
	application.locateAnchor(recorder, httptest.NewRequest(http.MethodGet, "/api/locate", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a missing anchor returned %d, want 400", recorder.Code)
	}
}

// fragmentRequest asks for one explanation's content the way the browser does
// once the shell has told it the explanation exists.
func fragmentRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/fragment?target="+url.QueryEscape(target), nil)
}

// sectionRequest asks for one chapter's body the way the browser does when a
// reviewer opens that chapter.
func sectionRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/section?target="+url.QueryEscape(target), nil)
}

func TestAnnotationBubblePointFollowsTheMark(t *testing.T) {
	for name, testCase := range map[string]struct {
		anchor saga.Anchor
		x, y   float64
		placed bool
	}{
		"rectangle uses its top-right corner": {
			anchor: saga.Anchor{Type: "region", Shapes: []saga.Shape{{Type: "rect", X: .1, Y: .2, Width: .3, Height: .4}}},
			x:      .4, y: .2, placed: true,
		},
		"freehand uses the extremes of its points": {
			anchor: saga.Anchor{Type: "drawing", Shapes: []saga.Shape{{Type: "path", Points: []saga.Point{{X: .5, Y: .6}, {X: .2, Y: .1}}}}},
			x:      .5, y: .1, placed: true,
		},
		"several shapes share one bubble": {
			anchor: saga.Anchor{Type: "drawing", Shapes: []saga.Shape{
				{Type: "rect", X: .1, Y: .5, Width: .1, Height: .1},
				{Type: "path", Points: []saga.Point{{X: .8, Y: .2}}},
			}},
			x: .8, y: .2, placed: true,
		},
		"an ellipse spans its radii": {
			anchor: saga.Anchor{Type: "region", Shapes: []saga.Shape{{Type: "ellipse", X: .5, Y: .5, Width: .2, Height: .1}}},
			x:      .7, y: .4, placed: true,
		},
		"a line spans its endpoints": {
			anchor: saga.Anchor{Type: "region", Shapes: []saga.Shape{{Type: "line", X: .8, Y: .9, Width: .2, Height: .1}}},
			x:      .8, y: .1, placed: true,
		},
		"a sticky note uses its own placement": {
			anchor: saga.Anchor{Type: "note", Note: &saga.NoteSelector{X: .25, Y: .75}},
			x:      .25, y: .75, placed: true,
		},
		"a mark outside the stage is pulled back onto it": {
			anchor: saga.Anchor{Type: "region", Shapes: []saga.Shape{{Type: "rect", X: .9, Y: -.4, Width: .5, Height: .2}}},
			x:      1, y: 0, placed: true,
		},
		"a highlight is placed by the browser": {
			anchor: saga.Anchor{Type: "text", Text: &saga.TextSelector{Exact: "quote"}},
		},
		"an empty annotation is placed by the browser": {
			anchor: saga.Anchor{Type: "region"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			x, y, placed := annotationBubblePoint(testCase.anchor)
			if placed != testCase.placed || x != testCase.x || y != testCase.y {
				t.Fatalf("bubble point = (%v, %v, %v), want (%v, %v, %v)", x, y, placed, testCase.x, testCase.y, testCase.placed)
			}
		})
	}
}

func TestAnnotationAnchorsAreExactlyTheMarksDrawnOnContent(t *testing.T) {
	for kind, want := range map[string]bool{
		"region": true, "drawing": true, "text": true, "note": true,
		"target": false, "diff": false, "": false,
	} {
		if got := annotationAnchor(kind); got != want {
			t.Errorf("annotationAnchor(%q) = %v, want %v", kind, got, want)
		}
	}
	if got := annotationBubbleLabel("note"); got != "sticky note" {
		t.Errorf("sticky note bubble label = %q", got)
	}
	if got := annotationBubbleLabel("region"); got != anchorLabel("region") {
		t.Errorf("rectangle bubble label = %q, want the shared reviewer word %q", got, anchorLabel("region"))
	}
}

func TestThreadAnchorEditPersistsWithoutChangingState(t *testing.T) {
	root := validServerSaga(t)
	threadID, err := reviewstore.AddThread(root, "urn:change-saga:test:fragment:overview", "Move this", saga.Anchor{Type: "region", Coordinate: "normalized", Shapes: []saga.Shape{{Type: "rect", X: .1, Y: .1, Width: .2, Height: .2}}}, "comment", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"thread": {threadID},
		"anchor": {`{"type":"drawing","coordinate_space":"normalized","shapes":[{"type":"path","points":[{"x":0.2,"y":0.3},{"x":0.4,"y":0.5}],"color":"#123456"}]}`},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/thread-anchor", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	(&app{root: root}).threadAnchor(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("anchor edit status = %d: %s", recorder.Code, recorder.Body.String())
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid || len(document.Threads) != 1 {
		t.Fatalf("edited annotation should load: validation=%#v err=%v", validation, err)
	}
	thread := document.Threads[0]
	if thread.State != "open" || thread.Anchor.Type != "drawing" || thread.Anchor.Shapes[0].Points[0].X != .2 || len(thread.Events) != 1 {
		t.Fatalf("unexpected edited annotation: %#v", thread)
	}
}

func TestStickyNoteThreadCommitsPlacementAndAppendsEdits(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	const noteText = "Rename <this> helper"
	request := multipartRequest(t, "/api/thread", map[string]string{
		"target": "urn:change-saga:test:fragment:overview",
		"body":   noteText,
		"anchor": `{"type":"note","coordinate_space":"normalized","note":{"text":"Rename <this> helper","x":0.25,"y":0.5,"color":"#f2bd4b"}}`,
	})
	recorder := httptest.NewRecorder()
	application.createThread(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("sticky note status = %d: %s", recorder.Code, recorder.Body.String())
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid || len(document.Threads) != 1 {
		t.Fatalf("committed sticky note should validate: validation=%#v err=%v", validation, err)
	}
	thread := document.Threads[0]
	if thread.Anchor.Type != "note" || thread.Anchor.Note == nil || thread.Anchor.Note.Text != noteText || thread.Anchor.Note.X != .25 || thread.Anchor.Note.Y != .5 || thread.Anchor.Note.Color != "#f2bd4b" {
		t.Fatalf("unexpected sticky note anchor: %#v", thread.Anchor)
	}
	if thread.Kind != "comment" || len(thread.Messages) != 1 {
		t.Fatalf("a sticky note should commit as a comment thread with one message: %#v", thread)
	}
	manifestPath := filepath.Join(thread.Directory, "thread.json")
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{
		"thread": {thread.ID},
		"anchor": {`{"type":"note","coordinate_space":"normalized","note":{"text":"Renamed already","x":0.8,"y":0.1,"color":"#3366cc"}}`},
	}
	edit := httptest.NewRequest(http.MethodPost, "/api/thread-anchor", strings.NewReader(values.Encode()))
	edit.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	editRecorder := httptest.NewRecorder()
	application.threadAnchor(editRecorder, edit)
	if editRecorder.Code != http.StatusNoContent {
		t.Fatalf("sticky note edit status = %d: %s", editRecorder.Code, editRecorder.Body.String())
	}
	document, validation, err = saga.Load(root)
	if err != nil || !validation.Valid || len(document.Threads) != 1 {
		t.Fatalf("edited sticky note should validate: validation=%#v err=%v", validation, err)
	}
	edited := document.Threads[0]
	if edited.State != "open" || len(edited.Events) != 1 || edited.Anchor.Note.Text != "Renamed already" || edited.Anchor.Note.X != .8 || edited.Anchor.Note.Color != "#3366cc" {
		t.Fatalf("unexpected edited sticky note: %#v", edited)
	}
	if manifestAfter, err := os.ReadFile(manifestPath); err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("editing a sticky note rewrote its original thread record")
	}
	if len(edited.Messages) != 1 {
		t.Fatalf("editing note text should not add a message: %#v", edited.Messages)
	}
}

func TestStickyNoteThreadRejectsUnplaceableNote(t *testing.T) {
	root := validServerSaga(t)
	request := multipartRequest(t, "/api/thread", map[string]string{
		"target": "urn:change-saga:test:fragment:overview",
		"body":   "Off canvas",
		"anchor": `{"type":"note","coordinate_space":"normalized","note":{"text":"Off canvas","x":1.4,"y":0.5}}`,
	})
	recorder := httptest.NewRecorder()
	(&app{root: root}).createThread(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("off-canvas sticky note status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestStickyNoteOverlayRendersSafelyAndDeepLinks(t *testing.T) {
	tmpl := serverTemplate(t)
	fragmentDir := t.TempDir()
	writeServerFile(t, filepath.Join(fragmentDir, "content.md"), "# Story {#story}\n")
	fragment := &saga.Fragment{ID: "overview", Title: "Overview", Target: "urn:change-saga:test:fragment:overview", Directory: fragmentDir, MediaType: "text/markdown", Entrypoint: "content.md"}
	section := &saga.Section{Kind: "chapter", ID: "root", Title: "Test", Target: "urn:change-saga:test:saga", Path: "private/root.chapter", Fragments: []*saga.Fragment{fragment}}
	created := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	sticky := &saga.Thread{ID: "sticky", Target: fragment.Target, CreatedBy: "Ada Lovelace", State: "open", CreatedAt: created,
		Anchor:   saga.Anchor{Type: "note", Coordinate: "normalized", Note: &saga.NoteSelector{Text: "Rename <this> helper", X: .25, Y: .5, Color: "#f2bd4b"}},
		Messages: []*saga.Message{{ID: "message", CreatedAt: created}}}
	hostile := &saga.Thread{ID: "hostile", Target: fragment.Target, State: "open", CreatedAt: created,
		Anchor:   saga.Anchor{Type: "note", Coordinate: "normalized", Note: &saga.NoteSelector{Text: "Unstyled", X: 0, Y: 0, Color: "expression(alert(1))"}},
		Messages: []*saga.Message{{ID: "hostile-message", CreatedAt: created}}}
	threads := map[string][]*threadView{fragment.Target: {makeThreadView(sticky), makeThreadView(hostile)}}
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/a.git", Base: "main", Head: "HEAD"}}, Section: section},
		Root: makeSectionView(section, viewScope{threads: threads}), Code: &CodeReviewView{},
	}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	page := output.String()
	noteID := domID("thread:sticky") + "--note"
	if strings.Contains(page, "ZgotmplZ") || !strings.Contains(page, `class="sticky-note"`) || !strings.Contains(page, `data-sticky-note`) {
		t.Fatal("sticky note overlay was not rendered")
	}
	if !strings.Contains(page, "--note-color:#f2bd4b;left:25.0000%;top:50.0000%") || !strings.Contains(page, `data-x="0.25"`) {
		t.Fatal("sticky note placement was not rendered from normalized coordinates")
	}
	if strings.Contains(page, "Rename <this> helper") || !strings.Contains(page, "Rename &lt;this&gt; helper") {
		t.Fatal("sticky note text was not rendered safely")
	}
	if strings.Contains(page, "expression(alert(1))") || strings.Count(page, "--note-color:#f2bd4b") < 2 {
		t.Fatal("an unsafe note color was not replaced with the accessible default")
	}
	if !strings.Contains(page, `id="`+noteID+`"`) || !strings.Contains(page, `data-copy-link="#`+noteID+`"`) {
		t.Fatal("sticky note was not independently hyperlinkable")
	}
	if !strings.Contains(page, `data-tool="sticky"`) || !strings.Contains(page, `class="note-anchor"`) || !strings.Contains(page, `tabindex="0" role="note"`) {
		t.Fatal("sticky tool, thread echo, or keyboard affordance was missing")
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
		"target":      "urn:change-saga:test:fragment:overview",
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
		"target":    {"urn:change-saga:test:fragment:overview"},
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

func TestAsyncReviewDecisionPersistsWithoutRedirect(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root}
	values := url.Values{
		"target": {"urn:change-saga:test:fragment:overview"},
		"state":  {"rejected"},
		"body":   {"Please cover the failure path."},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Change-Saga-Async", "true")
	recorder := httptest.NewRecorder()
	application.review(recorder, request)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Location") != "" {
		t.Fatalf("async review response = %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("written async review should validate: validation=%#v err=%v", validation, err)
	}
	reviews := document.Section.Fragments[0].Reviews
	if len(reviews) != 1 || reviews[0].State != "rejected" || reviews[0].Body != "Please cover the failure path." {
		t.Fatalf("unexpected async review: %#v", reviews)
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

// Review identity is read from the saga's own repository, and committing review
// records changes it without changing a byte under the saga root. The snapshot
// is reused for as long as its inputs are observably unchanged, so its freshness
// check has to commit to that history as well as to those bytes — otherwise a
// reviewer keeps reading their own decision as uncommitted after recording it.
func TestCommittingReviewRecordsInvalidatesTheReviewSnapshot(t *testing.T) {
	source := t.TempDir()
	serverGit(t, source, "init", "-b", "main")
	serverGit(t, source, "config", "user.name", "Test")
	serverGit(t, source, "config", "user.email", "test@example.test")
	writeServerFile(t, filepath.Join(source, "base.txt"), "base\n")
	serverGit(t, source, "add", ".")
	serverGit(t, source, "commit", "-m", "base")
	base := strings.TrimSpace(serverGit(t, source, "rev-parse", "HEAD"))
	writeServerFile(t, filepath.Join(source, "app.go"), "package app\n")
	serverGit(t, source, "add", ".")
	serverGit(t, source, "commit", "-m", "feature")
	head := strings.TrimSpace(serverGit(t, source, "rev-parse", "HEAD"))
	repository, err := diffuri.FileRepository(source)
	if err != nil {
		t.Fatal(err)
	}

	// The saga lives in a different repository from the code it explains, which
	// is the shape that makes this a real hazard: committing the review leaves
	// the source comparison, and every saga byte, exactly as they were.
	sagaRepo := t.TempDir()
	serverGit(t, sagaRepo, "init", "-b", "main")
	root := filepath.Join(sagaRepo, "test.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"Test","source":{"repository":"`+repository+`","base":"`+base+`","head":"`+head+`"}}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	serverGit(t, sagaRepo, "add", ".")
	serverGitEnv(t, sagaRepo, []string{
		"GIT_AUTHOR_NAME=Author", "GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME=Author", "GIT_COMMITTER_EMAIL=author@example.test",
	}, "commit", "-m", "write the saga")

	tmpl := template.Must(template.New("page").Parse(`{{define "page"}}{{(index .Root.FragmentViews 0).ReviewAuthor}}|{{(index .Root.FragmentViews 0).ReviewDetail}}{{end}}`))
	application := &app{root: root, sourceDir: source, template: tmpl}
	render := func() string {
		recorder := httptest.NewRecorder()
		application.page(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("page status = %d: %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	// The cache has to actually be on, or nothing below is a test of anything.
	render()
	render()
	if application.outline.builds != 1 {
		t.Fatalf("two identical requests rebuilt the outline %d times; this fixture does not exercise reuse", application.outline.builds)
	}

	writeServerFile(t, filepath.Join(root, "overview.fragment", "___approvals", "review.json"), `{"version":2,"id":"review","author":"Payload Name","state":"approved","created_at":"2026-08-19T12:00:00Z"}`)
	if body := render(); !strings.Contains(body, gitattribution.Uncommitted) {
		t.Fatalf("a decision that is only on disk was attributed as if it were in history: %q", body)
	}

	serverGit(t, sagaRepo, "add", ".")
	serverGitEnv(t, sagaRepo, []string{
		"GIT_AUTHOR_NAME=Git Author", "GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME=Saga Reviewer", "GIT_COMMITTER_EMAIL=reviewer@example.test",
	}, "commit", "-m", "record the decision")
	if body := render(); !strings.Contains(body, "Saga Reviewer") || strings.Contains(body, gitattribution.Uncommitted) {
		t.Fatalf("a committed decision kept its uncommitted attribution; the snapshot outlived the history it was read from: %q", body)
	}
	if application.cache.builds != 0 {
		t.Fatalf("review file creation or attribution commit rebuilt coverage/diffs: builds=%d", application.cache.builds)
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
	fragment := &saga.Fragment{ID: "overview", Title: "Overview", Target: "urn:change-saga:test:fragment:overview", Directory: fragmentDir, MediaType: "text/markdown", Entrypoint: "content.md", Landmarks: []saga.Landmark{{Version: 2, ID: "story-text", Label: "Story text", Target: landmarkTarget, Selector: saga.LandmarkSelector{Type: "text", Exact: "Story"}}}}
	fragment.Reviews = []saga.Review{{State: "approved", Author: "Ada", AttributionDetail: "ada@example.test · committed abc123", Body: "Ready to merge.", CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}}
	emptyFragment := &saga.Fragment{ID: "empty", Title: "No changes", Target: "urn:change-saga:test:fragment:empty", Directory: fragmentDir, MediaType: "text/plain", Entrypoint: "missing.txt"}
	section := &saga.Section{Kind: "chapter", ID: "root", Title: "Test", Target: "urn:change-saga:test:saga", Path: "private/root.chapter", Fragments: []*saga.Fragment{fragment, emptyFragment}}
	thread := &saga.Thread{ID: "thread", Target: fragment.Target, Anchor: saga.Anchor{Type: "region", Coordinate: "normalized", Shapes: []saga.Shape{{Type: "rect", X: .1, Y: .2, Width: .3, Height: .4, Color: "#336699"}}}, State: "open", Messages: []*saga.Message{{ID: "message", CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}}}
	lineURI := "saga-diff://v1/line?base=aaa&end=1&head=product-bbb&path=app.go&repository=https%3A%2F%2Fexample.test%2Fa.git&side=new&start=1"
	fragment.Diffs = []saga.DiffFile{{Version: 2, Diffs: []saga.DiffReference{{URI: lineURI, Note: "Adds the package entrypoint so the example compiles."}}}}
	manifestFiles := []*ManifestFileView{{Path: "internal/app.go", AtomCount: 1, Added: 1, Covered: 1, HasDiff: true, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", AtomCount: 1, Excerpt: "package app", Href: CodeDiffURL("internal/app.go", lineURI), Covered: true, Owners: []*ManifestOwnerView{{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}}}}}}
	manifestFixture := &CoverageManifestView{
		Complete: true, Total: 1, Covered: 1, MappingCount: 1, Files: manifestFiles, Tree: makeManifestTree(manifestFiles),
		Targets: []*ManifestTargetView{{ManifestOwnerView: ManifestOwnerView{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}, AtomCount: 1, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", Excerpt: "package app", Href: CodeDiffURL("internal/app.go", lineURI)}}, Files: []*ManifestTargetFileView{{Path: "internal/app.go", AtomCount: 1, Added: 1, Href: CodeDiffURL("internal/app.go", ""), HasDiff: true, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", AtomCount: 1, Href: CodeDiffURL("internal/app.go", lineURI)}}}}}},
	}
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/a.git", Base: "main", Head: "HEAD"}}, Section: section},
		Root: makeSectionView(section, viewScope{
			changes: map[string][]gitdiff.Atom{
				fragment.Target: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}},
				landmarkTarget:  {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}},
			},
			threads: map[string][]*threadView{fragment.Target: {makeThreadView(thread)}},
		}),
		Code: &CodeReviewView{}, Manifest: manifestFixture, ReviewDecided: 2, ReviewTotal: 3,
		ReviewItems: []*reviewProgressItem{
			makeReviewProgressItem(section.Target, section.Title, "/#"+domID(section.Target), "", ""),
			makeReviewProgressItem(fragment.Target, fragment.Title, "/#"+domID(fragment.Target), "approved", "Ready to merge."),
			makeReviewProgressItem(emptyFragment.Target, emptyFragment.Title, "/#"+domID(emptyFragment.Target), "rejected", "Needs tests."),
		},
	}
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	renderedPage := output.String()
	// The page no longer carries diff rows, so the `saga-diff://` scheme it must
	// keep unmangled is asserted where those rows are now produced, in
	// TestFileDiffEndpointServesCoverageAndTargetedBodies.
	if strings.Contains(renderedPage, "ZgotmplZ") {
		t.Fatal("template produced an unsafe URL sentinel")
	}
	for _, expected := range []string{"/app.js", `x="100.00"`, `id="` + domID(fragment.Target) + `--story"`} {
		if !strings.Contains(renderedPage, expected) {
			t.Fatalf("template output is missing %q", expected)
		}
	}
	if strings.Count(renderedPage, `class="annotation-toolbox"`) != 1 || !strings.Contains(renderedPage, `data-annotation-tools="`+fragment.Target+`"`) || !strings.Contains(renderedPage, `aria-controls="annotation-toolbox"`) || !strings.Contains(renderedPage, `data-review-progress`) || !strings.Contains(renderedPage, `data-review-decided="2" data-review-total="3"`) || !strings.Contains(renderedPage, `aria-label="Review progress: 2 of 3 decisions"`) || !strings.Contains(renderedPage, `class="review-progress-segment approved"`) || !strings.Contains(renderedPage, `class="review-progress-segment rejected"`) || !strings.Contains(renderedPage, `class="review-progress-segment pending"`) || !strings.Contains(renderedPage, `data-review-progress-note="Ready to merge."`) || !strings.Contains(renderedPage, `Comment: Ready to merge.`) || !strings.Contains(renderedPage, `data-review-progress-tooltip`) || !strings.Contains(renderedPage, `href="/#`+domID(fragment.Target)+`"`) || !strings.Contains(renderedPage, `data-review-controls`) || !strings.Contains(renderedPage, `data-review-author="Ada"`) || !strings.Contains(renderedPage, `data-review-detail="ada@example.test · committed abc123"`) || !strings.Contains(renderedPage, `data-review-decision="approved" aria-pressed="true"`) || !strings.Contains(renderedPage, `data-review-comment`) || !strings.Contains(renderedPage, `data-review-note title="Ready to merge."`) || !strings.Contains(renderedPage, `i-approve-filled`) || !strings.Contains(renderedPage, `i-reject-filled`) || !strings.Contains(renderedPage, `data-shared-review-form`) || strings.Contains(renderedPage, `decision-dialog`) || strings.Contains(renderedPage, `class="review-form"`) {
		t.Fatal("fast inline review controls and progress were not rendered")
	}
	if !strings.Contains(renderedPage, `body data-saga-id="test"`) || !strings.Contains(renderedPage, `data-undo disabled`) || !strings.Contains(renderedPage, `data-redo disabled`) || strings.Contains(renderedPage, `name="record_history"`) {
		t.Fatal("annotation command history controls were not rendered")
	}
	if !strings.Contains(renderedPage, `data-annotation-selection`) || !strings.Contains(renderedPage, `data-annotation-entity`) || !strings.Contains(renderedPage, `data-thread-id="thread"`) || !strings.Contains(renderedPage, `data-shape-index="0"`) {
		t.Fatal("shape annotations were not rendered as selectable entities")
	}
	if !strings.Contains(renderedPage, `data-view-tab="manifest"`) || !strings.Contains(renderedPage, `data-review-surface="manifest"`) || !strings.Contains(renderedPage, `data-surface-href="/api/coverage"`) {
		t.Fatal("bounded coverage navigation was not rendered")
	}
	if strings.Contains(renderedPage, `data-manifest-panel="code"`) || strings.Contains(renderedPage, `class="manifest-range"`) {
		t.Fatal("the root page eagerly rendered coverage details")
	}
	// Coverage is an invariant, so a complete report earns no praise banner —
	// only failures and stale references are worth a reviewer's attention.
	for _, celebration := range []string{"Everything is accounted for", "Every source change has a live", "complete\"", "manifest-verdict"} {
		if strings.Contains(renderedPage, celebration) {
			t.Fatalf("complete coverage still congratulates the reviewer: %q", celebration)
		}
	}
	if strings.Contains(renderedPage, "Attached code") || strings.Contains(renderedPage, "Linked diffs</h2>") || !strings.Contains(renderedPage, `<strong>Linked code</strong>`) {
		t.Fatal("attached-code drawer retained redundant header chrome")
	}
	if !strings.Contains(renderedPage, `class="attached-file" data-file-diff-href=`) || !strings.Contains(renderedPage, `data-file-diff-rows`) || !strings.Contains(renderedPage, "Adds the package entrypoint so the example compiles.") || !strings.Contains(renderedPage, "Open in Code Diff") || strings.Contains(renderedPage, `<details class="attached-file" open`) || strings.Contains(renderedPage, "Linked ranges only") {
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

func TestMarkdownRendersSafeGFMWithStablePermalinks(t *testing.T) {
	rendered := string(markdownWithAnchors(`# Story {#stable-story}

| Before | After |
| --- | --- |
| **slow** | `+"`fast`"+` |

1. First
2. Second

The lease is renewed before its midpoint.[^lease-renewal]

[^lease-renewal]: The heartbeat path renews the lease before half its TTL elapses.

[safe](https://example.test) [unsafe](javascript:alert(1))

<script>alert("no")</script>
`, "fragment"))
	for _, expected := range []string{
		`id="fragment--stable-story"`,
		`data-copy-link="#fragment--stable-story"`,
		`<table>`,
		`<strong>slow</strong>`,
		`<code>fast</code>`,
		`<ol>`,
		`href="https://example.test"`,
		`id="fragment--fnref:1"`,
		`href="#fragment--fn:1"`,
		`class="footnote-ref"`,
		`The heartbeat path renews the lease before half its TTL elapses.`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("Markdown output is missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, `<script>`) || strings.Contains(rendered, `href="javascript:`) {
		t.Fatalf("Markdown output contains unsafe content:\n%s", rendered)
	}
	other := string(markdownWithAnchors("Another claim.[^lease-renewal]\n\n[^lease-renewal]: Other evidence.\n", "other-fragment"))
	if !strings.Contains(other, `id="other-fragment--fnref:1"`) || strings.Contains(other, `id="fragment--fnref:1"`) {
		t.Fatalf("footnote IDs were not namespaced per fragment:\n%s", other)
	}
}

// The first load is a shell: saga identity, coverage totals, the overview's
// explanations as descriptors, one summary per chapter, and the navigation
// outline. Everything below that is fetched from a bounded endpoint as the
// reviewer reaches it, so the page describes the story instead of containing it.
func TestPageHandlerShipsAChapterShellAndRedirectsLegacyRoutes(t *testing.T) {
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
	alphaTarget := saga.ChapterTarget("test", "alpha")
	overviewFragment := "urn:change-saga:test:fragment:overview"
	// The shell names every chapter and every explanation and carries the
	// content of none of them.
	for _, narrative := range []string{"Root-only introduction", "Alpha-exclusive narrative", "Beta-exclusive narrative"} {
		if strings.Contains(overviewBody, narrative) {
			t.Fatalf("first load carried narrative content it was only asked to describe: %q", narrative)
		}
	}
	if !strings.Contains(overviewBody, `href="#`+domID(alphaTarget)+`"`) ||
		!strings.Contains(overviewBody, `data-section-href="/api/section?target=`+template.HTMLEscapeString(url.QueryEscape(alphaTarget))+`"`) {
		t.Fatal("the shell did not describe its chapters as fetchable summaries")
	}
	if !strings.Contains(overviewBody, `data-review-target="`+alphaTarget+`"`) {
		t.Fatal("the chapter bar did not expose its review controls")
	}
	if strings.Contains(overviewBody, `data-chapter-review-directory`) {
		t.Fatal("the first-load shell eagerly carried a chapter review directory")
	}
	if !strings.Contains(overviewBody, `data-fragment-href="/api/fragment?target=`+template.HTMLEscapeString(url.QueryEscape(overviewFragment))+`"`) {
		t.Fatal("the overview did not describe its explanations as fetchable descriptors")
	}
	if strings.Count(overviewBody, `data-chapter-body hidden`) != 2 || strings.Count(overviewBody, `data-chapter-toggle aria-expanded="false"`) != 2 {
		t.Fatal("summarised chapters did not start collapsed")
	}

	// Opening a chapter fetches that chapter and nothing else, and the
	// explanations it names are themselves still descriptors.
	alpha := httptest.NewRecorder()
	application.sectionBody(alpha, sectionRequest(alphaTarget))
	if alpha.Code != http.StatusOK {
		t.Fatalf("chapter body status = %d: %s", alpha.Code, alpha.Body.String())
	}
	alphaBody := alpha.Body.String()
	alphaFragment := "urn:change-saga:test:fragment:alpha-story"
	if !strings.Contains(alphaBody, `data-fragment-href="/api/fragment?target=`+template.HTMLEscapeString(url.QueryEscape(alphaFragment))+`"`) {
		t.Fatalf("chapter body did not describe its explanations: %s", alphaBody)
	}
	if !strings.Contains(alphaBody, `data-chapter-review-directory`) ||
		!strings.Contains(alphaBody, `data-review-directory-target="`+alphaFragment+`"`) ||
		!strings.Contains(alphaBody, `href="#`+domID(alphaFragment)+`"`) {
		t.Fatalf("chapter body did not carry its bounded, navigable review directory: %s", alphaBody)
	}
	if strings.Count(alphaBody, `data-review-target="`+alphaFragment+`"`) != 2 {
		t.Fatalf("chapter explanation did not expose synchronized controls in its bar and directory: %s", alphaBody)
	}
	if strings.Contains(alphaBody, "Alpha-exclusive narrative") || strings.Contains(alphaBody, "Beta-exclusive narrative") {
		t.Fatal("a chapter body carried explanation content, or content from another chapter")
	}

	// Only the explanation endpoint produces content, and only for the one
	// explanation it was asked for.
	story := httptest.NewRecorder()
	application.fragmentContent(story, fragmentRequest(alphaFragment))
	if story.Code != http.StatusOK {
		t.Fatalf("explanation status = %d: %s", story.Code, story.Body.String())
	}
	if !strings.Contains(story.Body.String(), "Alpha-exclusive narrative") || strings.Contains(story.Body.String(), "Beta-exclusive narrative") {
		t.Fatalf("explanation response was not exactly the one explanation: %s", story.Body.String())
	}

	missingSection := httptest.NewRecorder()
	application.sectionBody(missingSection, sectionRequest(saga.ChapterTarget("test", "nowhere")))
	missingFragment := httptest.NewRecorder()
	application.fragmentContent(missingFragment, fragmentRequest("urn:change-saga:test:fragment:nowhere"))
	if missingSection.Code != http.StatusNotFound || missingFragment.Code != http.StatusNotFound {
		t.Fatalf("unknown targets did not 404: section=%d fragment=%d", missingSection.Code, missingFragment.Code)
	}

	chapterRequest := httptest.NewRequest(http.MethodGet, "/chapters/alpha", nil)
	chapterRequest.SetPathValue("chapter", "alpha")
	chapter := httptest.NewRecorder()
	application.page(chapter, chapterRequest)
	if chapter.Code != http.StatusFound || chapter.Header().Get("Location") != "/#"+domID(alphaTarget) {
		t.Fatalf("legacy chapter route did not redirect to its in-page target: status=%d location=%q", chapter.Code, chapter.Header().Get("Location"))
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/chapters/missing", nil)
	missingRequest.SetPathValue("chapter", "missing")
	missing := httptest.NewRecorder()
	application.page(missing, missingRequest)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing chapter status = %d, want 404", missing.Code)
	}
}

func TestFragmentContentNeverLoadsTheSourceComparison(t *testing.T) {
	root := validServerSaga(t)
	application := &app{root: root, sourceDir: root, template: serverTemplate(t)}
	application.comparisonLoader = func(context.Context) (*reviewSnapshot, error) {
		t.Fatal("narrative fragment requested the source comparison")
		return nil, nil
	}

	recorder := httptest.NewRecorder()
	application.fragmentContent(recorder, fragmentRequest("urn:change-saga:test:fragment:overview"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("explanation status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Story") {
		t.Fatal("comparison-independent fragment lost its narrative content")
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
	repository, err := diffuri.FileRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"Test","source":{"repository":"`+repository+`","base":"`+base+`","head":"HEAD"}}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Story\n")
	application := &app{root: root, sourceDir: repo, template: serverTemplate(t)}
	handler := newMux(application)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Overview") || !strings.Contains(recorder.Body.String(), "Code Diff") || strings.Contains(recorder.Body.String(), `<code>web/view.js</code>`) {
		t.Fatalf("root did not render the comparison-free shell: status=%d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/code?file=web%2Fview.js&limit=200", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "app.go") || !strings.Contains(recorder.Body.String(), `data-file-path="web/view.js"`) {
		t.Fatalf("incremental code page did not render expected file navigation: status=%d body=%s", recorder.Code, recorder.Body.String())
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
	request = httptest.NewRequest(http.MethodGet, "/api/code?diff="+url.QueryEscape(selectedURI), nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `data-file-path="web/view.js"`) {
		t.Fatalf("exact diff handler selection: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/code?file=missing.go", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown focused file status=%d", recorder.Code)
	}
}

func TestTargetCodeLoadsOneNarrativeMappingWithoutGlobalSnapshot(t *testing.T) {
	repo := t.TempDir()
	serverGit(t, repo, "init", "-b", "main")
	serverGit(t, repo, "config", "user.name", "Test")
	serverGit(t, repo, "config", "user.email", "test@example.test")
	writeServerFile(t, filepath.Join(repo, "base.txt"), "base\n")
	serverGit(t, repo, "add", "base.txt")
	serverGit(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(serverGit(t, repo, "rev-parse", "HEAD"))
	writeServerFile(t, filepath.Join(repo, "app.go"), "package app\n\nfunc Ready() bool { return true }\n")
	writeServerFile(t, filepath.Join(repo, "unrelated.go"), "package app\n")
	serverGit(t, repo, "add", "app.go", "unrelated.go")
	serverGit(t, repo, "commit", "-m", "feature")
	repository, err := diffuri.FileRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := gitdiff.Read(t.Context(), repo, repository, base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var appURI string
	for _, atom := range changes.Atoms {
		if atom.Path == "app.go" && atom.Kind == "line" {
			appURI = atom.URI
			break
		}
	}
	if appURI == "" {
		t.Fatal("fixture has no app.go change")
	}

	root := filepath.Join(repo, "linked.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"linked","title":"Linked","source":{"repository":"`+repository+`","base":"`+base+`","head":"HEAD"}}`)
	writeServerFile(t, filepath.Join(root, "story.fragment", "fragment.json"), `{"version":2,"id":"story","title":"Story","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeServerFile(t, filepath.Join(root, "story.fragment", "content.md"), "# Story\n")
	writeServerFile(t, filepath.Join(root, "story.fragment", "___diffs", "app.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q,"note":"Implements the ready path."}]}`, appURI))
	target := saga.FragmentTarget("linked", "story")
	application := &app{root: root, sourceDir: repo, template: serverTemplate(t)}
	application.comparisonLoader = func(context.Context) (*reviewSnapshot, error) {
		t.Fatal("target-scoped linked code requested the global comparison")
		return nil, nil
	}
	handler := newMux(application)

	fragment := httptest.NewRecorder()
	handler.ServeHTTP(fragment, fragmentRequest(target))
	if fragment.Code != http.StatusOK || !strings.Contains(fragment.Body.String(), `data-target-code-href="/api/target-code?target=`) || strings.Contains(fragment.Body.String(), `data-open-diffs=`) {
		t.Fatalf("narrative did not render a lazy linked-code control: status=%d body=%s", fragment.Code, fragment.Body.String())
	}

	summary := httptest.NewRecorder()
	handler.ServeHTTP(summary, httptest.NewRequest(http.MethodGet, "/api/target-code?target="+url.QueryEscape(target), nil))
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(), `data-open-diffs="diffs-`+domID(target)+`"`) || !strings.Contains(summary.Body.String(), "app.go") || !strings.Contains(summary.Body.String(), "Implements the ready path.") || strings.Contains(summary.Body.String(), "unrelated.go") {
		t.Fatalf("target code was not scoped to the authored mapping: status=%d body=%s", summary.Code, summary.Body.String())
	}

	file := httptest.NewRecorder()
	handler.ServeHTTP(file, httptest.NewRequest(http.MethodGet, "/api/file-diff?file=app.go&target="+url.QueryEscape(target), nil))
	if file.Code != http.StatusOK || !strings.Contains(file.Body.String(), "linked-evidence") || !strings.Contains(file.Body.String(), `data-target="`+target+`"`) || strings.Contains(file.Body.String(), "unrelated.go") {
		t.Fatalf("target file body lost its scoped evidence: status=%d body=%s", file.Code, file.Body.String())
	}
}

// Resume state is read from the document and the thread index, so a chapter
// reports where a reviewer left off before its body has ever been fetched.
func TestChapterResumeState(t *testing.T) {
	approved := saga.Review{State: "approved", CreatedAt: time.Unix(10, 0)}
	rejected := saga.Review{State: "rejected", CreatedAt: time.Unix(10, 0)}
	commented := &saga.Section{Kind: "chapter", ID: "commented", Title: "Commented",
		Fragments: []*saga.Fragment{{ID: "talked", Target: "urn:change-saga:test:fragment:talked"}}}
	chapters := []*saga.Section{
		{Kind: "chapter", ID: "done", Title: "Done", Reviews: []saga.Review{approved}},
		{Kind: "chapter", ID: "started", Title: "Started", Fragments: []*saga.Fragment{{ID: "part", Reviews: []saga.Review{rejected}}}},
		commented,
		{Kind: "chapter", ID: "new", Title: "New"},
	}
	threads := map[string][]*threadView{
		commented.Fragments[0].Target: {{Thread: &saga.Thread{ID: "open", Anchor: saga.Anchor{Type: "target"}}}},
	}
	statuses := make([]string, 0, len(chapters))
	for _, chapter := range chapters {
		status, _, _ := reviewProgress(chapter, threads)
		statuses = append(statuses, status)
	}
	if strings.Join(statuses, ",") != "Unreviewed,Needs changes,In progress,Unreviewed" {
		t.Fatalf("unexpected chapter resume states: %#v", statuses)
	}

	// An annotation is activity, but remains a signal separate from approval.
	drawn := map[string][]*threadView{
		commented.Fragments[0].Target: {{Thread: &saga.Thread{ID: "drawn", Anchor: saga.Anchor{Type: "region"}}}},
	}
	if status, _, _ := reviewProgress(commented, drawn); status != "In progress" {
		t.Fatalf("a drawn annotation was not counted as separate chapter activity: %q", status)
	}
	allApproved := &saga.Section{Kind: "chapter", ID: "complete", Title: "Complete",
		Fragments: []*saga.Fragment{{ID: "part", Target: "urn:change-saga:test:fragment:complete", Reviews: []saga.Review{approved}}}}
	if status, _, _ := reviewProgress(allApproved, nil); status != "Approved" {
		t.Fatalf("a chapter with every child approved reported %q", status)
	}
}

func TestChapterReviewDirectoryProjectsIndependentStatesAndSignals(t *testing.T) {
	decision := func(state string) []saga.Review {
		return []saga.Review{{State: state, CreatedAt: time.Unix(10, 0)}}
	}
	landmarkTarget := "urn:change-saga:test:landmark:diagram:edge"
	diagram := &saga.Fragment{
		ID: "diagram", Title: "Diagram", Target: "urn:change-saga:test:fragment:diagram", Reviews: decision("rejected"),
		Landmarks: []saga.Landmark{{ID: "edge", Target: landmarkTarget}},
	}
	child := &saga.Section{Kind: "section", ID: "details", Title: "Details", Target: "urn:change-saga:test:section:details", Reviews: decision("approved"),
		Fragments: []*saga.Fragment{{ID: "legacy-open", Title: "Legacy open", Target: "urn:change-saga:test:fragment:legacy-open", Reviews: decision("open")}}}
	chapter := &saga.Section{Kind: "chapter", ID: "chapter", Title: "Chapter", Target: "urn:change-saga:test:chapter:chapter",
		Reviews: decision("approved"), Fragments: []*saga.Fragment{diagram}, Children: []*saga.Section{child}}
	threads := map[string][]*threadView{
		diagram.Target: {{Thread: &saga.Thread{ID: "comment", Anchor: saga.Anchor{Type: "target"}}}},
		landmarkTarget: {{Thread: &saga.Thread{ID: "annotation", Anchor: saga.Anchor{Type: "region"}}}},
		child.Target:   {{Thread: &saga.Thread{ID: "section-comment", Anchor: saga.Anchor{Type: "target"}}}},
	}

	items := makeChapterReviewDirectory(chapter, threads)
	if len(items) != 3 {
		t.Fatalf("chapter directory has %d items, want 3: %#v", len(items), items)
	}
	if items[0].Target != diagram.Target || items[0].ReviewState != "rejected" || items[0].Status != "Changes requested" || items[0].CommentCount != 2 {
		t.Fatalf("diagram directory row lost its decision or combined discussion signal: %#v", items[0])
	}
	if items[1].Target != child.Target || items[1].ReviewState != "approved" || items[1].Status != "Approved" || items[1].CommentCount != 1 {
		t.Fatalf("nested section directory row is wrong: %#v", items[1])
	}
	if items[2].ReviewState != "" || items[2].Status != "Unreviewed" || items[2].StateClass != "unreviewed" {
		t.Fatalf("legacy open state was not projected to unreviewed: %#v", items[2])
	}
	for _, item := range items {
		if item.Target == chapter.Target {
			t.Fatal("legacy chapter approval became a directory item")
		}
		if item.Href != "#"+domID(item.Target) {
			t.Fatalf("directory item does not navigate to its exact target: %#v", item)
		}
	}
}

// The progress map counts approval-bearing items over the whole saga, including
// items inside chapters the shell has only summarised. Chapters themselves are
// containers and their legacy approval events do not add a decision.
func TestReviewDecisionProgressCountsTheWholeSaga(t *testing.T) {
	decision := func(state string) []saga.Review {
		return []saga.Review{{State: state, CreatedAt: time.Unix(10, 0)}}
	}
	chapter := &saga.Section{Kind: "chapter", ID: "chapter", Title: "Chapter", Target: "urn:change-saga:test:chapter:chapter",
		Fragments: []*saga.Fragment{{ID: "three", Title: "Three", Target: "urn:change-saga:test:fragment:three", Reviews: decision("approved")}}}
	root := &saga.Section{Kind: "saga", ID: "root", Title: "Root", Target: "urn:change-saga:test:saga", Reviews: decision("approved"),
		Fragments: []*saga.Fragment{
			{ID: "one", Title: "One", Target: "urn:change-saga:test:fragment:one", Reviews: decision("rejected")},
			{ID: "two", Title: "Two", Target: "urn:change-saga:test:fragment:two", Reviews: decision("open")},
		},
		Children: []*saga.Section{chapter}}

	items := makeReviewProgressItems(root)
	decided, total := reviewProgressSummary(items)
	if decided != 3 || total != 4 {
		t.Fatalf("review decision progress = %d/%d, want 3/4", decided, total)
	}
	if items[0].Href != "#"+domID(root.Target) || items[3].Href != "#"+domID(chapter.Fragments[0].Target) {
		t.Fatalf("review progress links do not navigate to their targets: %#v", items)
	}
	for _, item := range items {
		if item.Target == chapter.Target {
			t.Fatal("legacy chapter approval was counted as a current decision")
		}
	}
	if items[0].StateClass != "approved" || items[1].StateClass != "rejected" || items[2].StateClass != "pending" {
		t.Fatalf("review progress colors do not match decision state: %#v", items)
	}
}

// Stylesheet rules and the markup they target drift apart silently: a rule that
// matches nothing, or one that matches more than intended, breaks the design
// without breaking a render. These two pairs have both regressed before.
func TestStylesheetSelectorsMatchTheMarkupTheyTarget(t *testing.T) {
	// The disclosure chevron in the linked-code drawer is the shared glyph, so
	// the rule that sizes and rotates it must name that class.
	if !strings.Contains(pageStyles, ".attached-file[open]>summary .twisty{transform:rotate(90deg)}") || strings.Contains(pageStyles, ".attached-file-marker") {
		t.Fatal("linked-code drawer chevron rule does not match the rendered glyph")
	}
	// The chapter eyebrow is monospace; the fragment excerpt beneath it is
	// prose. A `.related-chapter>a` rule would silently capture both.
	if strings.Contains(pageStyles, ".related-chapter>a") {
		t.Fatal("chapter link rule also matches the prose excerpt beneath it")
	}
	if !strings.Contains(pageStyles, ".related-chapter-link{") || !strings.Contains(pageTemplate, `class="related-chapter-link"`) {
		t.Fatal("chapter link class is not applied in both the stylesheet and the template")
	}
}

// The sidebar is documentation navigation, not a view of storage: it lists the
// overview and every preloaded chapter while keeping chapter outlines collapsed.
func TestNavigationTreeReadsAsCollapsedDocumentationOutline(t *testing.T) {
	systemMap := &saga.Fragment{ID: "system-map", Title: "System map"}
	root := &saga.Section{ID: "root", Title: "Scaffold", Target: saga.SagaTarget("test"),
		Fragments: []*saga.Fragment{{ID: "overview", Title: "Overview"}, systemMap, {ID: "untitled"}},
		Children: []*saga.Section{
			{Kind: "chapter", ID: "format", Title: "Format", Target: saga.ChapterTarget("test", "format")},
			{Kind: "chapter", ID: "ui", Title: "Reviewer", Target: saga.ChapterTarget("test", "ui"),
				Fragments: []*saga.Fragment{{ID: "shell", Title: "Reviewer"}, systemMap}},
		}}

	nodes := makeNavTree(root, nil)
	if len(nodes) != 3 || nodes[0].Title != "Overview" || nodes[1].Title != "Format" || nodes[2].Title != "Reviewer" {
		t.Fatalf("unexpected navigation nodes: %#v", nodes)
	}
	// "Overview > Overview" is noise, and untitled content must not be exposed
	// under an internal identifier.
	if len(nodes[0].Children) != 1 || nodes[0].Children[0].Title != "System map" {
		t.Fatalf("overview outline was not deduplicated: %#v", nodes[0].Children)
	}
	if !nodes[0].Expanded || nodes[1].Expanded || nodes[2].Expanded {
		t.Fatal("only the open page may be expanded")
	}
	if nodes[1].StateLabel != "Unreviewed" || nodes[1].Href != sagaHref(saga.ChapterTarget("test", "format")) {
		t.Fatalf("chapter node is not navigation: %#v", nodes[1])
	}
	if nodes[2].Expanded || len(nodes[2].Children) != 1 || nodes[2].Children[0].Title != "System map" {
		t.Fatalf("collapsed chapter did not retain its navigable outline: %#v", nodes[2])
	}
}

func TestDOMIDIsStableAndCollisionResistantAfterReadablePrefix(t *testing.T) {
	prefix := "urn:change-saga:test:fragment:" + strings.Repeat("shared-prefix", 10)
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
	files := makeFileViews(changes, "urn:change-saga:test:saga", nil, nil)
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

func multipartFileRequest(t *testing.T, path, filename string, content []byte) *http.Request {
	return multipartFilesRequest(t, path, map[string][]byte{filename: content})
}

func multipartFilesRequest(t *testing.T, path string, files map[string][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for filename, content := range files {
		part, err := writer.CreateFormFile("attachment", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
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

func assertEmptyDirectory(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary upload files remain: %v", entries)
	}
}

func serverTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := newPageTemplate()
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
