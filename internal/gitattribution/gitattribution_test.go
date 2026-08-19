package gitattribution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
