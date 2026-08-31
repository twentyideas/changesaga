package saga

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

func TestLoadRecursiveFragmentsAndReviewOverlay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# The whole story\n")
	writeTestFile(t, filepath.Join(root, "backend.chapter", "chapter.json"), `{"version":2,"id":"backend","title":"Backend"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "section.json"), `{"version":2,"id":"request-flow","title":"Request flow"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "fragment.json"), `{"version":2,"id":"flow","title":"Flow","media_type":"text/html","entrypoint":"index.html"}`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "index.html"), `<button id="try-flow" onclick="this.textContent='ok'">Try it</button>`)
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "___landmarks", "try-flow.landmark", "landmark.json"), `{"version":2,"id":"try-flow","label":"Try the flow","selector":{"type":"element","element_id":"try-flow"}}`)

	diff, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "api.go", Side: "new", Start: 2, End: 4})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "___diffs", "api.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q}]}`, diff))
	writeTestFile(t, filepath.Join(root, "backend.chapter", "request-flow", "flow.fragment", "___landmarks", "try-flow.landmark", "___diffs", "api.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q}]}`, diff))
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "thread.json"), `{"version":2,"id":"thread-1","target":"urn:change-saga:test:fragment:flow","anchor":{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.1,"y":0.2,"width":0.3,"height":0.4}]},"created_by":"Ada","created_at":"2026-08-19T12:00:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "message.json"), `{"version":2,"id":"message-1","author":"Ada","created_at":"2026-08-19T12:00:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "body.fragment", "fragment.json"), `{"version":2,"id":"message-body","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "messages", "message-1.message", "body.fragment", "content.md"), "Please explain this transition.\n")
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "events", "withdrawn.json"), `{"version":2,"id":"withdrawn","state":"withdrawn","created_at":"2026-08-19T12:01:00Z"}`)
	writeTestFile(t, filepath.Join(root, "___review", "threads", "thread-1.thread", "events", "moved.json"), `{"version":2,"id":"moved","anchor":{"type":"region","coordinate_space":"normalized","shapes":[{"type":"rect","x":0.2,"y":0.3,"width":0.3,"height":0.4}]},"created_at":"2026-08-19T12:02:00Z"}`)

	document, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid {
		t.Fatalf("saga should be valid: %#v", validation)
	}
	if len(document.Section.Fragments) != 1 || len(document.Section.Children) != 1 {
		t.Fatalf("unexpected root: %#v", document.Section)
	}
	chapter := document.Section.Children[0]
	if chapter.Kind != "chapter" || chapter.Target != ChapterTarget("test", "backend") || len(chapter.Children) != 1 {
		t.Fatalf("chapter was not loaded as a review boundary: %#v", chapter)
	}
	flow := chapter.Children[0].Fragments[0]
	if flow.MediaType != "text/html" || len(flow.Diffs) != 1 || len(flow.Landmarks) != 1 || flow.Landmarks[0].Selector.ElementID != "try-flow" || flow.Landmarks[0].Target != LandmarkTarget("test", "flow", "try-flow") || len(flow.Landmarks[0].Diffs) != 1 {
		t.Fatalf("interactive fragment was not loaded: %#v", flow)
	}
	if len(document.Threads) != 1 || len(document.Threads[0].Messages) != 1 || document.Threads[0].Target != flow.Target || document.Threads[0].State != "withdrawn" || document.Threads[0].Anchor.Shapes[0].X != .2 {
		t.Fatalf("review overlay was not loaded: %#v", document.Threads)
	}
}

func TestLoadOutlineDoesNotOpenCoverageOrContentTrees(t *testing.T) {
	root := filepath.Join(t.TempDir(), "outline.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"outline","title":"Outline","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), strings.Repeat("large narrative body\n", 1024))
	writeTestFile(t, filepath.Join(root, "overview.fragment", "___diffs", "broken.json"), `{this is deliberately not JSON`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "___landmarks", "broken.landmark", "landmark.json"), `{this is deliberately not JSON`)

	document, validation, err := LoadOutline(root)
	if err != nil || !validation.Valid {
		t.Fatalf("outline load = valid %v, err %v, issues %#v", validation.Valid, err, validation.Issues)
	}
	fragment := document.Section.Fragments[0]
	if len(fragment.Diffs) != 0 || len(fragment.Landmarks) != 0 {
		t.Fatalf("outline materialized deferred metadata: diffs=%d landmarks=%d", len(fragment.Diffs), len(fragment.Landmarks))
	}
	if _, full, err := Load(root); err != nil || full.Valid {
		t.Fatalf("full load did not observe malformed deferred metadata: valid=%v err=%v", full.Valid, err)
	}
}

