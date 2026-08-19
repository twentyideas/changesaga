package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		{"event:binary:logo.png:logo.png:logo.png", ""},
	}
	for i, want := range wants {
		if atoms[i].Key != want.key || atoms[i].Content != want.content {
			t.Errorf("atom %d = %#v, want key %q content %q", i, atoms[i], want.key, want.content)
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
	if before.HeadOID != after.HeadOID || len(before.Atoms) != 1 || len(after.Atoms) != 1 || before.Atoms[0].URI != after.Atoms[0].URI {
		t.Fatalf("saga-only commit changed product identity: before=%#v after=%#v", before, after)
	}
	if len(after.SagaChanges) != 1 {
		t.Fatalf("saga-only change should still be reported: %#v", after.SagaChanges)
	}
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
