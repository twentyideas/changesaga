package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestAddLandmarkMakesDiagramElementsCoverable(t *testing.T) {
	root := newAuthoredSaga(t)
	source := filepath.Join(t.TempDir(), "map.svg")
	if err := os.WriteFile(source, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g id="worker-pool"><text>Workers</text></g></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := AddFragment(context.Background(), []string{"--type", "svg", "--title", "System map", "--source", source, root}, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := AddLandmark(context.Background(), []string{"--target", "system-map.fragment", "--element-id", "worker-pool", "--label", "Worker pool", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Target: urn:change-saga:") || !strings.Contains(output.String(), "change-saga cover --target") {
		t.Fatalf("landmark output did not make the next step discoverable: %s", output.String())
	}

	reference, err := diffuri.Build(diffuri.Reference{
		Repository: "https://example.test/acme/app.git",
		Base:       "aaa",
		Head:       "bbb",
		Kind:       "line",
		Path:       "worker.go",
		Side:       "new",
		Start:      3,
		End:        7,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := "system-map.fragment/___landmarks/worker-pool.landmark"
	if err := Cover(context.Background(), []string{"--target", target, "--uri", reference, "--note", "Implements the worker pool node.", root}, &output); err != nil {
		t.Fatalf("cover landmark: %v", err)
	}

	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load landmark saga: validation=%#v err=%v", validation, err)
	}
	fragment := document.Section.Fragments[1]
	if fragment.ID != "atomic-system-map" {
		fragment = document.Section.Fragments[0]
	}
	if len(fragment.Landmarks) != 1 || fragment.Landmarks[0].Selector.ElementID != "worker-pool" || len(fragment.Landmarks[0].Diffs) != 1 {
		t.Fatalf("landmark was not addressable coverage: %#v", fragment.Landmarks)
	}
}

func TestAddLandmarkRejectsMissingElementsWithoutPartialMetadata(t *testing.T) {
	root := newAuthoredSaga(t)
	source := filepath.Join(t.TempDir(), "map.svg")
	if err := os.WriteFile(source, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g id="present"/></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := AddFragment(context.Background(), []string{"--type", "svg", "--title", "System map", "--source", source, root}, &output); err != nil {
		t.Fatal(err)
	}
	err := AddLandmark(context.Background(), []string{"--target", "system-map.fragment", "--element-id", "missing", root}, &output)
	if err == nil || !strings.Contains(err.Error(), "does not appear") {
		t.Fatalf("missing element error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "system-map.fragment", "___landmarks", "missing.landmark")); !os.IsNotExist(statErr) {
		t.Fatalf("failed landmark left partial metadata: %v", statErr)
	}
}

func TestParseLandmarkRegionRejectsNonFiniteCoordinates(t *testing.T) {
	for _, value := range []string{"NaN,0,0.2,0.2", "0,+Inf,0.2,0.2", "0,0,-Inf,0.2"} {
		if _, err := parseLandmarkRegion(value); err == nil {
			t.Fatalf("parseLandmarkRegion(%q) succeeded", value)
		}
	}
}
