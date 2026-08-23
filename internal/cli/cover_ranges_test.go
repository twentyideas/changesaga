package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
)

const (
	rangeRepository = "https://example.test/acme/app.git"
	rangeBase       = "1111111111111111111111111111111111111111"
	rangeHead       = "product-2222222222222222222222222222222222222222222222222222222222222222"
	otherHead       = "product-3333333333333333333333333333333333333333333333333333333333333333"
)

// lineAtom builds the same single-line atom gitdiff.Read emits, so the unit
// tests exercise the exact input the derived selector path sees in production.
func lineAtom(t *testing.T, reference diffuri.Reference, line int) gitdiff.Atom {
	t.Helper()
	reference.Kind = "line"
	reference.Start = line
	reference.End = line
	uri, err := diffuri.Build(reference)
	if err != nil {
		t.Fatalf("build line URI: %v", err)
	}
	return gitdiff.Atom{Kind: "line", Path: reference.Path, Side: reference.Side, Line: line, URI: uri}
}

func newLine(t *testing.T, path string, line int) gitdiff.Atom {
	t.Helper()
	return lineAtom(t, diffuri.Reference{Repository: rangeRepository, Base: rangeBase, Head: rangeHead, Path: path, Side: "new"}, line)
}

func oldLine(t *testing.T, path string, line int) gitdiff.Atom {
	t.Helper()
	return lineAtom(t, diffuri.Reference{Repository: rangeRepository, Base: rangeBase, Head: rangeHead, Path: path, Side: "old"}, line)
}

func eventAtom(t *testing.T, event, path string) gitdiff.Atom {
	t.Helper()
	uri, err := diffuri.Build(diffuri.Reference{Repository: rangeRepository, Base: rangeBase, Head: rangeHead, Kind: "event", Event: event, Path: path})
	if err != nil {
		t.Fatalf("build event URI: %v", err)
	}
	return gitdiff.Atom{Kind: "event", Event: event, Path: path, URI: uri}
}

// describeSelectors renders emitted URIs as a compact, order-preserving shape so
// a failure names the range that was wrong rather than dumping query strings.
func describeSelectors(t *testing.T, uris []string) []string {
	t.Helper()
	described := make([]string, 0, len(uris))
	for _, uri := range uris {
		reference, err := diffuri.Parse(uri)
		if err != nil {
			t.Fatalf("parse emitted selector %q: %v", uri, err)
		}
		if reference.Kind == "line" {
			described = append(described, fmt.Sprintf("line %s %s %d-%d", reference.Path, reference.Side, reference.Start, reference.End))
			continue
		}
		described = append(described, fmt.Sprintf("%s %s %s", reference.Kind, reference.Event, reference.Path))
	}
	return described
}

func selectorsFor(t *testing.T, atoms []gitdiff.Atom) []string {
	t.Helper()
	uris, err := changedLineSelectors(atoms)
	if err != nil {
		t.Fatalf("changedLineSelectors: %v", err)
	}
	return describeSelectors(t, uris)
}

// expandSelectors lists every atom identity a selector set matches, which is the
// property coalescing must preserve exactly: the same atoms, no more, no fewer.
func expandSelectors(t *testing.T, uris []string) []string {
	t.Helper()
	var keys []string
	for _, uri := range uris {
		reference, err := diffuri.Parse(uri)
		if err != nil {
			t.Fatalf("parse emitted selector %q: %v", uri, err)
		}
		if reference.Kind != "line" {
			keys = append(keys, fmt.Sprintf("%s|%s|%s", reference.Kind, reference.Event, reference.Path))
			continue
		}
		for line := reference.Start; line <= reference.End; line++ {
			keys = append(keys, fmt.Sprintf("line|%s|%s|%s|%s|%d", reference.Repository, reference.Head, reference.Path, reference.Side, line))
		}
	}
	sort.Strings(keys)
	return keys
}

