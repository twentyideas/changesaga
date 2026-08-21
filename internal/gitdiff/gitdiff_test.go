package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

func TestParseLinesAndEvents(t *testing.T) {
	patch := []byte(`diff --git a/app.go b/app.go
index 1111111..2222222 100644
--- a/app.go
+++ b/app.go
@@ -2,2 +2,2 @@
-old one
-old two
+new one
+new two
diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
diff --git "a/script name.sh" "b/script name.sh"
old mode 100644
new mode 100755
diff --git a/logo.png b/logo.png
index 3333333..4444444 100644
Binary files a/logo.png and b/logo.png differ
`)
	atoms, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(atoms), 7; got != want {
		t.Fatalf("got %d atoms, want %d: %#v", got, want, atoms)
	}
	wants := []struct {
		key     string
		content string
	}{
		{"line:app.go:old:2", "old one"},
		{"line:app.go:old:3", "old two"},
		{"line:app.go:new:2", "new one"},
		{"line:app.go:new:3", "new two"},
		{"event:rename:new.go:old.go:new.go", ""},
		{"event:mode:script name.sh::", ""},
		{"event:binary:logo.png::", ""},
	}
	for i, want := range wants {
		if atoms[i].Key != want.key || atoms[i].Content != want.content {
			t.Errorf("atom %d = %#v, want key %q content %q", i, atoms[i], want.key, want.content)
		}
	}
}

func TestParseKeepsDisplayContextOutOfCoverageAtoms(t *testing.T) {
	patch := []byte(`diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -8,3 +8,3 @@
 unchanged before
-old value
+new value
 unchanged after
`)
	atoms, lines, err := parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("coverage atoms = %d, want only added and removed lines", len(atoms))
	}
	if len(lines) != 4 || lines[0].Kind != "context" || lines[0].OldLine != 8 || lines[0].NewLine != 8 || lines[1].Kind != "old" || lines[2].Kind != "new" || lines[3].Kind != "context" {
		t.Fatalf("unexpected display lines: %#v", lines)
	}
}

func TestParseDoesNotConfuseChangedContentWithFileHeaders(t *testing.T) {
	patch := []byte(`diff --git a/comments.txt b/comments.txt
--- a/comments.txt
+++ b/comments.txt
@@ -1,2 +1,2 @@
--- old comment
-old tail
+++ new value
+new tail
`)
	atoms, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"line:comments.txt:old:1", "line:comments.txt:old:2",
		"line:comments.txt:new:1", "line:comments.txt:new:2",
	}
	if len(atoms) != len(want) {
		t.Fatalf("header-like source lines were lost: %#v", atoms)
	}
	for i := range want {
		if atoms[i].Key != want[i] {
			t.Errorf("atom %d = %q, want %q", i, atoms[i].Key, want[i])
		}
	}
}

