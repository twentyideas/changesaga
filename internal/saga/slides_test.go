package saga

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

func TestLoadV4SlideNativeItemEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "visual.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"$schema":"https://changesaga.dev/schema/v4/saga.schema.json","version":4,"id":"visual","title":"Visual review","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "deck.json"), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.json"), `{"version":4,"id":"change","title":"Reject early","rank":0,"intent":"explain","layout":"diagram","media_type":"image/svg+xml","entrypoint":"slide.svg","takeaway":"Invalid requests stop before persistence.","reading_order":["validate","why"]}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "change.slide", "slide.svg"), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720"><g id="validate"><rect width="200" height="100"/></g><g id="why"><text>Reject before writes</text></g></svg>`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "change.slide", "___items", "validate.item", "item.json"), `{"version":4,"id":"validate","kind":"node","label":"Validate","description":"The validation boundary.","selector":{"type":"element","element_id":"validate"}}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "change.slide", "___items", "why.item", "item.json"), `{"version":4,"id":"why","kind":"callout","label":"Why here","description":"Explains why validation moved.","selector":{"type":"element","element_id":"why"},"about":"validate","body":"Reject before any write.","placement":"right","leader":"arrow"}`)
	uri, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "main", Head: "feature", Kind: "line", Path: "handler.go", Side: "new", Start: 12, End: 12})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "overview.deck", "change.slide", "___items", "why.item", "___diffs", "handler.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q}]}`, uri))

	document, validation, err := Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load v4: valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	if len(document.Decks) != 1 || len(document.Decks[0].Slides) != 1 || len(document.Decks[0].Slides[0].Items) != 2 {
		t.Fatalf("native hierarchy not loaded: %#v", document.Decks)
	}
	item := document.Decks[0].Slides[0].Items[1]
	if item.Kind != "callout" || len(item.Diffs) != 1 || item.Target != ItemTarget("visual", "change", "why") {
		t.Fatalf("callout evidence not preserved: %#v", item)
	}
	projected := document.Section.Children[0].Fragments[0].Landmarks[1]
	if projected.Target != item.Target || len(projected.Diffs) != 1 {
		t.Fatalf("review projection lost Item identity or evidence: %#v", projected)
	}
	index := MutationIndexFromDocument(document)
	if index.Targets[item.Target] != item.Directory || index.ReviewTargets[item.Target] != item.Directory {
		t.Fatalf("item is not a stable mutation/review target: %#v", index)
	}
}

func TestV4RefusesLegacyPackagesAndBroadEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "refuse.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":4,"id":"refuse","title":"Refuse compatibility","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "deck.json"), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	writeTestFile(t, filepath.Join(root, "legacy.fragment", "fragment.json"), `{"version":2,"id":"legacy","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.deck", "___diffs", "broad.json"), `{"version":2,"diffs":[]}`)
	_, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("v4 silently accepted a legacy fragment and deck-level evidence")
	}
	var report strings.Builder
	for _, issue := range validation.Issues {
		report.WriteString(issue.Message)
		report.WriteByte('\n')
	}
	if !strings.Contains(report.String(), "migrate them explicitly") || !strings.Contains(report.String(), "attached to an item") {
		t.Fatalf("refusal was not actionable:\n%s", report.String())
	}
}