func TestChangedLineSelectorsCoalesceOnlyDenseRuns(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		atoms []gitdiff.Atom
		want  []string
	}{
		{
			name:  "consecutive new lines become one range",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 1), newLine(t, "a.go", 2), newLine(t, "a.go", 3)},
			want:  []string{"line a.go new 1-3"},
		},
		{
			name:  "a single line keeps a degenerate range",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 7)},
			want:  []string{"line a.go new 7-7"},
		},
		{
			name:  "a one line gap splits the run",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 1), newLine(t, "a.go", 2), newLine(t, "a.go", 4), newLine(t, "a.go", 5)},
			want:  []string{"line a.go new 1-2", "line a.go new 4-5"},
		},
		{
			name:  "nonconsecutive lines never merge",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 1), newLine(t, "a.go", 10), newLine(t, "a.go", 20)},
			want:  []string{"line a.go new 1-1", "line a.go new 10-10", "line a.go new 20-20"},
		},
		{
			name:  "old and new sides stay separate",
			atoms: []gitdiff.Atom{oldLine(t, "a.go", 1), oldLine(t, "a.go", 2), newLine(t, "a.go", 1), newLine(t, "a.go", 2)},
			want:  []string{"line a.go old 1-2", "line a.go new 1-2"},
		},
		{
			name:  "different paths stay separate",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 1), newLine(t, "b.go", 2), newLine(t, "a.go", 2)},
			want:  []string{"line a.go new 1-2", "line b.go new 2-2"},
		},
		{
			name:  "events are never coalesced and keep their place",
			atoms: []gitdiff.Atom{eventAtom(t, "add", "a.go"), newLine(t, "a.go", 1), newLine(t, "a.go", 2)},
			want:  []string{"event add a.go", "line a.go new 1-2"},
		},
		{
			name:  "two events on one path remain two references",
			atoms: []gitdiff.Atom{eventAtom(t, "mode", "a.go"), eventAtom(t, "modify", "a.go")},
			want:  []string{"event mode a.go", "event modify a.go"},
		},
		{
			name:  "an event between line runs does not bridge or split them",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 1), eventAtom(t, "modify", "a.go"), newLine(t, "a.go", 2)},
			want:  []string{"line a.go new 1-2", "event modify a.go"},
		},
		{
			name:  "unsorted input canonicalizes to ascending ranges",
			atoms: []gitdiff.Atom{newLine(t, "a.go", 3), newLine(t, "a.go", 1), newLine(t, "a.go", 2)},
			want:  []string{"line a.go new 1-3"},
		},
		{
			name: "a different head is a different identity",
			atoms: []gitdiff.Atom{
				newLine(t, "a.go", 1),
				lineAtom(t, diffuri.Reference{Repository: rangeRepository, Base: rangeBase, Head: otherHead, Path: "a.go", Side: "new"}, 2),
			},
			want: []string{"line a.go new 1-1", "line a.go new 2-2"},
		},
		{
			name: "a different base is a different identity",
			atoms: []gitdiff.Atom{
				newLine(t, "a.go", 1),
				lineAtom(t, diffuri.Reference{Repository: rangeRepository, Base: "4444444444444444444444444444444444444444", Head: rangeHead, Path: "a.go", Side: "new"}, 2),
			},
			want: []string{"line a.go new 1-1", "line a.go new 2-2"},
		},
		{
			name: "a different repository is a different identity",
			atoms: []gitdiff.Atom{
				newLine(t, "a.go", 1),
				lineAtom(t, diffuri.Reference{Repository: "https://example.test/acme/other.git", Base: rangeBase, Head: rangeHead, Path: "a.go", Side: "new"}, 2),
			},
			want: []string{"line a.go new 1-1", "line a.go new 2-2"},
		},
		{
			name:  "no atoms produce no selectors",
			atoms: nil,
			want:  []string{},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := selectorsFor(t, testCase.atoms)
			if strings.Join(got, "; ") != strings.Join(testCase.want, "; ") {
				t.Fatalf("selectors = %v, want %v", got, testCase.want)
			}
		})
	}
}

