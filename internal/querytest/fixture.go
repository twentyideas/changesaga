// Package querytest provides adversarial, real-Git fixtures shared by the
// review application and its transport integration tests.
package querytest

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	Repository       = "https://example.test/acme/query-security.git"
	OverviewTarget   = "urn:change-saga:security:fragment:overview"
	LargeTarget      = "urn:change-saga:security:fragment:large"
	ActiveHTMLTarget = "urn:change-saga:security:fragment:active-html"
	ActiveSVGTarget  = "urn:change-saga:security:fragment:active-svg"
)

// Fixture keeps the saga and its compared source in different Git worktrees.
// Query tests should always pass SourceDir explicitly; accidentally loading the
// source from SagaRepo makes this fixture fail instead of masking the bug.
type Fixture struct {
	testing testing.TB

	SourceDir string
	SagaRepo  string
	SagaRoot  string
	BaseOID   string
}

// New creates two independent, clean Git repositories. The source repository
// has a base commit and one product commit. The saga repository contains a
// minimal valid v2 saga which compares those source revisions.
func New(t testing.TB) *Fixture {
	t.Helper()
	root := t.TempDir()
	fixture := &Fixture{
		testing:   t,
		SourceDir: filepath.Join(root, "source"),
		SagaRepo:  filepath.Join(root, "saga-repository"),
	}
	fixture.SagaRoot = filepath.Join(fixture.SagaRepo, "security.saga")

	fixture.initRepository(fixture.SourceDir)
	fixture.git(fixture.SourceDir, "remote", "add", "origin", Repository)
	fixture.WriteSource("README.md", "base\n")
	fixture.git(fixture.SourceDir, "add", "README.md")
	fixture.git(fixture.SourceDir, "commit", "-m", "source base")
	fixture.BaseOID = strings.TrimSpace(fixture.git(fixture.SourceDir, "rev-parse", "HEAD"))
	fixture.git(fixture.SourceDir, "checkout", "-b", "feature")
	fixture.WriteSource("app.go", "package app\n\nconst Secure = true\n")
	fixture.git(fixture.SourceDir, "add", "app.go")
	fixture.git(fixture.SourceDir, "commit", "-m", "source feature")

	fixture.initRepository(fixture.SagaRepo)
	fixture.WriteSaga("saga.json", fmt.Sprintf(`{"version":2,"id":"security","title":"Query security","source":{"repository":%q,"base":%q,"head":"HEAD"}}`, Repository, fixture.BaseOID))
	fixture.WriteSaga("overview.fragment/fragment.json", `{"version":2,"id":"overview","title":"Overview","media_type":"text/markdown","entrypoint":"content.md"}`)
	fixture.WriteSaga("overview.fragment/content.md", "# Query security {#query-security}\n")
	fixture.git(fixture.SagaRepo, "add", ".")
	fixture.git(fixture.SagaRepo, "commit", "-m", "saga fixture")
	return fixture
}

// WriteSaga writes a slash-relative path below the saga root.
func (f *Fixture) WriteSaga(relative, content string) {
	f.testing.Helper()
	f.write(filepath.Join(f.SagaRoot, filepath.FromSlash(relative)), []byte(content))
}

// WriteSource writes a slash-relative path below the source worktree.
func (f *Fixture) WriteSource(relative, content string) {
	f.testing.Helper()
	f.write(filepath.Join(f.SourceDir, filepath.FromSlash(relative)), []byte(content))
}

// MakeInvalidManifest adds an unknown field. saga.Load returns a decode error
// for this case, which the query boundary must normalize to invalid_saga.
func (f *Fixture) MakeInvalidManifest() {
	f.WriteSaga("saga.json", fmt.Sprintf(`{"version":2,"id":"security","title":"Query security","unknown":true,"source":{"repository":%q,"base":%q,"head":"HEAD"}}`, Repository, f.BaseOID))
}

// AddAmbiguousTargets creates two structurally distinct fragments with the
// same public target. A session must reject the saga instead of selecting one
// according to traversal order.
func (f *Fixture) AddAmbiguousTargets() {
	for _, chapter := range []string{"alpha", "beta"} {
		f.WriteSaga(chapter+".chapter/chapter.json", fmt.Sprintf(`{"version":2,"id":%q,"title":%q}`, chapter, strings.ToUpper(chapter)))
		f.WriteSaga(chapter+".chapter/shared.fragment/fragment.json", `{"version":2,"id":"shared","title":"Shared","media_type":"text/plain","entrypoint":"content.txt"}`)
		f.WriteSaga(chapter+".chapter/shared.fragment/content.txt", chapter+"\n")
	}
}

