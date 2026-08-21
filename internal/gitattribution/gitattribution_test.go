package gitattribution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkResolverLargeSaga(b *testing.B) {
	repo := b.TempDir()
	benchmarkGit(b, repo, "init", "-b", "main")
	benchmarkGit(b, repo, "config", "user.name", "Benchmark")
	benchmarkGit(b, repo, "config", "user.email", "benchmark@example.test")
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = filepath.Join(repo, fmt.Sprintf("reviews/%03d.json", i))
		if err := os.MkdirAll(filepath.Dir(paths[i]), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(paths[i], []byte("{}\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkGit(b, repo, "add", ".")
	benchmarkGit(b, repo, "commit", "-m", "add review events")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		resolver := New(context.Background(), repo)
		for _, path := range paths {
			if value := resolver.Resolve(context.Background(), path); value.State != Committed {
				b.Fatalf("attribution for %s = %#v", path, value)
			}
		}
	}
}

func benchmarkGit(tb testing.TB, dir string, args ...string) {
	tb.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

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

func TestResolverLoadsRepositoryIndexOnce(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Default")
	git(t, repo, "config", "user.email", "default@example.test")
	paths := make([]string, 4)
	for i := range paths {
		paths[i] = filepath.Join(repo, fmt.Sprintf("reviews/%d.json", i))
		if err := os.MkdirAll(filepath.Dir(paths[i]), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[i], []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "review records")

	resolver := New(context.Background(), repo)
	commands := map[string]int{}
	resolver.command = func(ctx context.Context, args ...string) *exec.Cmd {
		for _, arg := range args {
			switch arg {
			case "status", "ls-files", "log":
				commands[arg]++
			}
		}
		return gitCommand(ctx, args...)
	}
	for _, path := range paths {
		if value := resolver.Resolve(context.Background(), path); value.State != Committed {
			t.Fatalf("attribution for %s = %#v", path, value)
		}
	}
	resolver.Resolve(context.Background(), paths[0])
	if commands["status"] != 1 || commands["ls-files"] != 1 || commands["log"] != len(paths) {
		t.Fatalf("Git commands = %v, want one status and ls-files plus one log per unique path", commands)
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
