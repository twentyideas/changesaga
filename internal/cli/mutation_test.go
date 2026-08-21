package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/change-saga/change-saga/internal/saga"
)

// newAuthoredSaga returns a valid saga backed by a real repository, because
// every authoring command reloads and revalidates the saga it is about to
// change.
func newAuthoredSaga(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	root := filepath.Join(t.TempDir(), "atomic.saga")
	var output bytes.Buffer
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", "https://example.test/acme/app.git", root}, &output); err != nil {
		t.Fatal(err)
	}
	assertValid(t, root)
	return root
}

func assertValid(t *testing.T, root string) {
	t.Helper()
	_, validation, err := saga.Load(root)
	if err != nil {
		t.Fatalf("load %s: %v", root, err)
	}
	if !validation.Valid {
		t.Fatalf("saga became invalid: %#v", validation.Issues)
	}
}

// A fragment that is missing its entrypoint invalidates the whole saga, and an
// invalid saga blocks every later review mutation. A failed add-fragment must
// therefore leave nothing behind at all.
func TestFailedAddFragmentLeavesNoPartialPackage(t *testing.T) {
	root := newAuthoredSaga(t)
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "styles.css"), "body{}\n")

	var output bytes.Buffer
	err := AddFragment(context.Background(), []string{"--type", "html", "--name", "walkthrough", "--source", source, root}, &output)
	if err == nil {
		t.Fatal("a source directory without the entrypoint must fail")
	}
	if !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries, statErr := os.ReadDir(root); statErr == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "walkthrough") {
				t.Fatalf("a failed fragment left %s behind", entry.Name())
			}
			if strings.HasPrefix(entry.Name(), ".change-saga-") {
				t.Fatalf("a failed fragment left staging state %s behind", entry.Name())
			}
		}
	}
	assertValid(t, root)

	// The name is still free, so the author can retry after fixing the source.
	writeFile(t, filepath.Join(source, "index.html"), "<p>ok</p>\n")
	output.Reset()
	if err := AddFragment(context.Background(), []string{"--type", "html", "--name", "walkthrough", "--source", source, root}, &output); err != nil {
		t.Fatalf("retry after a failed attempt: %v", err)
	}
	assertValid(t, root)
}

func TestAddFragmentRejectsUnusableEntrypoints(t *testing.T) {
	root := newAuthoredSaga(t)
	cases := []string{"../escape.html", "/etc/passwd", `sub\index.html`, "___diffs/a.json", "fragment.json", "C:/windows/win.ini"}
	for i, entrypoint := range cases {
		var output bytes.Buffer
		name := fmt.Sprintf("case-%d", i)
		err := AddFragment(context.Background(), []string{"--type", "html", "--name", name, "--entrypoint", entrypoint, root}, &output)
		if err == nil {
			t.Fatalf("entrypoint %q was accepted", entrypoint)
		}
		if _, statErr := os.Stat(filepath.Join(root, name+".fragment")); statErr == nil {
			t.Fatalf("entrypoint %q left a fragment behind", entrypoint)
		}
	}
	assertValid(t, root)
}

func TestAddFragmentSupportsNestedEntrypoints(t *testing.T) {
	root := newAuthoredSaga(t)
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "assets", "index.html"), "<p>ok</p>\n")
	var output bytes.Buffer
	if err := AddFragment(context.Background(), []string{"--type", "html", "--name", "nested", "--source", source, "--entrypoint", "assets/index.html", root}, &output); err != nil {
		t.Fatal(err)
	}
	assertValid(t, root)
	document, _, err := saga.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, fragment := range document.Section.Fragments {
		if fragment.Entrypoint == "assets/index.html" {
			found = true
		}
	}
	if !found {
		t.Fatal("the nested entrypoint was not stored as a slash path")
	}
}

// Authoring commands take the same writer lock the review overlay uses, so
// concurrent writers serialize instead of interleaving half-built entities.
func TestConcurrentAuthoringKeepsTheSagaValid(t *testing.T) {
	root := newAuthoredSaga(t)
	const writers = 8
	var wait sync.WaitGroup
	failures := make([]error, writers)
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			var output bytes.Buffer
			failures[index] = AddChapter(context.Background(), []string{"--title", fmt.Sprintf("Chapter %d", index), root, fmt.Sprintf("chapter-%d", index)}, &output)
		}(i)
	}
	wait.Wait()
	for index, err := range failures {
		if err != nil {
			t.Fatalf("writer %d failed: %v", index, err)
		}
	}
	assertValid(t, root)
	document, _, err := saga.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Section.Children) != writers {
		t.Fatalf("expected %d chapters, found %d", writers, len(document.Section.Children))
	}
}

// Two writers racing for the same chapter name must not both appear to win, and
// the loser must not corrupt the winner's directory.
func TestConcurrentDuplicateChapterNamesResolveToOne(t *testing.T) {
	root := newAuthoredSaga(t)
	const writers = 6
	var wait sync.WaitGroup
	results := make([]error, writers)
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			var output bytes.Buffer
			results[index] = AddChapter(context.Background(), []string{"--title", "Shared", root, "shared"}, &output)
		}(i)
	}
	wait.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		if !strings.Contains(err.Error(), "already used") && !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "busy") {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one winner, got %d", successes)
	}
	assertValid(t, root)
}

func TestInitLeavesNothingBehindWhenTheRepositoryIsUnusable(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	root := filepath.Join(t.TempDir(), "broken.saga")
	var output bytes.Buffer
	err := Init(context.Background(), []string{"--repo", repo, "--repository", "not-a-uri", root}, &output)
	if err == nil {
		t.Fatal("an unusable repository identity must fail")
	}
	if _, statErr := os.Stat(root); statErr == nil {
		t.Fatal("a failed init left a saga directory behind that would block a retry")
	}
}

// init publishes the saga with one rename, but its containing directories are
// ordinary parents: a missing parent is created, and a symlinked ancestor (the
// normal shape of macOS /tmp) must not be mistaken for a symlink inside a saga.
func TestInitCreatesMissingParentsAndToleratesSymlinkedAncestors(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "T")
	git(t, repo, "config", "user.email", "t@example.test")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")

	workspace := t.TempDir()
	var output bytes.Buffer
	nested := filepath.Join(workspace, "reviews", "2026", "deep.saga")
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", "https://example.test/acme/app.git", nested}, &output); err != nil {
		t.Fatalf("init through missing parents: %v", err)
	}
	assertValid(t, nested)

	real := filepath.Join(workspace, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	output.Reset()
	through := filepath.Join(link, "linked.saga")
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", "https://example.test/acme/app.git", through}, &output); err != nil {
		t.Fatalf("init under a symlinked ancestor: %v", err)
	}
	assertValid(t, through)
	output.Reset()
	if err := AddChapter(context.Background(), []string{"--title", "Backend", through, "backend"}, &output); err != nil {
		t.Fatalf("add-chapter under a symlinked ancestor: %v", err)
	}
	assertValid(t, through)
}

func TestReplyRejectsAnUnstableThreadIdentifier(t *testing.T) {
	root := newAuthoredSaga(t)
	var output bytes.Buffer
	for _, threadID := range []string{"../escape", "a/b", "..", "with space"} {
		err := Reply(context.Background(), []string{"--thread", threadID, "--body", "hello", root}, &output)
		if err == nil {
			t.Fatalf("thread id %q was accepted", threadID)
		}
		if !strings.Contains(err.Error(), "stable identifier") && !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("thread id %q: unexpected error %v", threadID, err)
		}
	}
	assertValid(t, root)
}
