package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/reviewstore"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestSlideNativeTemplateRendersADeckInsteadOfChapters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "visual.saga")
	assetName := writeFlatSlideFixture(t, root)
	document, validation, err := saga.LoadNarrative(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load slides: valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	data := pageData{Saga: document, Root: makeSectionView(document.Section, viewScope{}), SlideNative: true}
	if err := tmpl.ExecuteTemplate(&rendered, "page", data); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, contract := range []string{`data-slide-native`, `data-native-slide`, `data-slide-thumbnail`, `role="img" data-slide-review-status`, `data-slide-sidebar-toggle`, `data-slide-present`, `data-slide-exit-presentation`, `/f/change/` + assetName} {
		if !strings.Contains(html, contract) {
			t.Fatalf("native slide contract %q missing:\n%s", contract, html)
		}
	}
	if !strings.Contains(html, `Visual review`) {
		t.Fatalf("native slide controls/content missing:\n%s", html)
	}
	if strings.Contains(html, `<h2>Chapters</h2>`) {
		t.Fatal("v4 renderer silently reused the report chapter surface")
	}
	for _, contract := range []string{"requestFullscreen", "fullscreenchange", "slide-sidebar-collapsed", "presentation-mode", "data-slide-thumbnail", "updateSlideReviewState"} {
		if !strings.Contains(appJavaScript, contract) {
			t.Fatalf("slide interaction contract %q missing", contract)
		}
	}
	for _, contract := range []string{
		`.slide-thumbnail-hit:hover{background:transparent}`,
		`body.presentation-mode .diff-drawer,body.presentation-mode .drawer-backdrop{display:none}`,
	} {
		if !strings.Contains(pageStyles, contract) {
			t.Fatalf("slide overlay isolation contract %q missing", contract)
		}
	}
	if !strings.Contains(appJavaScript, `if (q('.diff-drawer.open')) closeDrawer(false);`) {
		t.Fatal("presentation mode did not close the review drawer before entering fullscreen")
	}
}

