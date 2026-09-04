package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestAuthoringLoopAgainstGitDiff(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	git(t, repo, "remote", "add", "origin", "https://example.test/acme/app.git")
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
	bootstrap, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read reviewer bootstrap: %v", err)
	}
	for _, expected := range []string{
		"Review this Change Saga",
		"change-saga open",
		"ask the user",
		"change-saga query overview",
		"change-saga query mappings",
		"change-saga query claims",
		"code diff independently",
		"All-atoms-mapped detects omissions only",
	} {
		if !strings.Contains(string(bootstrap), expected) {
			t.Errorf("reviewer bootstrap omitted %q", expected)
		}
	}
	rootScaffold, err := os.ReadFile(filepath.Join(root, "overview.fragment", "content.md"))
	if err != nil || len(rootScaffold) != 0 {
		t.Fatalf("root overview should start empty, not expose authoring instructions: content=%q err=%v", rootScaffold, err)
	}
	writeFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Feature {#feature}\n\nThe change at a glance.\n")
	if err := AddChapter(context.Background(), []string{"--title", "Backend behavior", root, "backend"}, &output); err != nil {
		t.Fatal(err)
	}
	chapterScaffold, err := os.ReadFile(filepath.Join(root, "backend.chapter", "overview.fragment", "content.md"))
	if err != nil || len(chapterScaffold) != 0 {
		t.Fatalf("chapter overview should start empty, not expose authoring instructions: content=%q err=%v", chapterScaffold, err)
	}
	chapterManifest, err := os.ReadFile(filepath.Join(root, "backend.chapter", "overview.fragment", "fragment.json"))
	if err != nil || strings.Contains(string(chapterManifest), "Chapter overview") {
		t.Fatalf("chapter overview leaked renderer-facing scaffold metadata: manifest=%q err=%v", chapterManifest, err)
	}
	writeFile(t, filepath.Join(root, "backend.chapter", "overview.fragment", "content.md"), "# Backend behavior {#backend-behavior}\n\nReview this boundary independently.\n")
	if err := AddSection(context.Background(), []string{"--title", "Request flow", root, "backend.chapter/request-flow"}, &output); err != nil {
		t.Fatal(err)
	}
	report, err := buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 4 || report.Complete {
		t.Fatalf("expected an uncovered add event and three lines: %#v", report.Summary)
	}
	output.Reset()
	if err := Cover(context.Background(), []string{"--repo", repo, "--target", "overview.fragment", "--path", "app.go", "--side", "new", "--lines", "1-3", root}, &output); err != nil {
		t.Fatal(err)
	}
	if err := Cover(context.Background(), []string{"--repo", repo, "--target", "overview.fragment", "--path", "app.go", "--event", "add", root}, &output); err != nil {
		t.Fatal(err)
	}
	report, err = buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Summary.Covered != 4 {
		t.Fatalf("expected complete report: %#v", report)
	}
	if len(report.Targets) != 1 || !strings.Contains(report.Targets[0].Target, ":fragment:") {
		t.Fatalf("coverage should belong to a fragment: %#v", report.Targets)
	}
	if err := AddFragment(context.Background(), []string{"--section", ".", "--type", "html", "--title", "Interactive flow", root}, &output); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "interactive-flow.fragment", "index.html"), `<!doctype html><title>Flow</title><p id="flow">Interactive behavior</p>`)
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
		"Do not create review", "change-saga --help", "change-saga status --json", "change-saga add-landmark", "SVG diagram",
		"interactive HTML", "data flows", "data models", "exact diff atoms",
		"zero citations", "code-bearing SVG/HTML", "node, edge, arrow, transition", "SVG element bounds become on-canvas links automatically",
		"change-saga query mappings --sort scrutiny", "change-saga add-claim", "change-saga verify-claim",
		"change-saga compare --json", "must_update", "new_content", "source diffs only",
		"read the code diff independently", "All-atoms-mapped is an omission invariant",
		"Storyboard visual questions", "system-context diagram", "state machine", "entity-relationship diagram",
		"Silhouette test", "Relationship test", "Contact-sheet test", "Do not use cards as a universal container",
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

