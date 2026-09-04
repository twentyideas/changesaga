package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/requirements"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestRebaseEvidenceProvesProductIdentityAndRollsClaimsForward(t *testing.T) {
	fixture := newEvidenceRebaseFixture(t)
	evidenceBefore, err := os.ReadFile(fixture.evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	claimBefore, err := os.ReadFile(fixture.claimPath)
	if err != nil {
		t.Fatal(err)
	}

	var preview bytes.Buffer
	if err := RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, "--from-base", fixture.old.BaseOID, "--carry-verifications", "--dry-run", "--json", fixture.root}, &preview); err != nil {
		t.Fatal(err)
	}
	var previewResult evidenceRebaseOutput
	if err := json.Unmarshal(preview.Bytes(), &previewResult); err != nil {
		t.Fatalf("decode preview: %v\n%s", err, preview.String())
	}
	if !previewResult.OK || !previewResult.DryRun || previewResult.FromBase != fixture.old.BaseOID || previewResult.ToBase != fixture.current.BaseOID {
		t.Fatalf("unexpected preview: %#v", previewResult)
	}
	if previewResult.ProductIdentity != fixture.old.HeadOID || previewResult.Atoms != len(fixture.current.Atoms) || previewResult.Selectors != len(fixture.old.Atoms) {
		t.Fatalf("preview did not prove the exact comparison: %#v", previewResult)
	}
	if previewResult.Claims != 1 || previewResult.VerificationsCarried != 1 || previewResult.ClaimsLeftUnverified != 0 {
		t.Fatalf("preview did not describe claim rollover: %#v", previewResult)
	}
	if after, readErr := os.ReadFile(fixture.evidencePath); readErr != nil || !bytes.Equal(after, evidenceBefore) {
		t.Fatalf("dry run changed evidence: equal=%v err=%v", bytes.Equal(after, evidenceBefore), readErr)
	}
	if entries, readErr := os.ReadDir(filepath.Join(fixture.root, "___requirements")); readErr == nil && len(entries) != 0 {
		t.Fatalf("dry run created requirements records: %#v", entries)
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}

	var output bytes.Buffer
	if err := RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, "--from-base", fixture.old.BaseOID, "--carry-verifications", "--json", fixture.root}, &output); err != nil {
		t.Fatal(err)
	}
	var result evidenceRebaseOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.DryRun || len(result.ClaimReplacements) != 1 {
		t.Fatalf("unexpected mutation result: %#v", result)
	}

	document, validation, err := saga.Load(fixture.root)
	if err != nil || !validation.Valid {
		t.Fatalf("rebased Saga valid=%v err=%v issues=%#v", validation.Valid, err, validation.Issues)
	}
	if len(document.Claims) != 2 || len(document.Verifications) != 2 {
		t.Fatalf("immutable records were not appended: claims=%d verifications=%d", len(document.Claims), len(document.Verifications))
	}
	if after, readErr := os.ReadFile(fixture.claimPath); readErr != nil || !bytes.Equal(after, claimBefore) {
		t.Fatalf("old claim was edited: equal=%v err=%v", bytes.Equal(after, claimBefore), readErr)
	}
	var replacement *saga.Claim
	for index := range document.Claims {
		if document.Claims[index].ID == result.ClaimReplacements[0].Replacement {
			replacement = &document.Claims[index]
		}
	}
	if replacement == nil || replacement.Statement != fixture.statement || len(replacement.Evidence) != 1 {
		t.Fatalf("replacement claim did not preserve assertion content: %#v", replacement)
	}
	reference, err := diffuri.Parse(replacement.Evidence[0])
	if err != nil || reference.Base != fixture.current.BaseOID || reference.Head != fixture.current.HeadOID {
		t.Fatalf("replacement claim evidence was not rebased: %#v err=%v", reference, err)
	}
	var carried *saga.Verification
	for index := range document.Verifications {
		if document.Verifications[index].Claim == replacement.ID {
			carried = &document.Verifications[index]
		}
	}
	if carried == nil || carried.Status != "verified" || carried.Method != "analysis" || !strings.Contains(carried.Summary, fixture.verificationID) {
		t.Fatalf("verification audit trail was not explicit: %#v", carried)
	}

	requirementDocument, err := requirements.Load(fixture.root, document.Manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirementDocument.Relations) != 1 {
		t.Fatalf("supersedes relation count = %d", len(requirementDocument.Relations))
	}
	relation := requirementDocument.Relations[0]
	if relation.Type != requirements.RelationSupersedes || !strings.HasSuffix(relation.From, ":claim:"+replacement.ID) || !strings.HasSuffix(relation.To, ":claim:"+fixture.claimID) {
		t.Fatalf("wrong claim lineage: %#v", relation)
	}
	report, err := buildReport(context.Background(), fixture.root, fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Summary.Covered != len(fixture.current.Atoms) || report.Summary.Orphaned != 0 {
		t.Fatalf("coverage did not survive exact rebase: %#v", report.Summary)
	}
	for _, file := range collectDiffFiles(document.Section) {
		for _, diff := range file.Diffs {
			parsed, parseErr := diffuri.Parse(diff.URI)
			if parseErr != nil || parsed.Base != fixture.current.BaseOID || parsed.Head != fixture.current.HeadOID || diff.Note != "preserve this note" {
				t.Fatalf("evidence details changed: %#v err=%v", diff, parseErr)
			}
		}
	}
	if err := RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, fixture.root}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no evidence uses an older base") {
		t.Fatalf("repeat migration should be a no-op refusal, got %v", err)
	}
	afterRepeat, _, err := saga.Load(fixture.root)
	if err != nil || len(afterRepeat.Claims) != 2 || len(afterRepeat.Verifications) != 2 {
		t.Fatalf("repeat migration duplicated immutable records: claims=%d verifications=%d err=%v", len(afterRepeat.Claims), len(afterRepeat.Verifications), err)
	}
}

