package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestDesignAuthoringReusesHierarchyMutationsUnderV3Root(t *testing.T) {
	root := newAuthoredSaga(t)
	if err := Design(context.Background(), []string{"add-chapter", "--id", "architecture", root, "architecture"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "upgrade --to 3") {
		t.Fatalf("v2 design authoring error = %v", err)
	}
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Design(context.Background(), []string{"add-fragment", "--id", "system-map", "--name", "system-map", "--title", "System map", root}, &output); err != nil {
		t.Fatalf("add root design fragment: %v", err)
	}
	if !strings.Contains(output.String(), "___design/system-map.fragment") {
		t.Fatalf("root design fragment output = %q", output.String())
	}
	output.Reset()
	if err := Design(context.Background(), []string{"add-chapter", "--id", "architecture", "--title", "Architecture", root, "architecture"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "___design/architecture.chapter") || !strings.Contains(output.String(), "change-saga design add-section") {
		t.Fatalf("design chapter output = %q", output.String())
	}
	output.Reset()
	if err := Design(context.Background(), []string{"add-section", "--id", "request-flow", root, "architecture/request-flow"}, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := Design(context.Background(), []string{"add-fragment", "--section", "architecture.chapter/request-flow", "--id", "sequence", "--name", "sequence", "--title", "Sequence", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "___design/architecture.chapter/request-flow/sequence.fragment") || !strings.Contains(output.String(), "change-saga design set-fragment-content") {
		t.Fatalf("design fragment output = %q", output.String())
	}

	content := "# Request sequence {#request-sequence}\n\nThe design is independently editable.\n"
	output.Reset()
	if err := setFragmentContentScoped(context.Background(), []string{"--target", "sequence", "--source", "-", "--json", root}, &output, strings.NewReader(content), designAuthoring); err != nil {
		t.Fatal(err)
	}
	if written, err := os.ReadFile(filepath.Join(root, "___design", "architecture.chapter", "request-flow", "sequence.fragment", "content.md")); err != nil || string(written) != content {
		t.Fatalf("design fragment content = %q, %v", written, err)
	}

	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("authored design = valid %v, err %v, issues %#v", validation.Valid, err, validation.Issues)
	}
	if len(document.Section.Fragments) != 2 || len(document.Section.Children) != 1 {
		t.Fatalf("v2 root narrative or design chapter missing: %#v", document.Section)
	}
	fragments := map[string]string{}
	for _, current := range document.Section.Fragments {
		fragments[current.ID] = current.Path
	}
	if fragments["atomic-overview"] != "overview.fragment" || fragments["system-map"] != "___design/system-map.fragment" {
		t.Fatalf("root narrative/design fragments = %#v", fragments)
	}
	chapter := document.Section.Children[0]
	fragment := chapter.Children[0].Fragments[0]
	if chapter.Target != saga.ChapterTarget(document.Manifest.ID, "architecture") || fragment.Target != saga.FragmentTarget(document.Manifest.ID, "sequence") {
		t.Fatalf("design targets = chapter %q fragment %q", chapter.Target, fragment.Target)
	}
}

func TestDesignScopedMutationsRejectNarrativeTargets(t *testing.T) {
	root := newAuthoredSaga(t)
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "overview.fragment", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	err = setFragmentContentScoped(context.Background(), []string{"--target", "overview.fragment", "--source", "-", root}, &bytes.Buffer{}, strings.NewReader("must not be written\n"), designAuthoring)
	if err == nil {
		t.Fatal("design content mutation accepted a root narrative fragment")
	}
	after, readErr := os.ReadFile(filepath.Join(root, "overview.fragment", "content.md"))
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("narrative fragment changed: equal=%v err=%v", bytes.Equal(after, before), readErr)
	}
}

func TestDesignHelpListsSupportedOperations(t *testing.T) {
	var output bytes.Buffer
	if err := Design(context.Background(), []string{"--help"}, &output); err == nil {
		t.Fatal("help should return flag.ErrHelp to the command dispatcher")
	}
	for _, operation := range designOperations {
		if !strings.Contains(output.String(), commandUsage["design-"+operation]) {
			t.Errorf("design help omitted %s:\n%s", operation, output.String())
		}
	}
}
