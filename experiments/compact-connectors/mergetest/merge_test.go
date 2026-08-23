// Package mergetest exercises the claim that motivates the whole encoding:
// that two reviewers documenting the same change concurrently can merge their
// work with Git, and that when they genuinely disagree the conflict is small
// and legible instead of an opaque wall of URIs.
//
// Every case runs a real `git merge` in a real repository. A hand-rolled
// three-way merge would only prove that this package agrees with itself.
package mergetest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
)

const (
	repository = "https://example.test/acme/app.git"
	base       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	head       = "product-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var comparison = connector.Comparison{Repository: repository, Base: base, Head: head}

func git(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.test",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var out bytes.Buffer
	command.Stdout = &out
	command.Stderr = &out
	err := command.Run()
	return out.String(), err
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := git(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func newRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "--initial-branch=main", ".")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("saga\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "start")
	return dir
}

func lines(side string, start, end int, note string) connector.Record {
	return connector.Record{Comparison: comparison, Note: note, Kind: "lines", Side: side, Start: start, End: end}
}

func writeShard(t *testing.T, repo, relative string, file connector.File) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := connector.Write(handle, file); err != nil {
		t.Fatal(err)
	}
}

// branchWork commits one reviewer's contribution on its own branch, starting
// from main each time, which is exactly the shape of two people working in
// parallel.
func branchWork(t *testing.T, repo, branch string, write func()) {
	t.Helper()
	mustGit(t, repo, "checkout", "-q", "main")
	mustGit(t, repo, "checkout", "-q", "-b", branch)
	write()
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", branch)
}

const shardDir = "api.fragment/___diffs"

// Two reviewers documenting two different source files write two different
// shards. This is the common case and it must not even reach the merge
// algorithm.
func TestConcurrentCoverageOfDifferentSourceFilesMergesCleanly(t *testing.T) {
	repo := newRepository(t)
	owner := "urn:change-saga:demo:fragment:api"

	branchWork(t, repo, "alice", func() {
		writeShard(t, repo, shardDir+"/handler-aaaa1111.connectors", connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 1, 40, "Handler rewrite")},
		})
	})
	branchWork(t, repo, "bob", func() {
		writeShard(t, repo, shardDir+"/store-bbbb2222.connectors", connector.File{
			Owner: owner, Source: "internal/store.go",
			Records: []connector.Record{lines("new", 1, 25, "Storage rewrite")},
		})
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	if output, err := git(t, repo, "merge", "--no-edit", "bob"); err != nil {
		t.Fatalf("merging two different source files conflicted:\n%s", output)
	}
	for _, name := range []string{"handler-aaaa1111.connectors", "store-bbbb2222.connectors"} {
		if _, err := os.Stat(filepath.Join(repo, shardDir, name)); err != nil {
			t.Fatalf("%s did not survive the merge: %v", name, err)
		}
	}
}

// Two reviewers documenting different regions of the same source file write to
// the same shard. Sorted, one-record-per-line bodies are what let Git merge
// this instead of declaring a conflict over a rewritten array.
func TestConcurrentCoverageOfDifferentRegionsOfOneFileMergesCleanly(t *testing.T) {
	repo := newRepository(t)
	owner := "urn:change-saga:demo:fragment:api"
	shard := shardDir + "/handler-aaaa1111.connectors"

	// A shard that already documents several regions is the realistic state of
	// a file under review. The existing records are what give Git distinct
	// anchors to attach each reviewer's insertion to.
	existing := []connector.Record{
		lines("new", 1, 20, "Existing"),
		lines("new", 60, 80, "Existing"),
		lines("new", 200, 240, "Existing"),
		lines("new", 500, 540, "Existing"),
	}
	mustGit(t, repo, "checkout", "-q", "main")
	writeShard(t, repo, shard, connector.File{Owner: owner, Source: "internal/handler.go", Records: existing})
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", "existing coverage")

	branchWork(t, repo, "alice", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: append(append([]connector.Record(nil), existing...), lines("new", 300, 340, "Alice documents the middle")),
		})
	})
	branchWork(t, repo, "bob", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: append(append([]connector.Record(nil), existing...), lines("new", 30, 40, "Bob documents the head")),
		})
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	if output, err := git(t, repo, "merge", "--no-edit", "bob"); err != nil {
		t.Fatalf("two reviewers editing different regions of one file conflicted:\n%s\n%s",
			output, read(t, filepath.Join(repo, shard)))
	}

	merged := parse(t, filepath.Join(repo, shard))
	found := map[string]bool{}
	for _, record := range merged.Records {
		found[record.Note] = true
	}
	for _, note := range []string{"Existing", "Alice documents the middle", "Bob documents the head"} {
		if !found[note] {
			t.Fatalf("the merge lost %q; kept %v", note, found)
		}
	}
	if len(merged.Records) != len(existing)+2 {
		t.Fatalf("merged shard has %d records, want %d", len(merged.Records), len(existing)+2)
	}
}

