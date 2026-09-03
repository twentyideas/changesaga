package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestSlideNativeTemplateRendersADeckInsteadOfChapters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "visual.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":4,"id":"visual","title":"Visual review","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "deck.json"), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.json"), `{"version":4,"id":"change","title":"What changed","rank":0,"intent":"orient","layout":"hero","media_type":"image/svg+xml","entrypoint":"slide.svg","takeaway":"Validation happens first.","reading_order":["premise"]}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.svg"), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720"><text id="premise">Validation happens first</text></svg>`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "___items", "premise.item", "item.json"), `{"version":4,"id":"premise","kind":"statement","label":"Premise","description":"Validation happens first.","selector":{"type":"element","element_id":"premise"}}`)
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
	for _, contract := range []string{`data-slide-native`, `data-native-slide`, `data-slide-thumbnail`, `data-slide-sidebar-toggle`, `data-slide-present`, `data-slide-exit-presentation`, `/f/change/slide.svg`} {
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
	for _, contract := range []string{"requestFullscreen", "fullscreenchange", "slide-sidebar-collapsed", "presentation-mode", "data-slide-thumbnail"} {
		if !strings.Contains(appJavaScript, contract) {
			t.Fatalf("slide interaction contract %q missing", contract)
		}
	}
}

func TestSlideNativeAssetRouteResolvesTheSlideTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "visual.saga")
	writeServerFile(t, filepath.Join(root, "saga.json"), `{"version":4,"id":"visual","title":"Visual review","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "deck.json"), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.json"), `{"version":4,"id":"change","title":"What changed","rank":0,"intent":"orient","layout":"hero","media_type":"image/svg+xml","entrypoint":"slide.svg","takeaway":"Validation happens first.","reading_order":["premise"]}`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.svg"), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720"><text id="premise">Validation happens first</text></svg>`)
	writeServerFile(t, filepath.Join(root, "overview.deck", "change.slide", "___items", "premise.item", "item.json"), `{"version":4,"id":"premise","kind":"statement","label":"Premise","description":"Validation happens first.","selector":{"type":"element","element_id":"premise"}}`)

	request := httptest.NewRequest(http.MethodGet, "/f/change/slide.svg", nil)
	recorder := httptest.NewRecorder()
	newMux(&app{root: root}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Validation happens first") {
		t.Fatalf("slide asset was not served: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
