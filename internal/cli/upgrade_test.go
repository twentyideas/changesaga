package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

func TestUpgradeToV3PreservesV2ContentAndCreatesLivingRoots(t *testing.T) {
	root := newAuthoredSaga(t)
	before, err := os.ReadFile(filepath.Join(root, "overview.fragment", "fragment.json"))
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "format v2 to v3") {
		t.Fatalf("unexpected output: %q", output.String())
	}
	for _, name := range livingRootDirectories {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s is not a real directory: info=%v err=%v", name, info, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "overview.fragment", "fragment.json"))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("v2 component changed: equal=%v err=%v", bytes.Equal(after, before), err)
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("upgraded Saga = valid %v, err %v, issues %#v", validation.Valid, err, validation.Issues)
	}
	if document.Manifest.Version != saga.CurrentSagaVersion || document.Manifest.Schema != saga.V3SchemaURL {
		t.Fatalf("manifest was not upgraded: %#v", document.Manifest)
	}
	if document.Section.Fragments[0].ID == "" {
		t.Fatal("v2 narrative did not remain readable")
	}
}

func TestUpgradeFailureRestoresExactV2ManifestAndNoLivingRoots(t *testing.T) {
	steps := []string{"after-stage", "after-root:___requirements", "after-root:___workplan", "before-manifest", "after-manifest"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := newAuthoredSaga(t)
			before, err := os.ReadFile(filepath.Join(root, "saga.json"))
			if err != nil {
				t.Fatal(err)
			}
			upgradeFaultHook = func(current string) error {
				if current == step {
					return errors.New("injected upgrade failure")
				}
				return nil
			}
			t.Cleanup(func() { upgradeFaultHook = nil })

			err = Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("Upgrade error = %v", err)
			}
			upgradeFaultHook = nil
			after, readErr := os.ReadFile(filepath.Join(root, "saga.json"))
			if readErr != nil || !bytes.Equal(after, before) {
				t.Fatalf("manifest changed after %s: equal=%v err=%v", step, bytes.Equal(after, before), readErr)
			}
			for _, name := range livingRootDirectories {
				if _, statErr := os.Lstat(filepath.Join(root, name)); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("failed upgrade left %s: %v", name, statErr)
				}
			}
			assertValid(t, root)
			matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(root), ".change-saga-upgrade-*"))
			if globErr != nil || len(matches) != 0 {
				t.Fatalf("failed upgrade left staging state: %v, err=%v", matches, globErr)
			}
		})
	}
}

func TestUpgradeRejectsUnsupportedTargetsAndRepeatUpgrade(t *testing.T) {
	root := newAuthoredSaga(t)
	for _, args := range [][]string{{root}, {"--to", "2", root}, {"--to", "4", root}} {
		if err := Upgrade(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("Upgrade(%v) succeeded", args)
		}
	}
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before := rootEntryNames(t, root)
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("repeat Upgrade error = %v", err)
	}
	if after := rootEntryNames(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("repeat upgrade changed root entries: before=%v after=%v", before, after)
	}
}

func TestUpgradeJSONResultIsBounded(t *testing.T) {
	root := newAuthoredSaga(t)
	var output bytes.Buffer
	if err := Upgrade(context.Background(), []string{"--to", "3", "--json", root}, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK      bool     `json:"ok"`
		From    int      `json:"from"`
		To      int      `json:"to"`
		Created []string `json:"created"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.From != 2 || result.To != 3 || !reflect.DeepEqual(result.Created, livingRootDirectories) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestUpgradePreservesValidV2SymlinkedContent(t *testing.T) {
	root := newAuthoredSaga(t)
	link := filepath.Join(root, "overview.fragment", "alias.md")
	if err := os.Symlink("content.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Upgrade(context.Background(), []string{"--to", "3", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("content symlink changed: info=%v err=%v", info, err)
	}
	assertValid(t, root)
}

func rootEntryNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}
