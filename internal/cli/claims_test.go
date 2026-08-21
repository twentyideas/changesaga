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

func TestClaimsAndVerificationsAreIndependentAppendOnlyRecords(t *testing.T) {
	root := newAuthoredSaga(t)
	uri, err := diffuri.Build(diffuri.Reference{
		Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb",
		Kind: "line", Path: "worker.go", Side: "new", Start: 3, End: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := AddClaim(context.Background(), []string{
		"--id", "single-flight", "--target", "overview.fragment", "--kind", "invariant",
		"--statement", "Only one sampler can run at a time.", "--diff", uri, root,
	}, &output); err != nil {
		t.Fatal(err)
	}
	if err := VerifyClaim(context.Background(), []string{
		"--id", "single-flight-check", "--claim", "single-flight", "--status", "verified",
		"--method", "test", "--summary", "The concurrency test passed.", "--command", "go test ./...", root,
	}, &output); err != nil {
		t.Fatal(err)
	}
	if err := VerifyClaim(context.Background(), []string{
		"--id", "single-flight-recheck", "--claim", "single-flight", "--status", "inconclusive",
		"--method", "analysis", "--summary", "Exception cleanup still needs inspection.", root,
	}, &output); err != nil {
		t.Fatal(err)
	}

	document, validation, err := saga.Load(root)
	if err != nil || !validation.Valid {
		t.Fatalf("load: validation=%#v err=%v", validation, err)
	}
	if len(document.Claims) != 1 || len(document.Verifications) != 2 {
		t.Fatalf("records were consolidated: claims=%#v verifications=%#v", document.Claims, document.Verifications)
	}
	claim := document.Claims[0]
	if claim.Target != saga.FragmentTarget("atomic", "atomic-overview") || claim.Statement != "Only one sampler can run at a time." || len(claim.Evidence) != 1 {
		t.Fatalf("claim = %#v", claim)
	}
	for _, path := range []string{
		filepath.Join(root, "___claims", "single-flight.json"),
		filepath.Join(root, "___verifications", "single-flight-check.json"),
		filepath.Join(root, "___verifications", "single-flight-recheck.json"),
	} {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing independent record %s: %v", path, statErr)
		}
	}
}

func TestClaimFailuresDoNotWriteRecords(t *testing.T) {
	root := newAuthoredSaga(t)
	var output bytes.Buffer
	err := AddClaim(context.Background(), []string{
		"--id", "bad", "--target", "overview.fragment", "--kind", "behavior",
		"--statement", "This should not be written.", "--diff", "not-a-uri", root,
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "invalid --diff") {
		t.Fatalf("invalid evidence error = %v", err)
	}
	if entries, readErr := os.ReadDir(filepath.Join(root, "___claims")); readErr != nil || len(entries) != 0 {
		t.Fatalf("failed claim wrote records: entries=%v err=%v", entries, readErr)
	}
	err = VerifyClaim(context.Background(), []string{
		"--id", "bad-check", "--claim", "missing", "--status", "verified", "--method", "test",
		"--summary", "No claim exists.", root,
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing claim error = %v", err)
	}
	if entries, readErr := os.ReadDir(filepath.Join(root, "___verifications")); readErr != nil || len(entries) != 0 {
		t.Fatalf("failed verification wrote records: entries=%v err=%v", entries, readErr)
	}
}