// Coalescing is only sound if the emitted ranges address exactly the atoms they
// were built from. This checks that identity directly on a mixed atom set.
func TestChangedLineSelectorsPreserveExactAtomIdentity(t *testing.T) {
	atoms := []gitdiff.Atom{
		eventAtom(t, "modify", "a.go"),
		oldLine(t, "a.go", 4), oldLine(t, "a.go", 5),
		newLine(t, "a.go", 4), newLine(t, "a.go", 5), newLine(t, "a.go", 6),
		newLine(t, "a.go", 40),
		newLine(t, "b.go", 1),
	}
	uris, err := changedLineSelectors(atoms)
	if err != nil {
		t.Fatalf("changedLineSelectors: %v", err)
	}
	if len(uris) != 5 {
		t.Fatalf("expected five canonical selectors, got %v", describeSelectors(t, uris))
	}
	verbatim := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		verbatim = append(verbatim, atom.URI)
	}
	got := strings.Join(expandSelectors(t, uris), "\n")
	want := strings.Join(expandSelectors(t, verbatim), "\n")
	if got != want {
		t.Fatalf("coalesced selectors address different atoms:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseRangesCanonicalizesEquivalentManualSelectors(t *testing.T) {
	ranges, err := parseRanges("9-10, 1, 3-5, 2-3, 8, 10")
	if err != nil {
		t.Fatal(err)
	}
	want := []lineRange{{Start: 1, End: 5}, {Start: 8, End: 10}}
	if fmt.Sprint(ranges) != fmt.Sprint(want) {
		t.Fatalf("canonical ranges = %v, want %v", ranges, want)
	}
}

// changedLinesSaga has one added file, one modified file whose changed lines are
// deliberately nonconsecutive on both sides, and one deleted file, so a single
// comparison exercises gaps, both sides, and several event kinds.
func changedLinesSaga(t *testing.T) (root, repo string) {
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test Author")
	git(t, repo, "config", "user.email", "test@example.test")
	git(t, repo, "remote", "add", "origin", rangeRepository)
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	writeFile(t, filepath.Join(repo, "internal", "service", "handler.go"), "package service\n\nconst A = 1\nconst B = 2\nconst C = 3\nconst D = 4\nconst E = 5\nconst F = 6\nconst G = 7\n")
	writeFile(t, filepath.Join(repo, "internal", "service", "legacy.go"), "package service\n\nconst Legacy = true\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "checkout", "-b", "feature")
	// Two separated edits: lines 3-4 and line 8 change, everything else is
	// untouched context, so the derived selectors must show a gap.
	writeFile(t, filepath.Join(repo, "internal", "service", "handler.go"), "package service\n\nconst A = 10\nconst B = 20\nconst C = 3\nconst D = 4\nconst E = 5\nconst F = 60\nconst G = 7\n")
	writeFile(t, filepath.Join(repo, "internal", "service", "added.go"), "package service\n\nconst New = 1\n")
	if err := os.Remove(filepath.Join(repo, "internal", "service", "legacy.go")); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "feature")

	root = filepath.Join(t.TempDir(), "ranges.saga")
	var output bytes.Buffer
	if err := Init(context.Background(), []string{"--repo", repo, "--repository", rangeRepository, "--base", base, "--head", "HEAD", root}, &output); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "overview.fragment", "content.md"), "# Range change {#range-change}\n\nThe canonical range coverage test change.\n")
	return root, repo
}

func parseSelectors(t *testing.T, references []struct {
	URI  string `json:"uri"`
	Note string `json:"note"`
}) []diffuri.Reference {
	t.Helper()
	parsed := make([]diffuri.Reference, 0, len(references))
	for _, reference := range references {
		value, err := diffuri.Parse(reference.URI)
		if err != nil {
			t.Fatalf("parse persisted selector %q: %v", reference.URI, err)
		}
		parsed = append(parsed, value)
	}
	return parsed
}

func TestCoverChangedLinesEmitsCanonicalRangesWithGaps(t *testing.T) {
	root, repo := changedLinesSaga(t)
	output, err := runCover(t, "", "--repo", repo, "--path", "internal/service/handler.go", "--changed-lines", "--note", "the modified constants", "--name", "handler", "--json", root)
	if err != nil {
		t.Fatalf("changed-lines cover: %v\n%s", err, output)
	}
	var result coverageMutationOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output)
	}
	references := readDiffFile(t, filepath.Join(root, "___diffs", "handler.json"))
	got := describeSelectors(t, urisOf(references))
	// Each identity is emitted once, at the position of its first atom, with its
	// dense runs ascending: the two separated edits stay two ranges per side.
	want := []string{"line internal/service/handler.go old 3-4", "line internal/service/handler.go old 8-8", "line internal/service/handler.go new 3-4", "line internal/service/handler.go new 8-8"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("derived selectors = %v, want %v", got, want)
	}
	if result.Selectors != len(want) {
		t.Fatalf("selector count = %d, want %d", result.Selectors, len(want))
	}
	// Every reference the record produced must carry the record's note; a range
	// stands in for the lines it replaced, not for a different annotation.
	for _, reference := range references {
		if reference.Note != "the modified constants" {
			t.Fatalf("range lost the record note: %#v", reference)
		}
	}
}

