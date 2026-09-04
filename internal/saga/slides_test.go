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
	writeTestFile(t, filepath.Join(root, FlatManifestName), `{"$schema":"https://changesaga.dev/schema/v4/saga.schema.json","version":4,"id":"visual","title":"Visual review","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	deckTarget := DeckTarget("visual", "overview")
	deckName, _ := FlatDeckFilename(deckTarget, 0)
	writeTestFile(t, filepath.Join(root, deckName), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	slideTarget := SlideTarget("visual", "change")
	slideName, _ := FlatSlideFilename(deckTarget, slideTarget, 0)
	assetName, _ := FlatSlideAssetFilename(slideName, ".svg")
	writeTestFile(t, filepath.Join(root, slideName), fmt.Sprintf(`{"version":4,"id":"change","deck":"overview","title":"Reject early","rank":0,"intent":"explain","layout":"diagram","media_type":"image/svg+xml","entrypoint":%q,"takeaway":"Invalid requests stop before persistence.","reading_order":["validate","why"]}`, assetName))
	writeTestFile(t, filepath.Join(root, assetName), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720"><g id="validate"><rect width="200" height="100"/></g><g id="why"><text>Reject before writes</text></g></svg>`)
	validateTarget := ItemTarget("visual", "change", "validate")
	validateName, _ := FlatItemFilename(slideTarget, validateTarget, 0)
	writeTestFile(t, filepath.Join(root, validateName), `{"version":4,"id":"validate","slide":"change","rank":0,"kind":"node","label":"Validate","description":"The validation boundary.","selector":{"type":"element","element_id":"validate"}}`)
	whyTarget := ItemTarget("visual", "change", "why")
	whyName, _ := FlatItemFilename(slideTarget, whyTarget, 10)
	writeTestFile(t, filepath.Join(root, whyName), `{"version":4,"id":"why","slide":"change","rank":10,"kind":"callout","label":"Why here","description":"Explains why validation moved.","selector":{"type":"element","element_id":"why"},"about":"validate","body":"Reject before any write.","placement":"right","leader":"arrow"}`)
	uri, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "main", Head: "feature", Kind: "line", Path: "handler.go", Side: "new", Start: 12, End: 12})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, FlatEvidenceFilename(whyTarget, "handler")), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q}]}`, uri))

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
	if index.Targets[item.Target] != item.Directory {
		t.Fatalf("item is not a stable mutation target: %#v", index)
	}
	if _, ok := index.ReviewTargets[item.Target]; ok {
		t.Fatalf("v4 Item unexpectedly accepted an approval decision: %#v", index.ReviewTargets)
	}
	if index.ReviewTargets[slideTarget] != root {
		t.Fatalf("slide is not the v4 approval boundary: %#v", index.ReviewTargets)
	}

	writeTestFile(t, filepath.Join(root, slideName), fmt.Sprintf(`{"version":4,"id":"change","deck":"wrong-deck","title":"Reject early","rank":0,"intent":"explain","layout":"diagram","media_type":"image/svg+xml","entrypoint":%q,"takeaway":"Invalid requests stop before persistence.","reading_order":["validate","why"]}`, assetName))
	writeTestFile(t, filepath.Join(root, validateName), `{"version":4,"id":"validate","slide":"wrong-slide","rank":0,"kind":"node","label":"Validate","description":"The validation boundary.","selector":{"type":"element","element_id":"validate"}}`)
	_, validation, err = Load(root)
	if err != nil || validation.Valid {
		t.Fatalf("incorrect semantic parent hints were accepted: valid=%v err=%v", validation.Valid, err)
	}
	var parentIssues strings.Builder
	for _, issue := range validation.Issues {
		parentIssues.WriteString(issue.Message)
		parentIssues.WriteByte('\n')
	}
	if !strings.Contains(parentIssues.String(), "slide deck must name its semantic parent") || !strings.Contains(parentIssues.String(), "item slide must name its semantic parent") {
		t.Fatalf("semantic parent failures were not explicit:\n%s", parentIssues.String())
	}
}

func TestV4RefusesLegacyPackagesAndBroadEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "refuse.saga")
	writeTestFile(t, filepath.Join(root, FlatManifestName), `{"version":4,"id":"refuse","title":"Refuse compatibility","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"feature"},"presentation":{"mode":"slides","aspect_ratio":"16:9","overview_deck":"overview"}}`)
	deckName, _ := FlatDeckFilename(DeckTarget("refuse", "overview"), 0)
	writeTestFile(t, filepath.Join(root, deckName), `{"version":4,"id":"overview","title":"Overview","role":"overview","rank":0,"objective":"Orient the reviewer."}`)
	writeTestFile(t, filepath.Join(root, "legacy.fragment", "fragment.json"), `{"version":2,"id":"legacy","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, FlatEvidenceFilename(SagaTarget("refuse"), "broad")), `{"version":2,"diffs":[]}`)
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
	if !strings.Contains(report.String(), "migrate nested packages explicitly") || !strings.Contains(report.String(), "unknown Item key") {
		t.Fatalf("refusal was not actionable:\n%s", report.String())
	}
}