func TestIsSagaPath(t *testing.T) {
	tests := map[string]bool{
		"pr-12.saga/title.md":              true,
		"docs/reviews/pr-12.saga/a/x.json": true,
		"docs/saga/title.md":               false,
		"service.saga.go":                  false,
	}
	for path, want := range tests {
		if got := IsSagaPath(path); got != want {
			t.Errorf("IsSagaPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestProductIdentityIgnoresSagaOnlyCommits(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.name", "Test")
	gitTest(t, repo, "config", "user.email", "test@example.test")
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/app.git")
	writeGitTestFile(t, filepath.Join(repo, "README.md"), "base\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	writeGitTestFile(t, filepath.Join(repo, "app.go"), "package app\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "product")
	productHead := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))

	before, err := Read(context.Background(), repo, "https://example.test/acme/app.git", base, productHead)
	if err != nil {
		t.Fatal(err)
	}
	writeGitTestFile(t, filepath.Join(repo, "pr-1.saga", "note.txt"), "review metadata\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "review")
	after, err := Read(context.Background(), repo, "https://example.test/acme/app.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if before.HeadOID != after.HeadOID || len(before.Atoms) != 2 || len(after.Atoms) != 2 || before.Atoms[0].URI != after.Atoms[0].URI || before.Atoms[1].URI != after.Atoms[1].URI {
		t.Fatalf("saga-only commit changed product identity: before=%#v after=%#v", before, after)
	}
	if len(after.SagaChanges) != 2 {
		t.Fatalf("saga-only change should still be reported: %#v", after.SagaChanges)
	}
}

func TestParseFileLifecycleAndDefensiveModify(t *testing.T) {
	patch := []byte(`diff --git a/empty-new b/empty-new
new file mode 100644
index 0000000..e69de29
diff --git a/empty-old b/empty-old
deleted file mode 100644
index e69de29..0000000
diff --git a/node b/node
old mode 100644
new mode 120000
diff --git a/metadata-only b/metadata-only
index 1111111..2222222 100644
`)
	atoms, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"event:add:empty-new::",
		"event:delete:empty-old::",
		"event:type-change:node::",
		"event:modify:metadata-only::",
	}
	if len(atoms) != len(want) {
		t.Fatalf("atoms = %#v, want keys %v", atoms, want)
	}
	for i := range want {
		if atoms[i].Key != want[i] {
			t.Errorf("atom %d key = %q, want %q", i, atoms[i].Key, want[i])
		}
	}
}

func TestAdversarialGitFixtureCorpus(t *testing.T) {
	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/corpus.git")
	writeGitTestFile(t, filepath.Join(repo, "empty-delete"), "")
	writeGitTestFile(t, filepath.Join(repo, "text-delete.txt"), "old one\nold two\n")
	writeGitTestFile(t, filepath.Join(repo, "text-modify.txt"), "before\n")
	writeGitTestFile(t, filepath.Join(repo, "rename-clean.txt"), "unchanged rename\n")
	writeGitTestFile(t, filepath.Join(repo, "rename-edited.txt"), "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	writeGitTestFile(t, filepath.Join(repo, "script.sh"), "#!/bin/sh\necho ok\n")
	writeGitTestFile(t, filepath.Join(repo, "node"), "regular node\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "fixture base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))

	if err := os.Remove(filepath.Join(repo, "empty-delete")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "text-delete.txt")); err != nil {
		t.Fatal(err)
	}
	writeGitTestFile(t, filepath.Join(repo, "empty-add"), "")
	writeGitTestFile(t, filepath.Join(repo, "text-add.txt"), "new one\nnew two\n")
	writeGitTestFile(t, filepath.Join(repo, "text-modify.txt"), "after\n")
	writeGitTestBytes(t, filepath.Join(repo, "binary-new.bin"), []byte{0, 1, 2, 3, 0xff})
	gitTest(t, repo, "mv", "rename-clean.txt", "renamed clean.txt")
	gitTest(t, repo, "mv", "rename-edited.txt", "renamed-edited.txt")
	writeGitTestFile(t, filepath.Join(repo, "renamed-edited.txt"), "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	writeGitTestBytes(t, filepath.Join(repo, "crlf.txt"), []byte("first\r\nsecond\r\n"))
	writeGitTestFile(t, filepath.Join(repo, "no-final-newline.txt"), "last line")
	writeGitTestFile(t, filepath.Join(repo, "unicodé space.txt"), "snowman ☃\n")
	writeGitTestFile(t, filepath.Join(repo, "review.saga", "note.md"), "saga only\n")
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(repo, "script.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(repo, "node")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("text-add.txt", filepath.Join(repo, "node")); err != nil {
			t.Fatal(err)
		}
	}
	gitTest(t, repo, "add", "-A")
	gitTest(t, repo, "commit", "-m", "adversarial feature")
	baseline, err := Read(context.Background(), repo, "https://example.test/acme/corpus.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Poison common diff settings. Read must still produce the same canonical
	// prefixes, quoting, rename detection, submodule form, and algorithm.
	gitTest(t, repo, "config", "core.quotePath", "false")
	gitTest(t, repo, "config", "diff.noprefix", "true")
	gitTest(t, repo, "config", "diff.srcPrefix", "OLD/")
	gitTest(t, repo, "config", "diff.dstPrefix", "NEW/")
	gitTest(t, repo, "config", "diff.submodule", "log")
	gitTest(t, repo, "config", "diff.algorithm", "histogram")
	gitTest(t, repo, "config", "diff.context", "50")
	gitTest(t, repo, "config", "diff.interHunkContext", "50")

	changes, err := Read(context.Background(), repo, "https://example.test/acme/corpus.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if changes.HeadOID != baseline.HeadOID || len(changes.Atoms) != len(baseline.Atoms) {
		t.Fatalf("repository diff config changed canonical comparison: before=%#v after=%#v", baseline, changes)
	}
	keys := map[string]bool{}
	paths := map[string]bool{}
	for _, atom := range changes.Atoms {
		keys[atom.Key] = true
		paths[atom.Path] = true
		paths[atom.OldPath] = true
		paths[atom.NewPath] = true
		if _, err := diffuri.Parse(atom.URI); err != nil {
			t.Errorf("atom %q has invalid canonical URI: %v", atom.Key, err)
		}
	}
	for _, key := range []string{
		"event:rename:empty-add:empty-delete:empty-add",
		"event:add:text-add.txt::", "event:delete:text-delete.txt::",
		"event:binary:binary-new.bin::",
		"event:rename:renamed clean.txt:rename-clean.txt:renamed clean.txt",
		"event:rename:renamed-edited.txt:rename-edited.txt:renamed-edited.txt",
		"line:text-modify.txt:old:1", "line:text-modify.txt:new:1",
		"line:crlf.txt:new:1", "line:no-final-newline.txt:new:1",
		"line:unicodé space.txt:new:1",
	} {
		if !keys[key] {
			t.Errorf("fixture corpus omitted %q; keys=%v", key, keys)
		}
	}
	if runtime.GOOS != "windows" {
		for _, key := range []string{"event:mode:script.sh::", "event:type-change:node::"} {
			if !keys[key] {
				t.Errorf("fixture corpus omitted %q", key)
			}
		}
	}
	for _, productPath := range []string{
		"empty-add", "empty-delete", "text-add.txt", "text-delete.txt", "text-modify.txt",
		"binary-new.bin", "renamed clean.txt", "renamed-edited.txt", "crlf.txt",
		"no-final-newline.txt", "unicodé space.txt",
	} {
		if !paths[productPath] {
			t.Errorf("Git-reported product path %q yielded no coverage atom", productPath)
		}
	}
	if len(changes.SagaChanges) == 0 {
		t.Fatal("saga-only fixture was not classified separately")
	}
}