// Density is per identity, so ranges must not span the side they belong to.
func TestCoverChangedLinesRespectsSideFilter(t *testing.T) {
	for _, side := range []string{"old", "new"} {
		t.Run(side, func(t *testing.T) {
			root, repo := changedLinesSaga(t)
			if output, err := runCover(t, "", "--repo", repo, "--path", "internal/service/handler.go", "--changed-lines", "--side", side, "--name", "handler", root); err != nil {
				t.Fatalf("changed-lines cover: %v\n%s", err, output)
			}
			references := readDiffFile(t, filepath.Join(root, "___diffs", "handler.json"))
			got := describeSelectors(t, urisOf(references))
			want := []string{
				fmt.Sprintf("line internal/service/handler.go %s 3-4", side),
				fmt.Sprintf("line internal/service/handler.go %s 8-8", side),
			}
			if strings.Join(got, "; ") != strings.Join(want, "; ") {
				t.Fatalf("%s-side selectors = %v, want %v", side, got, want)
			}
		})
	}
}

// Events keep their own references even when the same path also contributes
// dense line runs, and covering every path must still close the saga exactly.
func TestCoverChangedLinesKeepsEventsSeparateAndCoverageExact(t *testing.T) {
	root, repo := changedLinesSaga(t)
	batch := strings.Join([]string{
		`{"path":"internal/service/handler.go","changed_lines":true,"name":"handler","note":"modified constants"}`,
		`{"path":"internal/service/added.go","changed_lines":true,"name":"added","note":"the new file"}`,
		`{"path":"internal/service/legacy.go","changed_lines":true,"name":"legacy","note":"the removed file"}`,
	}, "\n")
	if output, err := runCover(t, batch, "--repo", repo, "--batch", "-", root); err != nil {
		t.Fatalf("batch changed-lines cover: %v\n%s", err, output)
	}

	added := describeSelectors(t, urisOf(readDiffFile(t, filepath.Join(root, "___diffs", "added.json"))))
	if strings.Join(added, "; ") != "event add internal/service/added.go; line internal/service/added.go new 1-3" {
		t.Fatalf("added file selectors = %v", added)
	}
	deleted := describeSelectors(t, urisOf(readDiffFile(t, filepath.Join(root, "___diffs", "legacy.json"))))
	if strings.Join(deleted, "; ") != "event delete internal/service/legacy.go; line internal/service/legacy.go old 1-3" {
		t.Fatalf("deleted file selectors = %v", deleted)
	}

	report, err := buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	// Ranged selectors must own exactly the atoms per-line selectors owned:
	// nothing uncovered, nothing double-owned, nothing orphaned.
	if !report.Complete || report.Summary.Uncovered != 0 || report.Summary.Overlapping != 0 || report.Summary.Orphaned != 0 {
		t.Fatalf("canonical ranges changed coverage: %#v", report.Summary)
	}
	assertValid(t, root)
}

// A range built from consecutive atoms must never reach a line that belongs to
// another target, which is what "dense" buys over "widened".
func TestCoverChangedLinesRangesDoNotStealNeighbouringAtoms(t *testing.T) {
	root, repo := changedLinesSaga(t)
	if output, err := runCover(t, "", "--repo", repo, "--path", "internal/service/handler.go", "--changed-lines", "--name", "handler", root); err != nil {
		t.Fatalf("changed-lines cover: %v\n%s", err, output)
	}
	// Line 5 of the added file is not a changed atom at all; covering the added
	// file's own dense range must leave the handler ranges untouched.
	if output, err := runCover(t, "", "--repo", repo, "--path", "internal/service/added.go", "--changed-lines", "--name", "added", root); err != nil {
		t.Fatalf("added cover: %v\n%s", err, output)
	}
	report, err := buildReport(context.Background(), root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Overlapping != 0 {
		t.Fatalf("dense ranges overlapped: %#v", report.Overlaps)
	}
	// The deleted file was intentionally left uncovered, so its atoms must still
	// be reported: a range in another file cannot absorb them.
	if report.Complete || report.Summary.Uncovered == 0 {
		t.Fatalf("uncovered atoms disappeared behind ranges: %#v", report.Summary)
	}
	for _, atom := range report.Uncovered {
		if atom.Path != "internal/service/legacy.go" {
			t.Fatalf("unexpected uncovered atom: %#v", atom)
		}
	}
}

func urisOf(references []struct {
	URI  string `json:"uri"`
	Note string `json:"note"`
}) []string {
	uris := make([]string, 0, len(references))
	for _, reference := range references {
		uris = append(uris, reference.URI)
	}
	return uris
}
