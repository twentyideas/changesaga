package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/querytest"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestQueryCLIRealSeparateRepositoriesAllOperations(t *testing.T) {
	fixture := querytest.New(t)
	largeTarget := fixture.AddLargeFragment((1 << 20) + 1)
	fixture.AddActiveFragments()
	changes, err := gitdiff.Read(context.Background(), fixture.SourceDir, querytest.Repository, fixture.BaseOID, "HEAD")
	if err != nil || len(changes.Atoms) == 0 {
		t.Fatalf("read fixture comparison: atoms=%d err=%v", len(changes.Atoms), err)
	}
	evidence, err := json.Marshal(saga.DiffFile{Version: saga.CurrentVersion, Diffs: []saga.DiffReference{{URI: changes.Atoms[0].URI, Note: "query integration"}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.WriteSaga("overview.fragment/___diffs/coverage.json", string(evidence))
	fileURI, err := diffuri.Build(diffuri.Reference{
		Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID,
		Kind: "file", Path: changes.Atoms[0].Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.State()
	common := []string{"--saga", fixture.SagaRoot, "--repo", fixture.SourceDir}

	tests := []struct {
		name string
		args []string
	}{
		{name: "overview", args: []string{"overview"}},
		{name: "children", args: []string{"children", "--parent", saga.SagaTarget("security"), "--limit", "2"}},
		{name: "fragment", args: []string{"fragment", "--target", querytest.OverviewTarget, "--limit", "8"}},
		{name: "fragment-diffs", args: []string{"fragment-diffs", "--target", querytest.OverviewTarget}},
		{name: "diff-owners-atom", args: []string{"diff-owners", "--diff", changes.Atoms[0].URI}},
		{name: "diff-owners-file", args: []string{"diff-owners", "--diff", fileURI}},
		{name: "reviews", args: []string{"reviews"}},
		{name: "gaps", args: []string{"gaps", "--kind", "uncovered"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			envelope, status, body := runRealQuery(t, append(testCase.args, common...))
			if status != 0 || !envelope.OK || envelope.Schema != querySchema || envelope.Snapshot == "" || envelope.Data == nil || envelope.Page == nil {
				t.Fatalf("status=%d envelope=%#v body=%s", status, envelope, body)
			}
			if data, ok := envelope.Data.(map[string]any); ok {
				if _, nested := data["page"]; nested {
					t.Fatalf("pagination leaked into data instead of the common envelope: %s", body)
				}
			}
		})
	}
	_, firstStatus, firstOverview := runRealQuery(t, append([]string{"overview"}, common...))
	_, secondStatus, secondOverview := runRealQuery(t, append([]string{"overview"}, common...))
	if firstStatus != 0 || secondStatus != 0 || firstOverview != secondOverview {
		t.Fatalf("identical overview queries were not byte-deterministic\nfirst:  %s\nsecond: %s", firstOverview, secondOverview)
	}

	large, status, body := runRealQuery(t, append([]string{"fragment", "--target", largeTarget, "--limit", fmt.Sprint(1 << 20)}, common...))
	if status != 0 || !large.OK {
		t.Fatalf("large query status=%d body=%s", status, body)
	}
	data := large.Data.(map[string]any)
	content := data["content"].(map[string]any)
	if got := len(content["data"].(string)); got != 1<<20 {
		t.Fatalf("large chunk bytes=%d, want %d", got, 1<<20)
	}
	fixture.AssertUnchanged(before)
}

func TestQueryCLIStaleTamperedAndCrossQueryCursors(t *testing.T) {
	fixture := querytest.New(t)
	fixture.AddLargeFragment(32)
	fixture.AddActiveFragments()
	common := []string{"--saga", fixture.SagaRoot, "--repo", fixture.SourceDir}
	first, status, body := runRealQuery(t, append([]string{"children", "--parent", saga.SagaTarget("security"), "--limit", "1"}, common...))
	if status != 0 || first.Page == nil || first.Page.NextCursor == nil {
		t.Fatalf("first page status=%d envelope=%#v body=%s", status, first, body)
	}
	cursor := *first.Page.NextCursor
	second, status, body := runRealQuery(t, append([]string{"children", "--parent", saga.SagaTarget("security"), "--limit", "1", "--cursor", cursor}, common...))
	if status != 0 || !second.OK {
		t.Fatalf("unchanged cursor status=%d body=%s", status, body)
	}

	for _, attack := range querytest.TamperedCursors(cursor) {
		t.Run(attack.Name, func(t *testing.T) {
			envelope, status, body := runRealQuery(t, append([]string{"children", "--parent", saga.SagaTarget("security"), "--limit", "1", "--cursor", attack.Token}, common...))
			if status != 2 || envelope.OK || envelope.Error == nil || envelope.Error.Code != "invalid_argument" {
				t.Fatalf("status=%d envelope=%#v body=%s", status, envelope, body)
			}
		})
	}
	cross, status, body := runRealQuery(t, append([]string{"gaps", "--cursor", cursor}, common...))
	if status != 2 || cross.Error == nil || cross.Error.Code != "invalid_argument" {
		t.Fatalf("cross-query cursor status=%d envelope=%#v body=%s", status, cross, body)
	}

	fixture.AdvanceSagaSnapshot()
	stale, status, body := runRealQuery(t, append([]string{"children", "--parent", saga.SagaTarget("security"), "--limit", "1", "--cursor", cursor}, common...))
	if status != 4 || stale.Error == nil || stale.Error.Code != "stale_snapshot" || !stale.Error.Retryable {
		t.Fatalf("stale cursor status=%d envelope=%#v body=%s", status, stale, body)
	}
}

func TestQueryCLIInvalidAndAmbiguousSagasUseStableEnvelope(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		alter func(*querytest.Fixture)
	}{
		{name: "invalid", alter: (*querytest.Fixture).MakeInvalidManifest},
		{name: "ambiguous", alter: (*querytest.Fixture).AddAmbiguousTargets},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := querytest.New(t)
			testCase.alter(fixture)
			envelope, status, body := runRealQuery(t, []string{"overview", "--saga", fixture.SagaRoot, "--repo", fixture.SourceDir})
			if status != 3 || envelope.OK || envelope.Error == nil || envelope.Error.Code != "invalid_saga" || strings.Contains(body, fixture.SagaRoot) {
				t.Fatalf("status=%d envelope=%#v body=%s", status, envelope, body)
			}
		})
	}

	missing := filepath.Join(t.TempDir(), "private", "missing.saga")
	envelope, status, body := runRealQuery(t, []string{"overview", "--saga", missing})
	if status != 5 || envelope.Error == nil || envelope.Error.Code != "not_found" || strings.Contains(body, missing) {
		t.Fatalf("missing saga status=%d envelope=%#v body=%s", status, envelope, body)
	}

	t.Run("source unavailable", func(t *testing.T) {
		fixture := querytest.New(t)
		before := fixture.State()
		envelope, status, body := runRealQuery(t, []string{"overview", "--saga", fixture.SagaRoot, "--repo", fixture.SourceDir + "-missing"})
		if status != 7 || envelope.Error == nil || envelope.Error.Code != "source_unavailable" || !envelope.Error.Retryable {
			t.Fatalf("source error status=%d envelope=%#v body=%s", status, envelope, body)
		}
		fixture.AssertNoAbsolutePaths(body)
		fixture.AssertUnchanged(before)
	})
}

func runRealQuery(t *testing.T, args []string) (queryEnvelope, int, string) {
	t.Helper()
	var output bytes.Buffer
	err := Query(context.Background(), args, &output)
	status := 0
	if err != nil {
		var statusError *StatusError
		if !errors.As(err, &statusError) {
			t.Fatalf("query returned non-status error: %v", err)
		}
		status = statusError.Code
	}
	var envelope queryEnvelope
	decodeOneJSONValue(t, output.Bytes(), &envelope)
	return envelope, status, output.String()
}