// The honest limit of a line-oriented format: when two additions land at the
// same anchor — which is what happens in a shard too short to separate them —
// Git cannot tell them apart and asks. The encoding's job at that point is to
// make the question small.
func TestAdjacentInsertionsIntoAShortShardConflictLegibly(t *testing.T) {
	repo := newRepository(t)
	owner := "urn:change-saga:demo:fragment:api"
	shard := shardDir + "/handler-aaaa1111.connectors"

	mustGit(t, repo, "checkout", "-q", "main")
	writeShard(t, repo, shard, connector.File{
		Owner: owner, Source: "internal/handler.go",
		Records: []connector.Record{lines("new", 1, 20, "Existing")},
	})
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", "existing coverage")

	branchWork(t, repo, "alice", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 1, 20, "Existing"), lines("new", 400, 460, "Alice")},
		})
	})
	branchWork(t, repo, "bob", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 1, 20, "Existing"), lines("new", 100, 140, "Bob")},
		})
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	if _, err := git(t, repo, "merge", "--no-edit", "bob"); err == nil {
		t.Skip("Git resolved adjacent insertions on this version; the encoding is no worse than expected")
	}
	assertLegibleConflict(t, read(t, filepath.Join(repo, shard)), "lines new 400-460", "lines new 100-140")
}

// When two reviewers really do claim the same lines for different reasons,
// there is nothing to merge and Git should say so. What matters is that the
// conflict a human opens is a handful of readable lines.
func TestGenuinelyConflictingClaimsProduceASmallReadableConflict(t *testing.T) {
	repo := newRepository(t)
	owner := "urn:change-saga:demo:fragment:api"
	shard := shardDir + "/handler-aaaa1111.connectors"

	branchWork(t, repo, "alice", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 10, 30, "Alice's reading")},
		})
	})
	branchWork(t, repo, "bob", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 10, 30, "Bob's reading")},
		})
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	output, err := git(t, repo, "merge", "--no-edit", "bob")
	if err == nil {
		t.Fatalf("two different explanations of the same lines merged silently:\n%s", output)
	}

	conflicted := read(t, filepath.Join(repo, shard))
	// Both explanations are hoisted into note blocks, so the conflict region
	// carries the record lines; the whole file is what a reviewer reads, and it
	// has to stay short enough to read.
	assertLegibleConflict(t, conflicted, "9", "9")
	if !strings.Contains(conflicted, "Alice's reading") || !strings.Contains(conflicted, "Bob's reading") {
		t.Fatalf("the conflicted shard does not show both explanations in plain text:\n%s", conflicted)
	}
}

// assertLegibleConflict is the standard this encoding is held to when Git does
// have to ask: the conflicted region is a few lines of readable text, not a
// wall of URIs.
func assertLegibleConflict(t *testing.T, conflicted string, mustContain ...string) {
	t.Helper()
	open := strings.Index(conflicted, "<<<<<<<")
	close := strings.Index(conflicted, ">>>>>>>")
	if open < 0 || close < 0 {
		t.Fatalf("expected conflict markers:\n%s", conflicted)
	}
	region := conflicted[open:close]
	for _, needle := range mustContain {
		if !strings.Contains(region, needle) {
			t.Fatalf("the conflict region does not mention %q:\n%s", needle, region)
		}
	}
	if count := strings.Count(region, "\n"); count > 12 {
		t.Fatalf("the conflict region is %d lines; a reviewer should read it at a glance:\n%s", count, region)
	}
	if len(region) > 1024 {
		t.Fatalf("the conflict region is %d bytes; want a legible one:\n%s", len(region), region)
	}
}