func TestRebaseEvidenceRefusesChangedProductDiffWithoutWriting(t *testing.T) {
	fixture := newEvidenceRebaseFixture(t)
	evidenceBefore, err := os.ReadFile(fixture.evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(fixture.repo, "extra.go"), "package app\n\nconst ChangedAgain = true\n")
	git(t, fixture.repo, "add", "extra.go")
	git(t, fixture.repo, "commit", "-m", "change product after refresh")

	err = RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, fixture.root}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "product identity changed") {
		t.Fatalf("changed product comparison was accepted: %v", err)
	}
	if after, readErr := os.ReadFile(fixture.evidencePath); readErr != nil || !bytes.Equal(after, evidenceBefore) {
		t.Fatalf("refusal changed evidence: equal=%v err=%v", bytes.Equal(after, evidenceBefore), readErr)
	}
	document, _, loadErr := saga.Load(fixture.root)
	if loadErr != nil || len(document.Claims) != 1 || len(document.Verifications) != 1 {
		t.Fatalf("refusal appended immutable records: claims=%d verifications=%d err=%v", len(document.Claims), len(document.Verifications), loadErr)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.root, "___requirements")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refusal created requirements root: %v", statErr)
	}
}

func TestRebaseEvidenceLeavesReplacementClaimUnverifiedByDefault(t *testing.T) {
	fixture := newEvidenceRebaseFixture(t)
	if err := RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, fixture.root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	document, _, err := saga.Load(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Claims) != 2 || len(document.Verifications) != 1 {
		t.Fatalf("default should append an unverified replacement: claims=%d verifications=%d", len(document.Claims), len(document.Verifications))
	}
}

