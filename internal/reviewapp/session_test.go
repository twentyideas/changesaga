package reviewapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

type serviceFixture struct {
	repo       string
	root       string
	fragment   string
	atomURI    string
	fileURI    string
	asset      string
	session    Session
	comparison gitdiff.ChangeSet
}

func TestSessionReadOperations(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "overview", run: func(t *testing.T) {
			value, err := fixture.session.Overview(ctx, OverviewQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if value.Saga.ID != "query-test" || value.Source.BaseOID == "" || value.Source.HeadOID == "" || len(value.OverviewFragments) != 1 || len(value.Chapters) != 1 {
				t.Fatalf("unexpected overview: %#v", value)
			}
			if value.Coverage.Uncovered == 0 || value.Coverage.Overlapping != 1 || value.Coverage.Stale != 1 || value.Coverage.Complete {
				t.Fatalf("unexpected coverage summary: %#v", value.Coverage)
			}
		}},
		{name: "children paginate", run: func(t *testing.T) {
			root := saga.SagaTarget("query-test")
			first, err := fixture.session.Children(ctx, ChildrenQuery{Parent: root, Limit: 1})
			if err != nil || len(first.Children) != 1 || first.Page.NextCursor == nil {
				t.Fatalf("first page = %#v, err = %v", first, err)
			}
			second, err := fixture.session.Children(ctx, ChildrenQuery{Parent: root, Limit: 1, Cursor: *first.Page.NextCursor})
			if err != nil || len(second.Children) != 1 || second.Page.NextCursor != nil || first.Children[0].Target == second.Children[0].Target {
				t.Fatalf("second page = %#v, err = %v", second, err)
			}
		}},
		{name: "fragment chunk and assets", run: func(t *testing.T) {
			value, err := fixture.session.ReadFragment(ctx, FragmentQuery{Target: fixture.fragment, Limit: 7})
			if err != nil {
				t.Fatal(err)
			}
			if value.Content.Encoding != "utf-8" || value.Content.NextOffset == nil || !strings.HasPrefix(value.Content.SHA256, "sha256:") || len(value.Assets) != 1 || value.Assets[0].Name != "diagram.png" {
				t.Fatalf("unexpected fragment: %#v", value)
			}
			next, err := fixture.session.ReadFragment(ctx, FragmentQuery{Target: fixture.fragment, Offset: *value.Content.NextOffset, Limit: 64})
			if err != nil || next.Content.NextOffset != nil {
				t.Fatalf("last fragment chunk = %#v, err = %v", next, err)
			}
		}},
		{name: "fragment diffs", run: func(t *testing.T) {
			value, err := fixture.session.FragmentDiffs(ctx, FragmentDiffQuery{Target: fixture.fragment})
			if err != nil {
				t.Fatal(err)
			}
			if len(value.Selectors) != 2 || len(value.Atoms) != 1 || len(value.Stale) != 1 || value.Stale[0].Target != fixture.fragment {
				t.Fatalf("unexpected fragment diffs: %#v", value)
			}
		}},
		{name: "higher-level target diffs", run: func(t *testing.T) {
			target := saga.SagaTarget("query-test")
			value, err := fixture.session.FragmentDiffs(ctx, FragmentDiffQuery{Target: target})
			if err != nil {
				t.Fatal(err)
			}
			if value.Target != target || len(value.Selectors) != 1 || len(value.Atoms) != 1 || len(value.Stale) != 0 {
				t.Fatalf("unexpected saga-level diffs: %#v", value)
			}
		}},
		{name: "atom owners are bidirectional", run: func(t *testing.T) {
			value, err := fixture.session.DiffOwners(ctx, DiffOwnerQuery{Diff: fixture.atomURI})
			if err != nil {
				t.Fatal(err)
			}
			if value.Kind != "line" || len(value.Atoms) != 1 || len(value.Atoms[0].Owners) != 2 || len(value.Atoms[0].Threads) != 1 {
				t.Fatalf("unexpected atom ownership: %#v", value)
			}
		}},
		{name: "file owners group atoms", run: func(t *testing.T) {
			value, err := fixture.session.DiffOwners(ctx, DiffOwnerQuery{Diff: fixture.fileURI, Limit: 1})
			if err != nil || value.Kind != "file" || len(value.Atoms) != 1 || value.Page.NextCursor == nil {
				t.Fatalf("unexpected file ownership: %#v, err=%v", value, err)
			}
		}},
		{name: "reviews normalize content and attribution", run: func(t *testing.T) {
			value, err := fixture.session.Reviews(ctx, ReviewQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if len(value.Items) != 3 {
				t.Fatalf("review items = %#v", value.Items)
			}
			filtered, err := fixture.session.Reviews(ctx, ReviewQuery{Thread: "thread-1", State: "open"})
			if err != nil || len(filtered.Items) != 1 || filtered.Items[0].Thread == nil || filtered.Items[0].Thread.Attribution.Status != "committed" || filtered.Items[0].Thread.Messages[0].Fragments[0].Data != "Please clarify.\n" {
				t.Fatalf("filtered reviews = %#v, err=%v", filtered, err)
			}
		}},
		{name: "gaps expose public kinds", run: func(t *testing.T) {
			for _, kind := range []string{"uncovered", "stale", "overlap"} {
				value, err := fixture.session.Gaps(ctx, GapQuery{Kind: kind})
				if err != nil || len(value.Gaps) == 0 {
					t.Fatalf("%s gaps = %#v, err=%v", kind, value, err)
				}
				for _, gap := range value.Gaps {
					if gap.Kind != kind {
						t.Fatalf("%s query returned %q", kind, gap.Kind)
					}
				}
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func TestSessionStableErrorsSnapshotAndCursor(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()
	root := saga.SagaTarget("query-test")
	first, err := fixture.session.Children(ctx, ChildrenQuery{Parent: root, Limit: 1})
	if err != nil || first.Page.NextCursor == nil {
		t.Fatalf("create cursor: %#v, %v", first, err)
	}
	originalSnapshot := fixture.session.Snapshot()
	unchanged, err := Open(ctx, OpenOptions{SagaRoot: fixture.root, SourceDir: fixture.repo})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Snapshot() != originalSnapshot {
		t.Fatalf("unchanged snapshot moved: %q != %q", unchanged.Snapshot(), originalSnapshot)
	}
	writeFile(t, fixture.asset, "different ignored asset bytes")
	newSession, err := Open(ctx, OpenOptions{SagaRoot: fixture.root, SourceDir: fixture.repo})
	if err != nil {
		t.Fatal(err)
	}
	if newSession.Snapshot() == originalSnapshot {
		t.Fatal("snapshot did not include readable fragment assets")
	}
	_, err = newSession.Children(ctx, ChildrenQuery{Parent: root, Cursor: *first.Page.NextCursor})
	assertCode(t, err, CodeStaleSnapshot)

	tests := []struct {
		name string
		run  func() error
		code ErrorCode
	}{
		{name: "missing target", code: CodeNotFound, run: func() error {
			_, err := fixture.session.Children(ctx, ChildrenQuery{Parent: "urn:change-saga:query-test:section:missing"})
			return err
		}},
		{name: "bad cursor", code: CodeInvalidArgument, run: func() error {
			_, err := fixture.session.Gaps(ctx, GapQuery{Cursor: "not-base64"})
			return err
		}},
		{name: "oversize page", code: CodeInvalidArgument, run: func() error {
			_, err := fixture.session.Reviews(ctx, ReviewQuery{Limit: MaxPageLimit + 1})
			return err
		}},
		{name: "bad diff", code: CodeInvalidArgument, run: func() error {
			_, err := fixture.session.DiffOwners(ctx, DiffOwnerQuery{Diff: "app.go"})
			return err
		}},
		{name: "bad gap kind", code: CodeInvalidArgument, run: func() error {
			_, err := fixture.session.Gaps(ctx, GapQuery{Kind: "orphan"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertCode(t, test.run(), test.code) })
	}
}

func TestOpenReportsInvalidSagaAndUnavailableSource(t *testing.T) {
	ctx := context.Background()
	invalidRoot := filepath.Join(t.TempDir(), "invalid.saga")
	writeFile(t, filepath.Join(invalidRoot, "saga.json"), `{}`)
	_, err := Open(ctx, OpenOptions{SagaRoot: invalidRoot})
	assertCode(t, err, CodeInvalidSaga)
	var domain *Error
	if !errors.As(err, &domain) || len(domain.Details) == 0 {
		t.Fatalf("invalid saga omitted validation details: %#v", err)
	}

	fixture := newServiceFixture(t)
	_, err = Open(ctx, OpenOptions{SagaRoot: fixture.root, SourceDir: t.TempDir()})
	assertCode(t, err, CodeSourceUnavailable)
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	ctx := context.Background()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Query Tester")
	git(t, repo, "config", "user.email", "query@example.test")
	git(t, repo, "remote", "add", "origin", "https://example.test/acme/query.git")
	writeFile(t, filepath.Join(repo, "app.go"), "package app\n\nconst Ready = false\n")
	git(t, repo, "add", "app.go")
	git(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(repo, "app.go"), "package app\n\nconst Ready = true\nconst Mode = \"query\"\n")
	git(t, repo, "add", "app.go")
	git(t, repo, "commit", "-m", "product change")
	comparison, err := gitdiff.Read(ctx, repo, "https://example.test/acme/query.git", base, "HEAD")
	if err != nil || len(comparison.Atoms) < 2 {
		t.Fatalf("build real comparison: atoms=%d err=%v", len(comparison.Atoms), err)
	}
	current := comparison.Atoms[0]
	stale, err := diffuri.Build(diffuri.Reference{
		Repository: comparison.Repository, Base: comparison.BaseOID, Head: comparison.HeadOID,
		Kind: "line", Path: "missing.go", Side: "new", Start: 99, End: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	fileURI, err := diffuri.Build(diffuri.Reference{
		Repository: comparison.Repository, Base: comparison.BaseOID, Head: comparison.HeadOID,
		Kind: "file", Path: atomFilePath(current),
	})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repo, "review.saga")
	fragmentTarget := saga.FragmentTarget("query-test", "overview")
	writeJSON(t, filepath.Join(root, "saga.json"), saga.Manifest{
		Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: "query-test", Title: "Query test",
		Source: saga.Source{Repository: comparison.Repository, Base: base, Head: "HEAD"},
	})
	writeJSON(t, filepath.Join(root, "___diffs", "root.json"), saga.DiffFile{Version: 2, Diffs: []saga.DiffReference{{URI: current.URI, Note: "root ownership"}}})
	writeJSON(t, filepath.Join(root, "overview.fragment", "fragment.json"), saga.FragmentManifest{Version: 2, ID: "overview", Title: "Overview", MediaType: "text/markdown", Entrypoint: "content.md", Order: 1})
	writeFile(t, filepath.Join(root, "overview.fragment", "content.md"), "A café explains the change.\n")
	asset := filepath.Join(root, "overview.fragment", "diagram.png")
	writeFile(t, asset, "not-executed-image-bytes")
	writeJSON(t, filepath.Join(root, "overview.fragment", "___diffs", "coverage.json"), saga.DiffFile{Version: 2, Diffs: []saga.DiffReference{{URI: current.URI, Note: "fragment ownership"}, {URI: stale, Note: "needs repair"}}})
	writeJSON(t, filepath.Join(root, "overview.fragment", "___approvals", "review.json"), saga.Review{Version: 2, ID: "review-1", State: "approved", Body: "Looks good.", CreatedAt: mustTime("2026-08-20T10:02:00Z")})
	writeJSON(t, filepath.Join(root, "details.chapter", "chapter.json"), saga.ChapterManifest{Version: 2, ID: "details", Title: "Details", Order: 2})
	writeJSON(t, filepath.Join(root, "details.chapter", "details.fragment", "fragment.json"), saga.FragmentManifest{Version: 2, ID: "details-body", Title: "Details body", MediaType: "text/plain", Entrypoint: "content.txt"})
	writeFile(t, filepath.Join(root, "details.chapter", "details.fragment", "content.txt"), "Details.\n")
	threadDir := filepath.Join(root, "___review", "threads", "thread-1.thread")
	writeJSON(t, filepath.Join(threadDir, "thread.json"), saga.ThreadManifest{Version: 2, ID: "thread-1", Target: fragmentTarget, Kind: "comment", Anchor: saga.Anchor{Type: "diff", Diff: &saga.DiffSelector{URI: current.URI}}, CreatedAt: mustTime("2026-08-20T10:00:00Z")})
	messageDir := filepath.Join(threadDir, "messages", "message-1.message")
	writeJSON(t, filepath.Join(messageDir, "message.json"), saga.MessageManifest{Version: 2, ID: "message-1", CreatedAt: mustTime("2026-08-20T10:00:00Z")})
	writeJSON(t, filepath.Join(messageDir, "body.fragment", "fragment.json"), saga.FragmentManifest{Version: 2, ID: "message-body", MediaType: "text/markdown", Entrypoint: "content.md"})
	writeFile(t, filepath.Join(messageDir, "body.fragment", "content.md"), "Please clarify.\n")
	writeJSON(t, filepath.Join(root, "___review", "diffs", "file-review.json"), saga.DiffReview{Version: 2, ID: "file-review-1", URI: fileURI, State: "reviewed", CreatedAt: mustTime("2026-08-20T10:03:00Z")})
	git(t, repo, "add", "review.saga")
	git(t, repo, "commit", "-m", "add saga")

	opened, err := Open(ctx, OpenOptions{SagaRoot: root, SourceDir: repo})
	if err != nil {
		t.Fatal(err)
	}
	return serviceFixture{repo: repo, root: root, fragment: fragmentTarget, atomURI: current.URI, fileURI: fileURI, asset: asset, session: opened, comparison: comparison}
}

func assertCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	if err == nil || ErrorCodeOf(err) != want {
		t.Fatalf("error = %#v, code = %q, want %q", err, ErrorCodeOf(err), want)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}
