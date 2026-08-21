package server

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitattribution"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/reviewstore"
	"github.com/change-saga/change-saga/internal/saga"
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
		Root: makeSectionView(section, nil, threads, nil), Code: &CodeReviewView{},
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
	manifestFiles := []*ManifestFileView{{Path: "internal/app.go", AtomCount: 1, Added: 1, Covered: 1, Diff: &FileDiffView{Path: "internal/app.go", Lines: []*DiffLineView{{Kind: "new", NewLine: 1, Content: "package app"}}}, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", AtomCount: 1, Excerpt: "package app", Href: CodeDiffURL("internal/app.go", lineURI), Covered: true, Owners: []*ManifestOwnerView{{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}}}}}}
	manifestFixture := &CoverageManifestView{
		Complete: true, Total: 1, Covered: 1, MappingCount: 1, Files: manifestFiles, Tree: makeManifestTree(manifestFiles),
		Targets: []*ManifestTargetView{{ManifestOwnerView: ManifestOwnerView{Title: "Overview", Kind: "Fragment", Chapter: "Test", Href: "#overview"}, AtomCount: 1, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", Excerpt: "package app", Href: CodeDiffURL("internal/app.go", lineURI)}}, Files: []*ManifestTargetFileView{{Path: "internal/app.go", AtomCount: 1, Added: 1, Href: CodeDiffURL("internal/app.go", ""), Diff: manifestFiles[0].Diff, Chunks: []*ManifestChunkView{{Label: "+1", Path: "internal/app.go", AtomCount: 1, Href: CodeDiffURL("internal/app.go", lineURI)}}}}}},
	}
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "test", Title: "Test", Source: saga.Source{Repository: "https://example.test/a.git", Base: "main", Head: "HEAD"}}, Section: section},
		Root: makeSectionView(section, map[string][]gitdiff.Atom{fragment.Target: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}}, landmarkTarget: {{Kind: "line", URI: lineURI, Path: "app.go", Side: "new", Line: 1, Content: "package app"}}}, map[string][]*threadView{fragment.Target: {makeThreadView(thread)}}, nil),
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
	if strings.Contains(renderedPage, "ZgotmplZ") || !strings.Contains(renderedPage, "/app.js") || !strings.Contains(renderedPage, `x="100.00"`) || !strings.Contains(renderedPage, `data-diff-ref="saga-diff://v1/line?`) || !strings.Contains(renderedPage, `id="`+domID(fragment.Target)+`--story"`) {
		t.Fatalf("template produced unsafe or incomplete output")
	}
	if strings.Count(renderedPage, `class="annotation-toolbox"`) != 1 || !strings.Contains(renderedPage, `data-review-progress`) || !strings.Contains(renderedPage, `data-review-decided="2" data-review-total="3"`) || !strings.Contains(renderedPage, `aria-label="Review progress: 2 of 3 decisions"`) || !strings.Contains(renderedPage, `class="review-progress-segment approved"`) || !strings.Contains(renderedPage, `class="review-progress-segment rejected"`) || !strings.Contains(renderedPage, `class="review-progress-segment pending"`) || !strings.Contains(renderedPage, `data-review-progress-note="Ready to merge."`) || !strings.Contains(renderedPage, `Comment: Ready to merge.`) || !strings.Contains(renderedPage, `data-review-progress-tooltip`) || !strings.Contains(renderedPage, `href="/#`+domID(fragment.Target)+`"`) || !strings.Contains(renderedPage, `data-review-controls`) || !strings.Contains(renderedPage, `data-review-author="Ada"`) || !strings.Contains(renderedPage, `data-review-detail="ada@example.test · committed abc123"`) || !strings.Contains(renderedPage, `data-review-decision="approved" aria-pressed="true"`) || !strings.Contains(renderedPage, `data-review-decision-tooltip`) || !strings.Contains(renderedPage, `data-review-decision-author title="ada@example.test · committed abc123"`) || !strings.Contains(renderedPage, `data-review-comment`) || !strings.Contains(renderedPage, `data-review-note title="Ready to merge."`) || !strings.Contains(renderedPage, `i-approve-filled`) || !strings.Contains(renderedPage, `i-reject-filled`) || strings.Contains(renderedPage, `decision-dialog`) || strings.Contains(renderedPage, `class="review-form"`) {
		t.Fatal("fast inline review controls and progress were not rendered")
	}
	if !strings.Contains(renderedPage, `body data-saga-id="test"`) || !strings.Contains(renderedPage, `data-undo disabled`) || !strings.Contains(renderedPage, `data-redo disabled`) || strings.Contains(renderedPage, `name="record_history"`) {
		t.Fatal("annotation command history controls were not rendered")
	}
	if !strings.Contains(renderedPage, `data-annotation-selection`) || !strings.Contains(renderedPage, `data-annotation-entity`) || !strings.Contains(renderedPage, `data-thread-id="thread"`) || !strings.Contains(renderedPage, `data-shape-index="0"`) {
		t.Fatal("shape annotations were not rendered as selectable entities")
	}
	if !strings.Contains(renderedPage, `data-view-tab="manifest"`) || !strings.Contains(renderedPage, `data-manifest-panel="code"`) || !strings.Contains(renderedPage, "Code → Saga") || !strings.Contains(renderedPage, "Saga → Code") {
		t.Fatal("bidirectional coverage manifest was not rendered")
	}
	// Coverage is an invariant, so a complete report earns no praise banner —
	// only failures and stale references are worth a reviewer's attention.
	for _, celebration := range []string{"Everything is accounted for", "Every source change has a live", "complete\"", "manifest-verdict"} {
		if strings.Contains(renderedPage, celebration) {
			t.Fatalf("complete coverage still congratulates the reviewer: %q", celebration)
		}
	}
	if !strings.Contains(renderedPage, `<details class="manifest-folder" data-manifest-folder open`) || !strings.Contains(renderedPage, `data-manifest-search="internal/app.go"`) || !strings.Contains(renderedPage, `<use href="#f-go">`) {
		t.Fatal("coverage was not rendered as a fully expanded repository tree with file-type icons")
	}
	if !strings.Contains(renderedPage, `class="manifest-file-diff diff-surface"`) || !strings.Contains(renderedPage, `data-file-path="internal/app.go"`) || !strings.Contains(renderedPage, `class="diff-row new"`) || !strings.Contains(renderedPage, `data-code>package app</code>`) || !strings.Contains(renderedPage, `1 changed line`) {
		t.Fatal("expandable coverage file did not render its actual diff and compact mapping metadata")
	}
	if !strings.Contains(renderedPage, `class="manifest-target-file"`) || !strings.Contains(renderedPage, `class="manifest-target-file-detail"`) || !strings.Contains(renderedPage, `Open in Code Diff`) || strings.Contains(renderedPage, `class="manifest-target-row"`) {
		t.Fatal("saga-to-code coverage did not keep linked diffs expandable in place")
	}
	if strings.Contains(renderedPage, "Attached code") || strings.Contains(renderedPage, "Linked diffs</h2>") || !strings.Contains(renderedPage, `<strong>Linked code</strong>`) {
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

func TestPageHandlerPreloadsCollapsedChaptersAndRedirectsLegacyRoutes(t *testing.T) {
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
	if !strings.Contains(overviewBody, "Root-only introduction") || !strings.Contains(overviewBody, "Alpha-exclusive narrative") || !strings.Contains(overviewBody, "Beta-exclusive narrative") || !strings.Contains(overviewBody, `href="#`+domID(alphaTarget)+`"`) {
		t.Fatal("one-page saga did not preload its overview and chapters")
	}
	if strings.Count(overviewBody, `data-chapter-body hidden`) != 2 || strings.Count(overviewBody, `data-chapter-toggle aria-expanded="false"`) != 2 {
		t.Fatal("preloaded chapters did not start collapsed")
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

func TestChapterResumeState(t *testing.T) {
	root := &sectionView{ChildViews: []*sectionView{
		{Section: &saga.Section{Kind: "chapter", ID: "done", Title: "Done"}, ReviewState: "approved"},
		{Section: &saga.Section{Kind: "chapter", ID: "started", Title: "Started"}, FragmentViews: []*fragmentView{{ReviewState: "rejected"}}},
		{Section: &saga.Section{Kind: "chapter", ID: "new", Title: "New"}},
	}}
	statuses := make([]string, 0, len(root.ChildViews))
	for _, chapter := range root.ChildViews {
		status, _, _ := reviewProgress(chapter)
		statuses = append(statuses, status)
	}
	if strings.Join(statuses, ",") != "Approved,In progress,Unreviewed" {
		t.Fatalf("unexpected chapter resume states: %#v", statuses)
	}
}

func TestReviewDecisionProgressCountsTheWholeSaga(t *testing.T) {
	root := &sectionView{
		Section:     &saga.Section{Kind: "saga", ID: "root", Title: "Root", Target: "urn:change-saga:test:saga"},
		DOMID:       "root-target",
		ReviewState: "approved",
		FragmentViews: []*fragmentView{
			{Fragment: &saga.Fragment{ID: "one", Title: "One", Target: "urn:change-saga:test:fragment:one"}, DOMID: "one-target", ReviewState: "rejected"},
			{Fragment: &saga.Fragment{ID: "two", Title: "Two", Target: "urn:change-saga:test:fragment:two"}, DOMID: "two-target", ReviewState: "open"},
		},
		ChildViews: []*sectionView{{
			Section: &saga.Section{Kind: "chapter", ID: "chapter", Title: "Chapter", Target: "urn:change-saga:test:chapter:chapter"},
			DOMID:   "chapter-target",
			FragmentViews: []*fragmentView{
				{Fragment: &saga.Fragment{ID: "three", Title: "Three", Target: "urn:change-saga:test:fragment:three"}, DOMID: "three-target", ReviewState: "approved"},
			},
		}},
	}
	items := makeReviewProgressItems(root)
	decided, total := reviewProgressSummary(items)
	if decided != 3 || total != 5 {
		t.Fatalf("review decision progress = %d/%d, want 3/5", decided, total)
	}
	if items[0].Href != "#root-target" || items[3].Href != "#chapter-target" || items[4].Href != "#three-target" {
		t.Fatalf("review progress links do not navigate to their targets: %#v", items)
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
	overviewFragment := &fragmentView{Fragment: &saga.Fragment{ID: "overview", Title: "Overview"}}
	systemMap := &fragmentView{Fragment: &saga.Fragment{ID: "system-map", Title: "System map"}}
	untitled := &fragmentView{Fragment: &saga.Fragment{ID: "untitled"}}
	root := &sectionView{Section: &saga.Section{ID: "root", Title: "Scaffold", Target: saga.SagaTarget("test")}, FragmentViews: []*fragmentView{overviewFragment, systemMap, untitled}, ChildViews: []*sectionView{
		{Section: &saga.Section{Kind: "chapter", ID: "format", Title: "Format", Target: saga.ChapterTarget("test", "format")}},
		{Section: &saga.Section{Kind: "chapter", ID: "ui", Title: "Reviewer", Target: saga.ChapterTarget("test", "ui")}, FragmentViews: []*fragmentView{{Fragment: &saga.Fragment{ID: "shell", Title: "Reviewer"}}, systemMap}},
	}}

	nodes := makeNavTree("Scaffold", root)
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
