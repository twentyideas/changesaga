package gitattribution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestResolverBatchesRepositoryQueriesAndCaches(t *testing.T) {
	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"committed.json", "untracked.json", "staged.json", "missing.json"}
	for _, name := range paths[:3] {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var commands [][]string
	resolver := &Resolver{
		root:  repo,
		cache: map[string]Attribution{},
		command: func(_ context.Context, args ...string) ([]byte, error) {
			commands = append(commands, slices.Clone(args))
			switch {
			case slices.Contains(args, "status"):
				return []byte("?? untracked.json\x00A  staged.json\x00"), nil
			case slices.Contains(args, "ls-files"):
				return []byte("committed.json\x00staged.json\x00"), nil
			case slices.Contains(args, "log"):
				if !slices.Contains(args, "--follow") || args[len(args)-1] != "committed.json" {
					t.Fatalf("log did not preserve per-path --follow semantics: %v", args)
				}
				return []byte("0123456789abcdef\x00Reviewer\x00reviewer@example.test\x002026-01-01T00:00:00Z\n"), nil
			default:
				return nil, fmt.Errorf("unexpected git command: %v", args)
			}
		},
	}
	absolute := make([]string, len(paths))
	for index, path := range paths {
		absolute[index] = filepath.Join(repo, path)
	}
	values := resolver.ResolveAll(context.Background(), absolute)
	want := []string{Committed, Uncommitted, Uncommitted, Unavailable}
	for index := range want {
		if values[index].State != want[index] {
			t.Errorf("result[%d] = %#v, want state %q", index, values[index], want[index])
		}
	}
	if values[0].Name != "Reviewer" || values[0].Email != "reviewer@example.test" {
		t.Fatalf("committed attribution = %#v", values[0])
	}
	if len(commands) != 3 {
		t.Fatalf("git command count = %d, want one status, one ls-files, and one --follow log: %v", len(commands), commands)
	}
	resolver.ResolveAll(context.Background(), absolute)
	if len(commands) != 3 {
		t.Fatalf("cached batch ran more Git commands: %v", commands)
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