func TestSlideNativeAssetRouteResolvesTheSlideTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "visual.saga")
	assetName := writeFlatSlideFixture(t, root)

	request := httptest.NewRequest(http.MethodGet, "/f/change/"+assetName, nil)
	recorder := httptest.NewRecorder()
	newMux(&app{root: root}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Validation happens first") {
		t.Fatalf("slide asset was not served: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSlideNativeCoverageAndActivityUseTheFlatReviewOverlay(t *testing.T) {
	repo := t.TempDir()
	serverGit(t, repo, "init", "-b", "main")
	serverGit(t, repo, "config", "user.name", "Test")
	serverGit(t, repo, "config", "user.email", "test@example.test")
	writeServerFile(t, filepath.Join(repo, "app.go"), "package app\n")
	serverGit(t, repo, "add", "app.go")
	serverGit(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(serverGit(t, repo, "rev-parse", "HEAD"))
	writeServerFile(t, filepath.Join(repo, "app.go"), "package app\n\nfunc Ready() bool { return true }\n")
	serverGit(t, repo, "add", "app.go")
	serverGit(t, repo, "commit", "-m", "feature")

	root := filepath.Join(repo, "visual.saga")
	writeFlatSlideFixture(t, root)
	repository, err := diffuri.FileRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeServerFile(t, filepath.Join(root, saga.FlatManifestName), `{"version":4,"id":"visual","title":"Visual review","source":{"repository":"`+repository+`","base":"`+base+`","head":"HEAD"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)

	index, validation, err := saga.LoadMutationIndex(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load mutation index: valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	before, err := indexedReviewFingerprint(t.Context(), index)
	if err != nil {
		t.Fatalf("fingerprint empty flat review state: %v", err)
	}

	application := &app{root: root, sourceDir: repo, template: serverTemplate(t)}
	handler := newMux(application)
	coverage := httptest.NewRecorder()
	handler.ServeHTTP(coverage, httptest.NewRequest(http.MethodGet, "/api/coverage", nil))
	if coverage.Code != http.StatusOK || !strings.Contains(coverage.Body.String(), `data-review-surface-response="manifest"`) {
		t.Fatalf("flat coverage status=%d body=%s", coverage.Code, coverage.Body.String())
	}

	slideTarget := saga.SlideTarget("visual", "change")
	itemTarget := saga.ItemTarget("visual", "change", "premise")
	decision := url.Values{"target": {slideTarget}, "state": {"approved"}, "body": {"The slide is clear."}}
	decisionResponse := httptest.NewRecorder()
	decisionRequest := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(decision.Encode()))
	decisionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(decisionResponse, decisionRequest)
	if decisionResponse.Code != http.StatusSeeOther {
		t.Fatalf("flat slide decision status=%d body=%s", decisionResponse.Code, decisionResponse.Body.String())
	}
	itemDecision := url.Values{"target": {itemTarget}, "state": {"approved"}}
	itemDecisionRequest := httptest.NewRequest(http.MethodPost, "/api/review", strings.NewReader(itemDecision.Encode()))
	itemDecisionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	itemDecisionResponse := httptest.NewRecorder()
	handler.ServeHTTP(itemDecisionResponse, itemDecisionRequest)
	if itemDecisionResponse.Code != http.StatusBadRequest {
		t.Fatalf("flat Item approval status=%d, want 400", itemDecisionResponse.Code)
	}
	if _, err := reviewstore.AddThread(root, itemTarget, "Keep this callout", saga.Anchor{Type: "target"}, "comment", "", nil); err != nil {
		t.Fatal(err)
	}
	after, err := indexedReviewFingerprint(t.Context(), index)
	if err != nil || after == before {
		t.Fatalf("flat review records did not advance fingerprint: before=%q after=%q err=%v", before, after, err)
	}

	outline, outlineValidation, err := saga.LoadOutline(root)
	if err != nil || !outlineValidation.Valid || len(outline.Decks[0].Slides[0].Items) != 1 {
		t.Fatalf("outline lost Item review targets: valid=%v err=%v issues=%#v", outlineValidation.Valid, err, outlineValidation.Issues)
	}
	activity := httptest.NewRecorder()
	handler.ServeHTTP(activity, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	if activity.Code != http.StatusOK {
		t.Fatalf("flat activity status=%d body=%s", activity.Code, activity.Body.String())
	}
	for _, expected := range []string{"The slide is clear.", "Keep this callout", "What changed", "Slide", "Premise", "Item"} {
		if !strings.Contains(activity.Body.String(), expected) {
			t.Fatalf("flat activity is missing %q: %s", expected, activity.Body.String())
		}
	}
	if got := strings.Count(activity.Body.String(), `class="activity-slide-preview"`); got != 2 {
		t.Fatalf("slide and Item activity should both carry their visual slide reference; got %d: %s", got, activity.Body.String())
	}

	coverage = httptest.NewRecorder()
	handler.ServeHTTP(coverage, httptest.NewRequest(http.MethodGet, "/api/coverage", nil))
	if coverage.Code != http.StatusOK {
		t.Fatalf("coverage failed after flat review mutation: status=%d body=%s", coverage.Code, coverage.Body.String())
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-review-total="1"`) || !strings.Contains(page.Body.String(), `data-slide-review-status data-review-target="`+slideTarget+`" data-review-state="approved"`) {
		t.Fatalf("slide approval did not reach progress and thumbnail state: status=%d body=%s", page.Code, page.Body.String())
	}
}

func writeFlatSlideFixture(t *testing.T, root string) string {
	t.Helper()
	writeServerFile(t, filepath.Join(root, saga.FlatManifestName), `{"version":4,"id":"visual","title":"Visual review","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	deckTarget := saga.DeckTarget("visual", "overview")
	deckName, _ := saga.FlatDeckFilename(deckTarget, 0)
	writeServerFile(t, filepath.Join(root, deckName), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	slideTarget := saga.SlideTarget("visual", "change")
	slideName, _ := saga.FlatSlideFilename(deckTarget, slideTarget, 0)
	assetName, _ := saga.FlatSlideAssetFilename(slideName, ".svg")
	writeServerFile(t, filepath.Join(root, slideName), `{"version":4,"id":"change","deck":"overview","title":"What changed","rank":0,"intent":"orient","layout":"hero","media_type":"image/svg+xml","entrypoint":"`+assetName+`","takeaway":"Validation happens first.","reading_order":["premise"]}`)
	writeServerFile(t, filepath.Join(root, assetName), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720"><text id="premise">Validation happens first</text></svg>`)
	itemTarget := saga.ItemTarget("visual", "change", "premise")
	itemName, _ := saga.FlatItemFilename(slideTarget, itemTarget, 0)
	writeServerFile(t, filepath.Join(root, itemName), `{"version":4,"id":"premise","slide":"change","rank":0,"kind":"statement","label":"Premise","description":"Validation happens first.","selector":{"type":"element","element_id":"premise"}}`)
	return assetName
}
