package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestSlideNativeAuthoringLoopAndCompatibilityRefusal(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	git(t, repo, "remote", "add", "origin", "https://example.test/acme/app.git")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-m", "base")

	root := filepath.Join(t.TempDir(), "visual.saga")
	var output bytes.Buffer
	if err := Init(context.Background(), []string{"--mode", "slides", "--repo", repo, "--base", "main", "--head", "HEAD", "--title", "Visual", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := AddSlide(context.Background(), []string{"--deck", "overview", "--intent", "orient", "--layout", "hero", "--entrypoint", "assets/slide.svg", root, "nested-source"}, &output); err == nil || !strings.Contains(err.Error(), "simple filename") {
		t.Fatalf("nested v4 entrypoint was not refused clearly: %v", err)
	}
	if err := AddSlide(context.Background(), []string{"--deck", "overview", "--intent", "orient", "--layout", "hero", "--title", "What changed", "--takeaway", "Validation now happens first.", root, "change-overview"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := AddItem(context.Background(), []string{"--slide", "change-overview", "--kind", "callout", "--id", "premise", "--element-id", "slide-title", "--label", "Review premise", "--description", "The high-level behavioral change.", "--body", "Invalid requests never reach persistence.", "--placement", "right", "--leader", "arrow", root}, &output); err != nil {
		t.Fatal(err)
	}
	interactive := filepath.Join(t.TempDir(), "interactive.html")
	writeFile(t, interactive, `<main id="decision">Choose a path</main><script>document.querySelector('#decision').dataset.ready = 'true'</script>`)
	if err := AddSlide(context.Background(), []string{"--deck", "overview", "--intent", "compare", "--layout", "before-after", "--rank", "1", "--title", "Interactive comparison", "--takeaway", "The alternate path remains inspectable.", "--media-type", "text/html", "--entrypoint", "index.html", "--source", interactive, root, "interactive-comparison"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := AddItem(context.Background(), []string{"--slide", "interactive-comparison", "--kind", "statement", "--id", "decision", "--element-id", "decision", "--description", "The alternate path decision.", root}, &output); err != nil {
		t.Fatal(err)
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("authored v4 invalid: valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	if _, err := os.Stat(filepath.Join(root, document.Decks[0].Slides[1].Entrypoint)); err != nil {
		t.Fatalf("compact slide asset was not written: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) > saga.FlatMaxBasename {
			t.Fatalf("v4 output is not compact and flat: %s", entry.Name())
		}
	}
	item := document.Decks[0].Slides[0].Items[0]
	uri, err := diffuri.Build(diffuri.Reference{Repository: document.Manifest.Source.Repository, Base: "main", Head: "HEAD", Kind: "line", Path: "README.md", Side: "new", Start: 1, End: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := Cover(context.Background(), []string{"--target", document.Decks[0].Slides[0].Target, "--uri", uri, root}, &output); err == nil || !strings.Contains(err.Error(), "must target an Item") {
		t.Fatalf("slide-level coverage was not refused: %v", err)
	}
	if err := Cover(context.Background(), []string{"--target", item.Target, "--uri", uri, root}, &output); err != nil {
		t.Fatalf("item coverage failed: %v", err)
	}
	if err := Review(context.Background(), []string{"--target", item.Path, "--state", "approved", "--reviewer-kind", "human", root}, &output); err == nil || !strings.Contains(err.Error(), "must target a slide") {
		t.Fatalf("Item approval was not explicitly refused: %v", err)
	}
	if err := Review(context.Background(), []string{"--target", document.Decks[0].Slides[0].Path, "--state", "approved", "--reviewer-kind", "human", "--body", "Visual argument checked.", root}, &output); err != nil {
		t.Fatalf("slide review by compact record path failed: %v", err)
	}
	if err := Thread(context.Background(), []string{"--target", item.Target, "--body", "Keep the validation boundary visible.", root}, &output); err != nil {
		t.Fatalf("flat thread failed: %v", err)
	}
	if err := AddClaim(context.Background(), []string{"--id", "validation-boundary", "--target", item.Target, "--statement", "Validation runs before persistence.", "--diff", uri, root}, &output); err != nil {
		t.Fatalf("flat claim failed: %v", err)
	}
	if err := VerifyClaim(context.Background(), []string{"--id", "validation-check", "--claim", "validation-boundary", "--status", "verified", "--method", "inspection", "--summary", "The mapped line establishes the boundary.", root}, &output); err != nil {
		t.Fatalf("flat verification failed: %v", err)
	}
	document, validation, err = saga.Load(root)
	if err != nil || !validation.Valid || len(document.Decks[0].Slides[0].Reviews) != 1 || len(document.Decks[0].Slides[0].Items[0].Reviews) != 0 || len(document.Threads) != 1 || len(document.Threads[0].Messages) != 1 || len(document.Claims) != 1 || len(document.Verifications) != 1 {
		t.Fatalf("slide approval or Item comment was not preserved: valid=%v err=%v slide=%#v", validation.Valid, err, document.Decks[0].Slides[0])
	}
	var queryOutput bytes.Buffer
	if err := Query(context.Background(), []string{"slide", "--saga", root, "--repo", repo, "--target", document.Decks[0].Slides[0].Target}, &queryOutput); err != nil {
		t.Fatalf("slide query failed: %v\n%s", err, queryOutput.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(queryOutput.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := envelope["data"].(map[string]any)
	if envelope["schema"] != slideQuerySchema || data["items"] == nil || data["landmarks"] != nil || data["takeaway"] != "Validation now happens first." {
		t.Fatalf("slide query leaked report vocabulary or metadata: %#v", envelope)
	}
	queryOutput.Reset()
	if err := Query(context.Background(), []string{"overview", "--saga", root, "--repo", repo}, &queryOutput); err != nil {
		t.Fatalf("v4 overview query failed: %v\n%s", err, queryOutput.String())
	}
	envelope = map[string]any{}
	if err := json.Unmarshal(queryOutput.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data, _ = envelope["data"].(map[string]any)
	if envelope["schema"] != slideQuerySchema || data["decks"] == nil || data["chapters"] != nil || data["overview_fragments"] != nil {
		t.Fatalf("v4 overview leaked report hierarchy: %#v", envelope)
	}
	if err := AddChapter(context.Background(), []string{root, "legacy"}, &output); err == nil || !strings.Contains(err.Error(), "use add-deck") {
		t.Fatalf("legacy authoring was not refused: %v", err)
	}
	if !strings.Contains(output.String(), "change-saga cover") {
		t.Fatalf("authoring guidance did not lead to Item evidence:\n%s", output.String())
	}
}