func TestSpecJSONExposesPurposeFitVisualFormsAndAudits(t *testing.T) {
	var output bytes.Buffer
	if err := Spec([]string{"--json"}, &output); err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	v4, ok := contract["slide_native_v4"].(map[string]any)
	if !ok {
		t.Fatalf("spec omitted slide-native contract: %#v", contract)
	}
	forms, ok := v4["visual_forms"].(map[string]any)
	if !ok || forms["data-flow"] == nil || forms["state-machine"] == nil || forms["entity-relationship"] == nil || forms["failure-path"] == nil {
		t.Fatalf("spec omitted purpose-fit visual forms: %#v", forms)
	}
	audits, ok := v4["composition_audits"].([]any)
	if !ok || len(audits) != 3 {
		t.Fatalf("spec omitted composition audits: %#v", v4["composition_audits"])
	}
}

func TestInitRequiresPortableRepositoryOrExplicitLocalOptIn(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	var output bytes.Buffer
	root := filepath.Join(t.TempDir(), "portable.saga")
	if err := Init(context.Background(), []string{"--repo", repo, root}, &output); err == nil || !strings.Contains(err.Error(), "portable --repository") {
		t.Fatalf("Init without origin should require a deliberate identity: %v", err)
	}
	if err := Init(context.Background(), []string{"--repo", repo, "--allow-local-repository", root}, &output); err != nil {
		t.Fatal(err)
	}
	document, _, err := saga.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(document.Manifest.Source.Repository, "file://") {
		t.Fatalf("local opt-in did not record a file URI: %q", document.Manifest.Source.Repository)
	}
}

func TestRepositoryDiscoveryCanonicalizesCredentialsAndChecksMismatch(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "remote", "add", "origin", "https://user:secret@EXAMPLE.TEST/acme/app.git/")
	repository, _, err := discoverRepository(context.Background(), repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if repository != "https://example.test/acme/app.git" || strings.Contains(repository, "user") || strings.Contains(repository, "secret") {
		t.Fatalf("origin was not safely canonicalized: %q", repository)
	}
	if _, _, err := discoverRepository(context.Background(), repo, "https://example.test/other/repo.git"); err == nil {
		t.Fatal("mismatched declared repository was accepted")
	}
	overridden, _, err := discoverRepository(context.Background(), repo, "https://example.test/other/repo.git", false, true)
	if err != nil || overridden != "https://example.test/other/repo.git" {
		t.Fatalf("explicit mismatch override failed: %q %v", overridden, err)
	}
}

func TestNormalizeRepositoryURIStripsSCPUser(t *testing.T) {
	got, err := normalizeRepositoryURI("git@example.test:acme/app.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh://example.test/acme/app.git" {
		t.Fatalf("normalized repository = %q", got)
	}
}

func TestRepositoryDiscoveryRequiresOptInForLocalOrigin(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	localOrigin := t.TempDir()
	git(t, repo, "remote", "add", "origin", localOrigin)
	if _, _, err := discoverRepository(context.Background(), repo, ""); err == nil || !strings.Contains(err.Error(), "local file repository") {
		t.Fatalf("local origin was persisted without opt-in: %v", err)
	}
	repository, _, err := discoverRepository(context.Background(), repo, "", true)
	if err != nil || !strings.HasPrefix(repository, "file://") {
		t.Fatalf("local origin opt-in failed: %q %v", repository, err)
	}
}

func TestCoverRejectsURIForDifferentRepository(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	root := filepath.Join(t.TempDir(), "different.saga")
	var output bytes.Buffer
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", "https://example.test/acme/source.git", root}, &output); err != nil {
		t.Fatal(err)
	}
	uri, err := diffuri.Build(diffuri.Reference{Repository: "https://example.test/acme/other.git", Base: "a", Head: "b", Kind: "event", Event: "add", Path: "empty.txt"})
	if err != nil {
		t.Fatal(err)
	}
	err = Cover(context.Background(), []string{"--uri", uri, root}, &output)
	if err == nil || !strings.Contains(err.Error(), "does not match saga source repository") {
		t.Fatalf("foreign repository URI was accepted: %v", err)
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