func TestEmptyFileAddAndDeleteProduceLifecycleAtoms(t *testing.T) {
	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/empty.git")
	writeGitTestFile(t, filepath.Join(repo, "empty-delete"), "")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, repo, "checkout", "-b", "delete-empty")
	if err := os.Remove(filepath.Join(repo, "empty-delete")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "-A")
	gitTest(t, repo, "commit", "-m", "delete empty")
	deleted, err := Read(context.Background(), repo, "https://example.test/acme/empty.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !hasAtomKey(deleted.Atoms, "event:delete:empty-delete::") {
		t.Fatalf("empty delete yielded no lifecycle atom: %#v", deleted.Atoms)
	}

	gitTest(t, repo, "checkout", "-b", "add-empty", base)
	writeGitTestFile(t, filepath.Join(repo, "empty-add"), "")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "add empty")
	added, err := Read(context.Background(), repo, "https://example.test/acme/empty.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !hasAtomKey(added.Atoms, "event:add:empty-add::") {
		t.Fatalf("empty add yielded no lifecycle atom: %#v", added.Atoms)
	}
}

func TestCommittedAndWorktreeComparisonsUseActualMergeBase(t *testing.T) {
	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/topology.git")
	writeGitTestFile(t, filepath.Join(repo, "root.txt"), "root\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "root")
	root := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, repo, "checkout", "-b", "feature")
	writeGitTestFile(t, filepath.Join(repo, "feature.txt"), "committed\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "feature")
	feature := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, repo, "checkout", "main")
	writeGitTestFile(t, filepath.Join(repo, "advanced-base.txt"), "not part of feature\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "advanced base")

	committed, err := Read(context.Background(), repo, "https://example.test/acme/topology.git", "main", feature)
	if err != nil {
		t.Fatal(err)
	}
	if committed.BaseOID != root || hasAtomPath(committed.Atoms, "advanced-base.txt") || !hasAtomPath(committed.Atoms, "feature.txt") {
		t.Fatalf("committed comparison did not use merge base %s: %#v", root, committed)
	}

	gitTest(t, repo, "checkout", "feature")
	writeGitTestFile(t, filepath.Join(repo, "worktree.txt"), "uncommitted\n")
	writeGitTestFile(t, filepath.Join(repo, "untracked.txt"), "excluded\n")
	gitTest(t, repo, "add", "worktree.txt")
	worktree, err := Read(context.Background(), repo, "https://example.test/acme/topology.git", "main", "WORKTREE")
	if err != nil {
		t.Fatal(err)
	}
	if worktree.BaseOID != root || hasAtomPath(worktree.Atoms, "advanced-base.txt") || hasAtomPath(worktree.Atoms, "untracked.txt") || !hasAtomPath(worktree.Atoms, "feature.txt") || !hasAtomPath(worktree.Atoms, "worktree.txt") {
		t.Fatalf("worktree comparison did not diff merge-base tree to worktree: %#v", worktree)
	}
	gitTest(t, repo, "commit", "-m", "worktree committed")
	head, err := Read(context.Background(), repo, "https://example.test/acme/topology.git", "main", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	cleanWorktree, err := Read(context.Background(), repo, "https://example.test/acme/topology.git", "main", "WORKTREE")
	if err != nil {
		t.Fatal(err)
	}
	if head.HeadOID != cleanWorktree.HeadOID {
		t.Fatalf("clean HEAD and WORKTREE identities differ: %s != %s", head.HeadOID, cleanWorktree.HeadOID)
	}
}

func TestRenameAcrossSagaBoundaryRemainsAProductAtom(t *testing.T) {
	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/boundary.git")
	writeGitTestFile(t, filepath.Join(repo, "product.txt"), "one\ntwo\nthree\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	if err := os.MkdirAll(filepath.Join(repo, "review.saga"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "mv", "product.txt", "review.saga/product.txt")
	gitTest(t, repo, "commit", "-m", "move product into saga")
	changes, err := Read(context.Background(), repo, "https://example.test/acme/boundary.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	key := "event:rename:review.saga/product.txt:product.txt:review.saga/product.txt"
	if !hasAtomKey(changes.Atoms, key) || !hasAtomKey(changes.SagaChanges, key) {
		t.Fatalf("cross-boundary rename must be both product and saga-visible: %#v", changes)
	}
}

func TestProductIdentitySurvivesRebaseLikeCommitIdentityChange(t *testing.T) {
	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/rebase.git")
	writeGitTestFile(t, filepath.Join(repo, "base.txt"), "base\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, repo, "checkout", "-b", "first")
	writeGitTestFile(t, filepath.Join(repo, "same.txt"), "same patch\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "first identity")
	first, err := Read(context.Background(), repo, "https://example.test/acme/rebase.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "checkout", "-b", "second", base)
	writeGitTestFile(t, filepath.Join(repo, "same.txt"), "same patch\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "different commit identity")
	second, err := Read(context.Background(), repo, "https://example.test/acme/rebase.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if first.HeadOID != second.HeadOID || len(first.Atoms) != len(second.Atoms) {
		t.Fatalf("equivalent rebased product patch changed identity: %#v %#v", first, second)
	}
}

func TestReadVerifiesCheckoutRepositoryWithExplicitOverride(t *testing.T) {
	repo := newGitTestRepo(t)
	writeGitTestFile(t, filepath.Join(repo, "base.txt"), "base\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	writeGitTestFile(t, filepath.Join(repo, "change.txt"), "change\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "change")
	gitTest(t, repo, "remote", "add", "origin", "https://user:secret@example.test/acme/actual.git")
	_, err := Read(context.Background(), repo, "https://example.test/acme/other.git", base, "HEAD")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("repository mismatch was not safely rejected: %v", err)
	}
	changes, err := ReadWithOptions(context.Background(), repo, "https://example.test/acme/other.git", base, "HEAD", ReadOptions{AllowRepositoryMismatch: true})
	if err != nil || len(changes.Atoms) == 0 {
		t.Fatalf("explicit repository mismatch override failed: %#v %v", changes, err)
	}
}

func TestReadRequiresOverrideWhenCheckoutOriginIsUnavailable(t *testing.T) {
	repo := newGitTestRepo(t)
	writeGitTestFile(t, filepath.Join(repo, "base.txt"), "base\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	writeGitTestFile(t, filepath.Join(repo, "change.txt"), "change\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "change")
	_, err := Read(context.Background(), repo, "https://example.test/acme/declared.git", base, "HEAD")
	if err == nil || !strings.Contains(err.Error(), "no origin") {
		t.Fatalf("unverifiable checkout was accepted: %v", err)
	}
	if _, err := ReadWithOptions(context.Background(), repo, "https://example.test/acme/declared.git", base, "HEAD", ReadOptions{AllowRepositoryMismatch: true}); err != nil {
		t.Fatalf("explicit override did not permit originless checkout: %v", err)
	}
}

func TestRepositoryCorrespondenceIgnoresTransportAndGitSuffix(t *testing.T) {
	if !sameRepository("https://example.test/acme/app", "ssh://example.test/acme/app.git") {
		t.Fatal("equivalent HTTPS and SSH repository identities did not correspond")
	}
	if sameRepository("https://example.test/acme/app", "https://example.test/other/app") {
		t.Fatal("different repository paths corresponded")
	}
}

func TestSubmoduleGitlinkChangeProducesAtoms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local file-transport submodule fixture is verified on Unix CI")
	}
	child := newGitTestRepo(t)
	writeGitTestFile(t, filepath.Join(child, "child.txt"), "one\n")
	gitTest(t, child, "add", ".")
	gitTest(t, child, "commit", "-m", "one")
	firstChild := strings.TrimSpace(gitTest(t, child, "rev-parse", "HEAD"))
	writeGitTestFile(t, filepath.Join(child, "child.txt"), "two\n")
	gitTest(t, child, "add", ".")
	gitTest(t, child, "commit", "-m", "two")

	repo := newGitTestRepo(t)
	gitTest(t, repo, "remote", "add", "origin", "https://example.test/acme/submodule.git")
	gitTest(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", child, "deps/child")
	gitTest(t, filepath.Join(repo, "deps/child"), "checkout", firstChild)
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "submodule base")
	base := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, filepath.Join(repo, "deps/child"), "checkout", "main")
	gitTest(t, repo, "add", "deps/child")
	gitTest(t, repo, "commit", "-m", "advance gitlink")
	changes, err := Read(context.Background(), repo, "https://example.test/acme/submodule.git", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Atoms) == 0 || !hasAtomPath(changes.Atoms, "deps/child") {
		t.Fatalf("submodule change yielded no coverage atoms: %#v", changes)
	}
}

func hasAtomPath(atoms []Atom, path string) bool {
	for _, atom := range atoms {
		if atom.Path == path || atom.OldPath == path || atom.NewPath == path {
			return true
		}
	}
	return false
}

func hasAtomKey(atoms []Atom, key string) bool {
	for _, atom := range atoms {
		if atom.Key == key {
			return true
		}
	}
	return false
}

func newGitTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.name", "Test")
	gitTest(t, repo, "config", "user.email", "test@example.test")
	return repo
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeGitTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGitTestBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