func TestRebaseEvidenceFailureRestoresEvidenceAndRemovesAppends(t *testing.T) {
	fixture := newEvidenceRebaseFixture(t)
	evidenceBefore, err := os.ReadFile(fixture.evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	evidenceRebaseFaultHook = func(step string) error {
		if step == "after-relation" {
			return errors.New("injected rebase failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceRebaseFaultHook = nil })

	err = RebaseEvidence(context.Background(), []string{"--repo", fixture.repo, "--carry-verifications", fixture.root}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "injected rebase failure") {
		t.Fatalf("injected failure was not returned: %v", err)
	}
	evidenceRebaseFaultHook = nil
	if after, readErr := os.ReadFile(fixture.evidencePath); readErr != nil || !bytes.Equal(after, evidenceBefore) {
		t.Fatalf("rollback did not restore evidence: equal=%v err=%v", bytes.Equal(after, evidenceBefore), readErr)
	}
	document, validation, loadErr := saga.Load(fixture.root)
	if loadErr != nil || !validation.Valid || len(document.Claims) != 1 || len(document.Verifications) != 1 {
		t.Fatalf("rollback left appended records: valid=%v claims=%d verifications=%d err=%v", validation.Valid, len(document.Claims), len(document.Verifications), loadErr)
	}
	requirementDocument, loadErr := requirements.Load(fixture.root, document.Manifest.ID)
	if loadErr != nil || len(requirementDocument.Relations) != 0 {
		t.Fatalf("rollback left a relation: relations=%d err=%v", len(requirementDocument.Relations), loadErr)
	}
}

type evidenceRebaseFixture struct {
	root           string
	repo           string
	evidencePath   string
	claimPath      string
	claimID        string
	verificationID string
	statement      string
	old            gitdiff.ChangeSet
	current        gitdiff.ChangeSet
}

func newEvidenceRebaseFixture(t *testing.T) evidenceRebaseFixture {
	t.Helper()
	ctx := context.Background()
	repo := t.TempDir()
	const repository = "https://example.test/acme/rebase.git"
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	git(t, repo, "remote", "add", "origin", repository)
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	git(t, repo, "branch", "release/2.5.3")
	git(t, repo, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(repo, "app.go"), "package app\n\nconst Ready = true\n")
	git(t, repo, "add", "app.go")
	git(t, repo, "commit", "-m", "feature")
	old, err := gitdiff.Read(ctx, repo, repository, "release/2.5.3", "feature")
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "rebase.saga")
	if err := Init(ctx, []string{"--repo", repo, "--repository", repository, "--base", "release/2.5.3", "--head", "feature", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := Upgrade(ctx, []string{"--to", "3", root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load fixture Saga: valid=%v err=%v", validation.Valid, err)
	}
	evidencePath := filepath.Join(root, "overview.fragment", "___diffs", "product.json")
	diffs := make([]saga.DiffReference, 0, len(old.Atoms))
	for _, atom := range old.Atoms {
		diffs = append(diffs, saga.DiffReference{URI: atom.URI, Note: "preserve this note"})
	}
	writeJSONTestFile(t, evidencePath, saga.DiffFile{Version: saga.CurrentVersion, Diffs: diffs})
	claimID := "claim-v2"
	statement := "The feature remains ready."
	claimPath := filepath.Join(root, "___claims", claimID+".json")
	writeJSONTestFile(t, claimPath, saga.Claim{
		Version: saga.CurrentVersion, ID: claimID, Target: document.Section.Fragments[0].Target,
		Kind: "behavior", Statement: statement, Evidence: []string{old.Atoms[0].URI}, CreatedAt: time.Now().UTC().Add(-time.Hour),
	})
	verificationID := "claim-v2-check"
	writeJSONTestFile(t, filepath.Join(root, "___verifications", verificationID+".json"), saga.Verification{
		Version: saga.CurrentVersion, ID: verificationID, Claim: claimID, Status: "verified", Method: "inspection",
		Summary: "Original evidence was inspected.", CreatedAt: time.Now().UTC().Add(-30 * time.Minute),
	})

	git(t, repo, "checkout", "release/2.5.3")
	writeFile(t, filepath.Join(repo, "e2e", "invitation.txt"), "fresh fixture\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "refresh e2e invitation fixture")
	git(t, repo, "checkout", "feature")
	git(t, repo, "merge", "--no-edit", "release/2.5.3")
	current, err := gitdiff.Read(ctx, repo, repository, "release/2.5.3", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if old.BaseOID == current.BaseOID || old.HeadOID != current.HeadOID || len(old.Atoms) != len(current.Atoms) {
		t.Fatalf("fixture did not isolate a base-only refresh: old=%s/%s/%d current=%s/%s/%d", old.BaseOID, old.HeadOID, len(old.Atoms), current.BaseOID, current.HeadOID, len(current.Atoms))
	}
	return evidenceRebaseFixture{
		root: root, repo: repo, evidencePath: evidencePath, claimPath: claimPath, claimID: claimID,
		verificationID: verificationID, statement: statement, old: old, current: current,
	}
}

func writeJSONTestFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(append(data, '\n')))
}