// AddLargeFragment creates deterministic content without requiring a large
// checked-in fixture. Tests should request at least 1 MiB+1 to cross the
// documented maximum fragment chunk size.
func (f *Fixture) AddLargeFragment(size int) string {
	f.testing.Helper()
	if size < 1 {
		f.testing.Fatal("large fragment size must be positive")
	}
	f.WriteSaga("large.fragment/fragment.json", `{"version":2,"id":"large","title":"Large","media_type":"text/plain","entrypoint":"content.txt"}`)
	content := bytes.Repeat([]byte("0123456789abcdef"), (size+15)/16)[:size]
	f.write(filepath.Join(f.SagaRoot, "large.fragment", "content.txt"), content)
	return LargeTarget
}

// AddActiveFragments creates HTML and SVG whose scripts have obvious sentinel
// effects if evaluated. Read APIs must return bounded inert bytes; they must
// never load either document into an executing renderer.
func (f *Fixture) AddActiveFragments() (htmlTarget, svgTarget string) {
	f.WriteSaga("active-html.fragment/fragment.json", `{"version":2,"id":"active-html","title":"Active HTML","media_type":"text/html","entrypoint":"index.html"}`)
	f.WriteSaga("active-html.fragment/index.html", `<!doctype html><script>document.documentElement.dataset.aiQueryExecuted="html"</script><p>safe to read</p>`)
	f.WriteSaga("active-svg.fragment/fragment.json", `{"version":2,"id":"active-svg","title":"Active SVG","media_type":"image/svg+xml","entrypoint":"image.svg"}`)
	f.WriteSaga("active-svg.fragment/image.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>document.documentElement.dataset.aiQueryExecuted="svg"</script><text>safe to read</text></svg>`)
	return ActiveHTMLTarget, ActiveSVGTarget
}

// AddEscapingEntrypointSymlink replaces the overview entrypoint with a link to
// a secret outside the saga. Session opening must reject this as invalid_saga
// (or unsafe_path) without returning the secret.
func (f *Fixture) AddEscapingEntrypointSymlink() string {
	f.testing.Helper()
	outside := filepath.Join(filepath.Dir(f.SagaRepo), "entrypoint-secret.txt")
	f.write(outside, []byte("ENTRYPOINT_SECRET_MUST_NOT_LEAK\n"))
	entrypoint := filepath.Join(f.SagaRoot, "overview.fragment", "content.md")
	if err := os.Remove(entrypoint); err != nil {
		f.testing.Fatal(err)
	}
	if err := os.Symlink(outside, entrypoint); err != nil {
		f.testing.Skipf("symlinks unavailable: %v", err)
	}
	return outside
}

// AddEscapingAssetSymlink adds a non-entrypoint asset link. This distinction is
// important: the saga loader can remain valid while an asset enumerator still
// has to omit or reject the escaping link.
func (f *Fixture) AddEscapingAssetSymlink() string {
	f.testing.Helper()
	outside := filepath.Join(filepath.Dir(f.SagaRepo), "asset-secret.txt")
	f.write(outside, []byte("ASSET_SECRET_MUST_NOT_LEAK\n"))
	link := filepath.Join(f.SagaRoot, "overview.fragment", "leak.txt")
	if err := os.Symlink(outside, link); err != nil {
		f.testing.Skipf("symlinks unavailable: %v", err)
	}
	return outside
}

// AdvanceSagaSnapshot changes relevant fragment content without committing it.
// A cursor obtained before this call must not be accepted against the new
// snapshot.
func (f *Fixture) AdvanceSagaSnapshot() {
	f.WriteSaga("overview.fragment/content.md", "# Query security changed {#query-security-changed}\n")
}

// CursorAttack is a syntactically hostile derivative of a valid opaque cursor.
type CursorAttack struct {
	Name  string
	Token string
}

// TamperedCursors returns transport-safe cursor corruptions. The query API must
// classify all of them as invalid_argument and must not panic or expose cursor
// internals. Snapshot staleness is tested separately with AdvanceSagaSnapshot.
func TamperedCursors(valid string) []CursorAttack {
	truncated := "."
	bitFlip := "_"
	if len(valid) > 1 {
		truncated = valid[:len(valid)/2]
		if valid[0] == 'A' {
			bitFlip = "B" + valid[1:]
		} else {
			bitFlip = "A" + valid[1:]
		}
	}
	return []CursorAttack{
		{Name: "truncated", Token: truncated},
		{Name: "bit flip", Token: bitFlip},
		{Name: "suffix", Token: valid + "A"},
		{Name: "leading whitespace", Token: " " + valid},
		{Name: "non-base64 bytes", Token: "%00%ff"},
	}
}

// TreeState captures observable worktree and Git state while ignoring Git's
// mutable internal bookkeeping. It detects content changes, new files,
// permission changes, staging, and commits caused by a read request.
type TreeState struct {
	Files        []FileState
	SourceHEAD   string
	SagaHEAD     string
	SourceStatus string
	SagaStatus   string
}

type FileState struct {
	Path   string
	Mode   fs.FileMode
	Digest string
}

// State returns the current state of both repositories.
func (f *Fixture) State() TreeState {
	f.testing.Helper()
	files := append(f.tree("source", f.SourceDir), f.tree("saga", f.SagaRepo)...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return TreeState{
		Files:        files,
		SourceHEAD:   strings.TrimSpace(f.git(f.SourceDir, "rev-parse", "HEAD")),
		SagaHEAD:     strings.TrimSpace(f.git(f.SagaRepo, "rev-parse", "HEAD")),
		SourceStatus: f.git(f.SourceDir, "status", "--porcelain=v2", "--untracked-files=all"),
		SagaStatus:   f.git(f.SagaRepo, "status", "--porcelain=v2", "--untracked-files=all"),
	}
}

// AssertUnchanged fails if a read operation changed either worktree, staged
// content, or created a commit.
func (f *Fixture) AssertUnchanged(before TreeState) {
	f.testing.Helper()
	after := f.State()
	if !reflect.DeepEqual(before, after) {
		f.testing.Fatalf("query had filesystem or Git side effects\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// LeakedAbsolutePath returns the fixture root exposed by an application or CLI
// error, including its slash-normalized and JSON-escaped spelling. Empty means
// the output contains none of the fixture's absolute paths.
func (f *Fixture) LeakedAbsolutePath(output string) string {
	for _, root := range []string{f.SagaRoot, f.SagaRepo, f.SourceDir} {
		spellings := []string{
			root,
			filepath.ToSlash(root),
			strings.ReplaceAll(root, `\`, `\\`),
		}
		for _, spelling := range spellings {
			if spelling != "" && strings.Contains(output, spelling) {
				return root
			}
		}
	}
	return ""
}

// AssertNoAbsolutePaths checks the default diagnostic privacy contract.
func (f *Fixture) AssertNoAbsolutePaths(output string) {
	f.testing.Helper()
	if leaked := f.LeakedAbsolutePath(output); leaked != "" {
		f.testing.Fatalf("query output leaked absolute path %q: %s", leaked, output)
	}
}

func (f *Fixture) initRepository(dir string) {
	f.testing.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.testing.Fatal(err)
	}
	f.git(dir, "init", "-b", "main")
	f.git(dir, "config", "user.name", "Query Fixture")
	f.git(dir, "config", "user.email", "query-fixture@example.test")
}

func (f *Fixture) write(path string, content []byte) {
	f.testing.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.testing.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		f.testing.Fatal(err)
	}
}

func (f *Fixture) git(dir string, args ...string) string {
	f.testing.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		f.testing.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func (f *Fixture) tree(prefix, root string) []FileState {
	f.testing.Helper()
	var states []FileState
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := FileState{Path: prefix + "/" + filepath.ToSlash(relative), Mode: info.Mode()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256([]byte(target))
			state.Digest = fmt.Sprintf("%x", digest)
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(content)
			state.Digest = fmt.Sprintf("%x", digest)
		}
		states = append(states, state)
		return nil
	})
	if err != nil {
		f.testing.Fatal(err)
	}
	return states
}