func TestLoadNarrativeAdvertisesTargetEvidenceWithoutMaterializingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "narrative.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"narrative","title":"Narrative","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "story.fragment", "fragment.json"), `{"version":2,"id":"story","title":"Story","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "story.fragment", "content.md"), "# Story\n")
	uri, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "app.go", Side: "new", Start: 1, End: 2})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "story.fragment", "___diffs", "app.json"), fmt.Sprintf(`{"version":2,"diffs":[{"uri":%q,"note":"Implements the story."}]}`, uri))

	document, validation, err := LoadNarrative(root)
	if err != nil || !validation.Valid {
		t.Fatalf("narrative load = valid %v, err %v, issues %#v", validation.Valid, err, validation.Issues)
	}
	fragment := document.Section.Fragments[0]
	if !fragment.HasDiffs || len(fragment.Diffs) != 0 {
		t.Fatalf("narrative evidence state = has %v, materialized %d", fragment.HasDiffs, len(fragment.Diffs))
	}
	diffs, targetValidation, err := LoadTargetDiffs(MutationIndexFromDocument(document), fragment.Target)
	if err != nil || !targetValidation.Valid || len(diffs) != 1 || len(diffs[0].Diffs) != 1 || diffs[0].Diffs[0].URI != uri {
		t.Fatalf("target evidence = %#v, valid %v, err %v, issues %#v", diffs, targetValidation.Valid, err, targetValidation.Issues)
	}
}

func TestLoadRejectsNestedChapter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "outer.chapter", "chapter.json"), `{"version":2,"id":"outer","title":"Outer"}`)
	writeTestFile(t, filepath.Join(root, "outer.chapter", "inner.chapter", "chapter.json"), `{"version":2,"id":"inner","title":"Inner"}`)
	_, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("nested chapters should be invalid; recurse with sections instead")
	}
}

func TestLoadRejectsUnknownJSONFields(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","surprise":true,"source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	if _, _, err := Load(root); err == nil {
		t.Fatal("expected unknown manifest field to fail")
	}
}

func TestLoadSupportsV2AndV3SagaContainers(t *testing.T) {
	for _, version := range []int{LegacySagaVersion, CurrentSagaVersion} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "test.saga")
			writeTestFile(t, filepath.Join(root, "saga.json"), fmt.Sprintf(`{"$schema":%q,"version":%d,"id":"test","title":"A saga","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`, SagaSchemaURL(version), version))
			writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","media_type":"text/markdown","entrypoint":"content.md"}`)
			writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "Story.\n")
			if version == CurrentSagaVersion {
				for _, name := range []string{"___requirements", "___design", "___workplan"} {
					if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}

			document, validation, err := Load(root)
			if err != nil || !validation.Valid {
				t.Fatalf("Load(v%d) = valid %v, err %v, issues %#v", version, validation.Valid, err, validation.Issues)
			}
			if document.Manifest.Version != version || document.Section.Fragments[0].ID != "overview" {
				t.Fatalf("unexpected v%d document: %#v", version, document)
			}
		})
	}
}

func TestLivingRootsAreReservedOnlyForV3AndMustBeRealDirectories(t *testing.T) {
	for _, name := range []string{"___requirements", "___design", "___workplan"} {
		t.Run(name, func(t *testing.T) {
			v2 := buildSaga(t, map[string]string{filepath.ToSlash(filepath.Join(name, "placeholder")): "present\n"})
			validation, report := loadIssues(t, v2)
			if validation.Valid || !strings.Contains(report, "unknown reserved directory") {
				t.Fatalf("v2 accepted %s:\n%s", name, report)
			}

			v3 := buildSaga(t, map[string]string{
				"saga.json": `{"version":3,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`,
				filepath.ToSlash(filepath.Join(name, "placeholder")): "present\n",
			})
			validation, report = loadIssues(t, v3)
			if !validation.Valid {
				t.Fatalf("v3 rejected %s:\n%s", name, report)
			}

			fileRoot := buildSaga(t, map[string]string{
				"saga.json": `{"version":3,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`,
				name:        "not a directory\n",
			})
			validation, report = loadIssues(t, fileRoot)
			if validation.Valid || !strings.Contains(report, "real directory") {
				t.Fatalf("v3 accepted non-directory %s:\n%s", name, report)
			}

			symlinkRoot := buildSaga(t, map[string]string{
				"saga.json": `{"version":3,"id":"test","title":"A saga","source":{"repository":"https://example.test/acme/app.git","base":"main","head":"HEAD"}}`,
			})
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(symlinkRoot, name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			validation, report = loadIssues(t, symlinkRoot)
			if validation.Valid || !strings.Contains(report, "real directory") {
				t.Fatalf("v3 accepted symlinked %s:\n%s", name, report)
			}
		})
	}
}

func TestLoadRejectsMissingFragmentEntrypoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "test.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":2,"id":"test","title":"A saga","source":{"repository":"https://example.test/a.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "broken.fragment", "fragment.json"), `{"version":2,"id":"broken","media_type":"text/html","entrypoint":"index.html"}`)
	_, validation, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("missing entrypoint should invalidate saga")
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
