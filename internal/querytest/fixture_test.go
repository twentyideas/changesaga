package querytest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

func TestFixtureUsesIndependentSagaAndSourceRepositories(t *testing.T) {
	fixture := New(t)
	if filepath.Dir(fixture.SagaRoot) != fixture.SagaRepo || fixture.SourceDir == fixture.SagaRepo {
		t.Fatalf("fixture repositories are not independent: %#v", fixture)
	}
	document, validation, err := saga.Load(fixture.SagaRoot)
	if err != nil || !validation.Valid {
		t.Fatalf("load fixture: validation=%#v err=%v", validation, err)
	}
	if document.Manifest.Source.Repository != Repository || document.Manifest.Source.Base != fixture.BaseOID {
		t.Fatalf("unexpected source identity: %#v", document.Manifest.Source)
	}
	changes, err := gitdiff.Read(context.Background(), fixture.SourceDir, Repository, fixture.BaseOID, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Atoms) == 0 || changes.Atoms[0].Path != "app.go" {
		t.Fatalf("source comparison was not exercised: %#v", changes.Atoms)
	}
}

func TestFixtureAdversarialOverlays(t *testing.T) {
	t.Run("invalid manifest", func(t *testing.T) {
		fixture := New(t)
		fixture.MakeInvalidManifest()
		if _, _, err := saga.Load(fixture.SagaRoot); err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("invalid manifest unexpectedly loaded: %v", err)
		}
	})

	t.Run("ambiguous target", func(t *testing.T) {
		fixture := New(t)
		fixture.AddAmbiguousTargets()
		_, validation, err := saga.Load(fixture.SagaRoot)
		if err != nil {
			t.Fatal(err)
		}
		if validation.Valid || !issuesContain(validation, `duplicate id "shared"`) {
			t.Fatalf("ambiguous target was not rejected: %#v", validation)
		}
	})

	t.Run("large content", func(t *testing.T) {
		fixture := New(t)
		fixture.AddLargeFragment((1 << 20) + 1)
		info, err := os.Stat(filepath.Join(fixture.SagaRoot, "large.fragment", "content.txt"))
		if err != nil || info.Size() != (1<<20)+1 {
			t.Fatalf("large fixture size=%v err=%v", info, err)
		}
		_, validation, err := saga.Load(fixture.SagaRoot)
		if err != nil || !validation.Valid {
			t.Fatalf("large content should be structurally valid: %#v %v", validation, err)
		}
	})

	t.Run("active content", func(t *testing.T) {
		fixture := New(t)
		htmlTarget, svgTarget := fixture.AddActiveFragments()
		if htmlTarget != ActiveHTMLTarget || svgTarget != ActiveSVGTarget {
			t.Fatalf("unexpected active targets: %q %q", htmlTarget, svgTarget)
		}
		_, validation, err := saga.Load(fixture.SagaRoot)
		if err != nil || !validation.Valid {
			t.Fatalf("active content should be valid inert input: %#v %v", validation, err)
		}
	})

	t.Run("escaping entrypoint symlink", func(t *testing.T) {
		fixture := New(t)
		fixture.AddEscapingEntrypointSymlink()
		_, validation, err := saga.Load(fixture.SagaRoot)
		if err != nil {
			t.Fatal(err)
		}
		if validation.Valid || !issuesContain(validation, "entrypoint cannot escape") {
			t.Fatalf("escaping entrypoint was not rejected: %#v", validation)
		}
	})

	t.Run("escaping asset symlink", func(t *testing.T) {
		fixture := New(t)
		fixture.AddEscapingAssetSymlink()
		_, validation, err := saga.Load(fixture.SagaRoot)
		if err != nil || !validation.Valid {
			t.Fatalf("asset fixture must reach query-layer checks: %#v %v", validation, err)
		}
	})
}

func TestFixtureNoSideEffectAssertion(t *testing.T) {
	fixture := New(t)
	before := fixture.State()
	if _, _, err := saga.Load(fixture.SagaRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := gitdiff.Read(context.Background(), fixture.SourceDir, Repository, fixture.BaseOID, "HEAD"); err != nil {
		t.Fatal(err)
	}
	fixture.AssertUnchanged(before)
}

func TestFixtureAbsolutePathLeakDetection(t *testing.T) {
	fixture := New(t)
	fixture.AssertNoAbsolutePaths(`{"code":"invalid_saga","details":{"path":"overview.fragment/fragment.json"}}`)
	if leaked := fixture.LeakedAbsolutePath(`{"message":"failed below ` + filepath.ToSlash(fixture.SagaRoot) + `"}`); leaked != fixture.SagaRoot {
		t.Fatalf("absolute saga root was not detected: %q", leaked)
	}
}

func TestTamperedCursors(t *testing.T) {
	cases := TamperedCursors("eyJzbmFwc2hvdCI6ImFiYyIsImluZGV4IjoxfQ")
	seen := map[string]bool{}
	for _, testCase := range cases {
		if testCase.Name == "" || seen[testCase.Token] {
			t.Fatalf("cursor attack is unnamed or duplicated: %#v", testCase)
		}
		seen[testCase.Token] = true
	}
}

func issuesContain(validation saga.Validation, substring string) bool {
	for _, issue := range validation.Issues {
		if strings.Contains(issue.Message, substring) {
			return true
		}
	}
	return false
}
