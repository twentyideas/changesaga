package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetFragmentContentSupportsStdinAndJSON(t *testing.T) {
	root := newAuthoredSaga(t)
	var output bytes.Buffer
	content := "# New overview {#new-overview}\n\nAuthored through the CLI.\n"
	if err := setFragmentContent(context.Background(), []string{"--target", "overview.fragment", "--source", "-", "--json", root}, &output, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	var result fragmentContentOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output.String())
	}
	if !result.OK || result.Bytes != len(content) || result.MediaType != "text/markdown" {
		t.Fatalf("unexpected result: %#v", result)
	}
	written, err := os.ReadFile(filepath.Join(root, "overview.fragment", "content.md"))
	if err != nil || string(written) != content {
		t.Fatalf("entrypoint = %q, %v", written, err)
	}
	assertValid(t, root)
}

func TestMarkdownAuthoringCommandsRefuseDenseSlidesWithoutWriting(t *testing.T) {
	root := newAuthoredSaga(t)
	overview := filepath.Join(root, "overview.fragment", "content.md")
	before, err := os.ReadFile(overview)
	if err != nil {
		t.Fatal(err)
	}
	dense := "# Too much {#too-much}\n\n" + strings.Repeat("explanation ", 101)
	var output bytes.Buffer
	err = setFragmentContent(context.Background(), []string{"--target", "overview.fragment", "--source", "-", root}, &output, strings.NewReader(dense))
	if err == nil || !strings.Contains(err.Error(), "maximum 100") {
		t.Fatalf("dense replacement error = %v", err)
	}
	after, readErr := os.ReadFile(overview)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("refused replacement changed the slide: %v", readErr)
	}

	source := filepath.Join(t.TempDir(), "dense.md")
	if err := os.WriteFile(source, []byte(dense), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err = AddFragment(context.Background(), []string{"--name", "dense", "--title", "Dense", "--source", source, root}, &output)
	if err == nil || !strings.Contains(err.Error(), "maximum 100") {
		t.Fatalf("dense new slide error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "dense.fragment")); !os.IsNotExist(statErr) {
		t.Fatalf("refused new slide left a package: %v", statErr)
	}
	assertValid(t, root)
}

func TestStableIDsResolveAcrossHierarchyCommands(t *testing.T) {
	root := newAuthoredSaga(t)
	var output bytes.Buffer
	if err := AddChapter(context.Background(), []string{"--id", "architecture", root, "backend"}, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := AddSection(context.Background(), []string{"--id", "request-flow", root, "architecture/request-flow"}, &output); err != nil {
		t.Fatalf("add section by chapter ID: %v", err)
	}
	output.Reset()
	if err := AddFragment(context.Background(), []string{"--section", "request-flow", "--name", "example", "--title", "Example", root}, &output); err != nil {
		t.Fatalf("add fragment by section ID: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "backend.chapter", "request-flow", "example.fragment", "content.md")); err != nil {
		t.Fatalf("fragment was not created under ID-resolved hierarchy: %v", err)
	}
	assertValid(t, root)
}
