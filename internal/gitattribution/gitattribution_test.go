package gitattribution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverUsesIntroducingCommitCommitter(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Default")
	git(t, repo, "config", "user.email", "default@example.test")
	path := filepath.Join(repo, "event.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "event.json")
	command := exec.Command("git", "commit", "-m", "add event")
	command.Dir = repo
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Payload Author", "GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME=Canonical Committer", "GIT_COMMITTER_EMAIL=committer@example.test",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}

	value := New(context.Background(), repo).Resolve(context.Background(), path)
	if value.State != Committed || value.Name != "Canonical Committer" || value.Email != "committer@example.test" || value.CommitID == "" || value.CommittedAt.IsZero() {
		t.Fatalf("unexpected attribution: %#v", value)
	}
}

func TestResolverReportsUncommittedAndUnavailable(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	path := filepath.Join(repo, "local.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := New(context.Background(), repo)
	if value := resolver.Resolve(context.Background(), path); value.State != Uncommitted {
		t.Fatalf("local attribution = %#v", value)
	}
	stagedPath := filepath.Join(repo, "staged.json")
	if err := os.WriteFile(stagedPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "staged.json")
	if value := resolver.Resolve(context.Background(), stagedPath); value.State != Uncommitted {
		t.Fatalf("staged attribution = %#v", value)
	}
	if value := resolver.Resolve(context.Background(), filepath.Join(t.TempDir(), "outside.json")); value.State != Unavailable {
		t.Fatalf("outside attribution = %#v", value)
	}
}

func TestResolverFollowsRenameToIntroducingCommit(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Default")
	git(t, repo, "config", "user.email", "default@example.test")
	original := filepath.Join(repo, "event.json")
	if err := os.WriteFile(original, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "event.json")
	gitWithIdentity(t, repo, "Introducing Reviewer", "introducer@example.test", "commit", "-m", "add event")
	wantCommit := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "mv", "event.json", "renamed.json")
	git(t, repo, "commit", "-m", "rename event")

	value := New(context.Background(), repo).Resolve(context.Background(), filepath.Join(repo, "renamed.json"))
	if value.State != Committed || value.Name != "Introducing Reviewer" || value.Email != "introducer@example.test" || value.CommitID != wantCommit {
		t.Fatalf("renamed attribution = %#v", value)
	}
}

func TestResolverRecomputesAfterHistoryRewrite(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Default")
	git(t, repo, "config", "user.email", "default@example.test")
	path := filepath.Join(repo, "event.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "event.json")
	gitWithIdentity(t, repo, "Before Rewrite", "before@example.test", "commit", "-m", "event")
	before := New(context.Background(), repo).Resolve(context.Background(), path)
	gitWithIdentity(t, repo, "After Rewrite", "after@example.test", "commit", "--amend", "--no-edit")
	after := New(context.Background(), repo).Resolve(context.Background(), path)
	if before.CommitID == after.CommitID || after.State != Committed || after.Name != "After Rewrite" || after.Email != "after@example.test" {
		t.Fatalf("rewrite was not reflected: before=%#v after=%#v", before, after)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func gitWithIdentity(t *testing.T, dir, name, email string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Git Author", "GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
