package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestV3DesignChapterAndFragmentEnterExistingRenderTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "render-design.saga")
	writeDesignTestFile(t, filepath.Join(root, "saga.json"), `{"version":3,"id":"render-design","title":"Render design","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeDesignTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeDesignTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "Narrative overview.\n")
	writeDesignTestFile(t, filepath.Join(root, "___design", "architecture.chapter", "chapter.json"), `{"version":2,"id":"architecture","title":"Technical architecture"}`)
	writeDesignTestFile(t, filepath.Join(root, "___design", "architecture.chapter", "sequence.fragment", "fragment.json"), `{"version":2,"id":"sequence","title":"Request sequence","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeDesignTestFile(t, filepath.Join(root, "___design", "architecture.chapter", "sequence.fragment", "content.md"), "# Request sequence {#request-sequence}\n\nRenderable design content.\n")

	document, validation, err := saga.LoadNarrative(root)
	if err != nil || !validation.Valid {
		t.Fatalf("LoadNarrative = valid %v, err %v, issues %#v", validation.Valid, err, validation.Issues)
	}
	nav := makeNavTree(document.Section, nil)
	if len(nav) != 2 || nav[1].Title != "Technical architecture" || nav[1].Href != sagaHref(saga.ChapterTarget("render-design", "architecture")) {
		t.Fatalf("design navigation = %#v", nav)
	}
	view := makeSectionView(document.Section, viewScope{})
	if len(view.ChildViews) != 1 || len(view.ChildViews[0].FragmentViews) != 1 {
		t.Fatalf("design render tree = %#v", view.ChildViews)
	}
	fragment := view.ChildViews[0].FragmentViews[0]
	if fragment.Target != saga.FragmentTarget("render-design", "sequence") || fragment.Markdown == "" {
		t.Fatalf("rendered design fragment = %#v", fragment)
	}
}

func writeDesignTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
