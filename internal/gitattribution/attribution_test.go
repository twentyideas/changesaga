package gitattribution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUsesIntroducingCommitterAndFollowsRename(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, nil, "init", "-b", "main")
	sagaRoot := filepath.Join(repo, "review.saga")
	original := filepath.Join(sagaRoot, "___review", "diffs", "event.json")
	writeTestEvent(t, original)
	gitTest(t, repo, nil, "add", ".")
	gitTest(t, repo, identity("Payload Author", "author@example.test", "Reviewer One", "one@example.test"), "commit", "-m", "add event")
	wantCommit := strings.TrimSpace(gitTest(t, repo, nil, "rev-parse", "HEAD"))

	renamed := filepath.Join(sagaRoot, "___review", "diffs", "renamed.json")
	gitTest(t, repo, nil, "mv", original, renamed)
	gitTest(t, repo, identity("Another Author", "other@example.test", "Reviewer Two", "two@example.test"), "commit", "-m", "rename event")

	got := Resolve(context.Background(), sagaRoot, []string{renamed})[clean(renamed)]
	if got.Status != Committed || got.Commit != wantCommit || got.Committer == nil || got.Committer.Name != "Reviewer One" || got.Committer.Email != "one@example.test" || got.CommittedAt == nil {
		t.Fatalf("unexpected attribution: %#v", got)
	}
}

func TestResolveReportsLocalAndUnavailableStates(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, nil, "init", "-b", "main")
	tracked := filepath.Join(repo, "review.saga", "saga.json")
	writeTestEvent(t, tracked)
	gitTest(t, repo, nil, "add", ".")
	gitTest(t, repo, identity("Author", "author@example.test", "Reviewer", "reviewer@example.test"), "commit", "-m", "base")
	local := filepath.Join(repo, "review.saga", "___review", "diffs", "local.json")
	writeTestEvent(t, local)
	gitTest(t, repo, nil, "add", filepath.ToSlash(local))
	got := Resolve(context.Background(), filepath.Join(repo, "review.saga"), []string{local})[clean(local)]
	if got.Status != Uncommitted || got.Committer != nil || got.Commit != "" {
		t.Fatalf("local attribution = %#v", got)
	}

	outside := filepath.Join(t.TempDir(), "review.saga", "event.json")
	writeTestEvent(t, outside)
	got = Resolve(context.Background(), filepath.Dir(outside), []string{outside})[clean(outside)]
	if got.Status != HistoryUnavailable {
		t.Fatalf("outside-git attribution = %#v", got)
	}
}

func TestResolveRecomputesAfterHistoryRewrite(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, nil, "init", "-b", "main")
	event := filepath.Join(repo, "review.saga", "event.json")
	writeTestEvent(t, event)
	gitTest(t, repo, nil, "add", ".")
	gitTest(t, repo, identity("Author", "author@example.test", "Before Rewrite", "before@example.test"), "commit", "-m", "event")
	first := Resolve(context.Background(), filepath.Dir(event), []string{event})[clean(event)]
	gitTest(t, repo, identity("Author", "author@example.test", "After Rewrite", "after@example.test"), "commit", "--amend", "--no-edit")
	second := Resolve(context.Background(), filepath.Dir(event), []string{event})[clean(event)]
	if first.Commit == second.Commit || second.Committer == nil || second.Committer.Name != "After Rewrite" || second.Status != Committed {
		t.Fatalf("rewrite was not reflected: before=%#v after=%#v", first, second)
	}
}

func identity(authorName, authorEmail, committerName, committerEmail string) []string {
	return []string{
		"GIT_AUTHOR_NAME=" + authorName, "GIT_AUTHOR_EMAIL=" + authorEmail,
		"GIT_COMMITTER_NAME=" + committerName, "GIT_COMMITTER_EMAIL=" + committerEmail,
	}
}

func gitTest(t *testing.T, dir string, environment []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeTestEvent(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
