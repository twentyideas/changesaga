package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestRootTemplateDefersCodeAndCoverageModels(t *testing.T) {
	definition := strings.Index(pageTemplate, `{{define "code-view"}}`)
	if definition < 0 {
		t.Fatal("code response template is missing")
	}
	root := pageTemplate[:definition]
	for _, eager := range []string{`{{.Code`, `{{with .Code`, `{{.Manifest`, `{{with .Manifest`, `template "manifest-view"`, `template "code-view"`} {
		if strings.Contains(root, eager) {
			t.Errorf("root page still renders a bounded review model through %q", eager)
		}
	}
	for _, contract := range []string{
		`data-review-surface="code" data-surface-href="/api/code"`,
		`data-review-surface="manifest" data-surface-href="/api/coverage"`,
		`data-surface-status`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(root, contract) {
			t.Errorf("root page is missing deferred surface contract %q", contract)
		}
	}
}

func TestRootTemplateKeepsCoverageAvailableWithoutComparisonTotals(t *testing.T) {
	tmpl, err := newPageTemplate()
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{
		Saga: &saga.Saga{Manifest: saga.Manifest{ID: "cold", Title: "Cold review"}},
		Root: &sectionView{Section: &saga.Section{ID: "cold", Title: "Cold review"}, DOMID: "cold"},
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "page", data); err != nil {
		t.Fatal(err)
	}
	page := rendered.String()
	if !strings.Contains(page, `id="view-tab-manifest"`) || !strings.Contains(page, `data-review-surface="manifest"`) {
		t.Fatal("a cold root hid Coverage while its comparison totals were unavailable")
	}
	if strings.Contains(page, `data-coverage-totals`) {
		t.Fatal("a cold root invented comparison totals")
	}
}

func TestDeferredReviewBrowserSupportsBuildingPaginationAndDeepLinks(t *testing.T) {
	for _, contract := range []string{
		"response.status === 202",
		"response.headers.get('Retry-After')",
		"response.dataset.nextCursor",
		"X-Change-Saga-Next-Cursor",
		"url.searchParams.set('cursor', cursor)",
		"destination.append(...inserted)",
		"history.pushState({view}, '', destination)",
		"revealHashedAnnotationBubble()",
		"previous?.controller.abort()",
	} {
		if !strings.Contains(appJavaScript, contract) {
			t.Errorf("async browser contract is missing %q", contract)
		}
	}
}