// The v2 encoding never conflicts, because every `cover` run writes a new
// timestamped file. That is not merge friendliness: the two records survive
// side by side, the same atoms acquire two owners, and coverage reports an
// overlap nobody authored. The connector encoding converges on one shard
// instead, which is the difference this experiment is about.
func TestV2EvidenceMergesWithoutConflictButManufacturesAnOverlap(t *testing.T) {
	repo := newRepository(t)
	dir := "api.fragment/___diffs"

	uri := "saga-diff://v1/line?base=" + base + "&head=" + head +
		"&path=internal%2Fhandler.go&repository=https%3A%2F%2Fexample.test%2Facme%2Fapp.git&side=new&start=10&end=30"

	branchWork(t, repo, "alice", func() {
		writeLegacy(t, repo, dir+"/handler-20260101t000000-000000000z-aaaa1111.json", uri, "Alice's reading")
	})
	branchWork(t, repo, "bob", func() {
		writeLegacy(t, repo, dir+"/handler-20260101t000001-000000000z-bbbb2222.json", uri, "Bob's reading")
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	if output, err := git(t, repo, "merge", "--no-edit", "bob"); err != nil {
		t.Fatalf("v2 evidence unexpectedly conflicted:\n%s", output)
	}

	entries, err := os.ReadDir(filepath.Join(repo, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected both v2 records to survive the merge, found %d", len(entries))
	}
	// Both records select the same atom, so a coverage evaluation of this
	// merged tree reports every line 10-30 as overlapping. That is the failure
	// mode the connector encoding replaces with a conflict a human resolves.
}

func writeLegacy(t *testing.T, repo, relative, uri, note string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "version": 2,
  "diffs": [
    {
      "uri": "` + uri + `",
      "note": "` + note + `"
    }
  ]
}
`
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parse(t *testing.T, path string) connector.File {
	t.Helper()
	handle, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	file, err := connector.Parse(handle)
	if err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, read(t, path))
	}
	return file
}

// A union merge driver is the obvious escape hatch for a sorted, one-record-per
// -line format, and it does remove every conflict. It is recorded here as a
// measured option rather than a recommendation: union keeps both claims, which
// turns a disagreement two people would have talked about into an overlap the
// coverage report merely mentions.
func TestUnionMergeDriverRemovesConflictsButKeepsBothClaims(t *testing.T) {
	repo := newRepository(t)
	owner := "urn:change-saga:demo:fragment:api"
	shard := shardDir + "/handler-aaaa1111.connectors"

	mustGit(t, repo, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.connectors merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeShard(t, repo, shard, connector.File{
		Owner: owner, Source: "internal/handler.go",
		Records: []connector.Record{lines("new", 1, 20, "Existing")},
	})
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", "existing coverage")

	branchWork(t, repo, "alice", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 1, 20, "Existing"), lines("new", 10, 30, "Alice's reading")},
		})
	})
	branchWork(t, repo, "bob", func() {
		writeShard(t, repo, shard, connector.File{
			Owner: owner, Source: "internal/handler.go",
			Records: []connector.Record{lines("new", 1, 20, "Existing"), lines("new", 10, 30, "Bob's reading")},
		})
	})

	mustGit(t, repo, "checkout", "-q", "alice")
	if output, err := git(t, repo, "merge", "--no-edit", "bob"); err != nil {
		t.Fatalf("union merge still conflicted:\n%s\n%s", output, read(t, filepath.Join(repo, shard)))
	}

	// The union result must still be a valid shard: a merge strategy that
	// produces a file the loader rejects would be worse than a conflict.
	merged := parse(t, filepath.Join(repo, shard))
	notes := map[string]bool{}
	for _, record := range merged.Records {
		notes[record.Note] = true
	}
	if !notes["Alice's reading"] || !notes["Bob's reading"] {
		t.Fatalf("union merge lost a claim; kept %v", notes)
	}
	// Both reviewers now own lines 10-30. Coverage reports that as an overlap,
	// which is permitted and visible — but nobody chose it.
	overlapping := 0
	for _, record := range merged.Records {
		if record.Start == 10 && record.End == 30 {
			overlapping++
		}
	}
	if overlapping != 2 {
		t.Fatalf("expected both claims on lines 10-30 to survive, found %d", overlapping)
	}
}
