package saga

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentDesignContentDigestsTrackAuthoredContentOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "digest.saga")
	writeTestFile(t, filepath.Join(root, "saga.json"), `{"version":3,"id":"digest","title":"Digest","source":{"repository":"https://example.test/app.git","base":"main","head":"HEAD"}}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "fragment.json"), `{"version":2,"id":"overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(root, "overview.fragment", "content.md"), "Narrative.\n")
	writeTestFile(t, filepath.Join(root, "___design", "architecture.chapter", "chapter.json"), `{"version":2,"id":"architecture","title":"Architecture"}`)
	fragmentDir := filepath.Join(root, "___design", "architecture.chapter", "flow.fragment")
	writeTestFile(t, filepath.Join(fragmentDir, "fragment.json"), `{"version":2,"id":"flow","title":"Flow","media_type":"text/markdown","entrypoint":"content.md"}`)
	writeTestFile(t, filepath.Join(fragmentDir, "content.md"), "# Flow {#flow}\n\nOriginal.\n")
	writeTestFile(t, filepath.Join(fragmentDir, "___landmarks", "flow.landmark", "landmark.json"), `{"version":2,"id":"flow","label":"Flow","selector":{"type":"heading","heading_id":"flow"}}`)

	document, validation, err := Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load: valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	before, err := CurrentDesignContentDigests(document)
	if err != nil {
		t.Fatal(err)
	}
	chapter := ChapterTarget("digest", "architecture")
	fragment := FragmentTarget("digest", "flow")
	landmark := LandmarkTarget("digest", "flow", "flow")
	for _, target := range []string{chapter, fragment, landmark} {
		if !strings.HasPrefix(before[target], "sha256:") || len(before[target]) != 71 {
			t.Fatalf("digest[%s] = %q", target, before[target])
		}
	}
	if _, exists := before[FragmentTarget("digest", "overview")]; exists {
		t.Fatal("root narrative was indexed as technical design")
	}

	writeTestFile(t, filepath.Join(fragmentDir, "___diffs", "evidence.json"), `{"version":2,"diffs":[]}`)
	document, _, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	withEvidence, err := CurrentDesignContentDigests(document)
	if err != nil {
		t.Fatal(err)
	}
	if withEvidence[chapter] != before[chapter] || withEvidence[fragment] != before[fragment] || withEvidence[landmark] != before[landmark] {
		t.Fatal("review evidence changed authored design digests")
	}

	if err := os.WriteFile(filepath.Join(fragmentDir, "content.md"), []byte("# Flow {#flow}\n\nRevised.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document, _, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := CurrentDesignContentDigests(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{chapter, fragment, landmark} {
		if after[target] == before[target] {
			t.Fatalf("authored content change did not invalidate %s", target)
		}
	}

	got, ok, err := CurrentDesignContentDigest(document, fragment)
	if err != nil || !ok || got != after[fragment] {
		t.Fatalf("lookup = %q, %v, %v", got, ok, err)
	}
}
