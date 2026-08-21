package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/change-saga/change-saga/internal/saga"
)

func TestAuthoringLoopAgainstGitDiff(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(repo, "app.go"), "package app\n\nconst Ready = true\n")
	git(t, repo, "add", "app.go")
	git(t, repo, "commit", "-m", "feature")

	root := filepath.Join(t.TempDir(), "pr-1.saga")
	var output bytes.Buffer
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", "https://example.test/acme/app.git", "--base", base, "--head", "HEAD", "--title", "Feature", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := AddChapter(context.Background(), []string{"--title", "Backend behavior", root, "backend"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := AddSection(context.Background(), []string{"--title", "Request flow", root, "backend.chapter/request-flow"}, &output); err != nil {
		t.Fatal(err)
	}
	report, err := buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 3 || report.Complete {
		t.Fatalf("expected three uncovered lines: %#v", report.Summary)
	}
	output.Reset()
	if err := Cover(context.Background(), []string{"--repo", repo, "--target", "overview.fragment", "--path", "app.go", "--side", "new", "--lines", "1-3", root}, &output); err != nil {
		t.Fatal(err)
	}
	report, err = buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Summary.Covered != 3 {
		t.Fatalf("expected complete report: %#v", report)
	}
	if len(report.Targets) != 1 || !strings.Contains(report.Targets[0].Target, ":fragment:") {
		t.Fatalf("coverage should belong to a fragment: %#v", report.Targets)
	}
	if err := AddFragment(context.Background(), []string{"--section", ".", "--type", "html", "--title", "Interactive flow", root}, &output); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(t.TempDir(), "demo")
	writeFile(t, filepath.Join(packageDir, "index.html"), `<script src="app.js"></script>`)
	writeFile(t, filepath.Join(packageDir, "app.js"), `document.body.append('interactive')`)
	if err := AddFragment(context.Background(), []string{"--section", ".", "--type", "html", "--title", "Bundled demo", "--source", packageDir, root}, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bundled-demo.fragment", "app.js")); err != nil {
		t.Fatalf("fragment package dependency was not copied: %v", err)
	}
	attachment := filepath.Join(t.TempDir(), "note.svg")
	writeFile(t, attachment, `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`)
	if err := Thread(context.Background(), []string{"--target", "overview.fragment", "--body", "Please clarify this.", "--attachment", attachment, root}, &output); err != nil {
		t.Fatal(err)
	}
	threadDocument, _, err := saga.Load(root)
	if err != nil || len(threadDocument.Threads) != 1 {
		t.Fatalf("created thread should load: document=%#v err=%v", threadDocument, err)
	}
	threadID := threadDocument.Threads[0].ID
	if err := Reply(context.Background(), []string{"--thread", threadID, "--state", "withdrawn", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := Reply(context.Background(), []string{"--thread", threadID, "--state", "open", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := Review(context.Background(), []string{"--target", ".", "--state", "approved", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := Review(context.Background(), []string{"--target", "overview.fragment", "--state", "approved", root}, &output); err != nil {
		t.Fatal(err)
	}
	document, validation, err := saga.Load(root)
	fragmentReviews := 0
	for _, fragment := range document.Section.Fragments {
		fragmentReviews += len(fragment.Reviews)
	}
	if err != nil || !validation.Valid || len(document.Threads) != 1 || len(document.Threads[0].Events) != 2 || document.Threads[0].State != "open" || len(document.Section.Reviews) != 1 || fragmentReviews != 1 || len(document.Threads[0].Messages[0].Fragments) != 2 {
		t.Fatalf("authored saga should load with thread: validation=%#v err=%v", validation, err)
	}
	if len(document.Section.Children) != 1 || document.Section.Children[0].Kind != "chapter" || len(document.Section.Children[0].Children) != 1 {
		t.Fatalf("chapter hierarchy was not authored: %#v", document.Section.Children)
	}
}

func TestInstallSkillPrintsPortableAuthoringContract(t *testing.T) {
	var output bytes.Buffer
	if err := InstallSkill(nil, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"project-local agent skill", "existing PR-authoring", "thing to be reviewed, not the review itself",
		"Do not create review", "change-saga --help", "change-saga status --json", "SVG diagram",
		"interactive HTML", "data flows", "data models", "exact diff atoms",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("install-skill output omitted %q", expected)
		}
	}
	for _, platformPath := range []string{".claude/", ".opencode/", ".codex/"} {
		if strings.Contains(text, platformPath) {
			t.Errorf("install-skill hard-coded agent path %q", platformPath)
		}
	}
	if err := InstallSkill([]string{"unexpected"}, &output); err == nil {
		t.Fatal("install-skill accepted positional arguments")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
