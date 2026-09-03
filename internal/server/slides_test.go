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
	for _, contract := range []string{`data-slide-native`, `data-native-slide`, `data-slide-thumbnail`, `data-slide-sidebar-toggle`, `data-slide-present`, `data-slide-exit-presentation`, `/f/change/` + assetName} {
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
	assetName := writeFlatSlideFixture(t, root)

	request := httptest.NewRequest(http.MethodGet, "/f/change/"+assetName, nil)
	recorder := httptest.NewRecorder()
	newMux(&app{root: root}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Validation happens first") {
		t.Fatalf("slide asset was not served: status=%d body=%s", recorder.Code, recorder.Body.String())
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
