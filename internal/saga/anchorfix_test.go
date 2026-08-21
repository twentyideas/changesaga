package saga

import (
	"strings"
	"testing"
)

func TestFixMarkdownHeadingAnchorsOnlyAddsMissingAnchors(t *testing.T) {
	source := "# Kept {#already-stable}\n\nProse.\n\n## Needs one\n\n```\n## Not a heading\n```\n\n### Third level\n"
	fixed, added := FixMarkdownHeadingAnchors([]byte(source), nil)

	if len(added) != 2 {
		t.Fatalf("expected two added anchors, got %#v", added)
	}
	if added[0].Anchor != "needs-one" || added[0].Heading != "Needs one" || added[0].Line != 5 {
		t.Fatalf("unexpected first fix: %#v", added[0])
	}
	if added[1].Anchor != "third-level" {
		t.Fatalf("unexpected second fix: %#v", added[1])
	}
	result := string(fixed)
	if !strings.Contains(result, "# Kept {#already-stable}") {
		t.Fatal("an existing anchor must be left exactly as the author wrote it")
	}
	if !strings.Contains(result, "## Needs one {#needs-one}") || !strings.Contains(result, "### Third level {#third-level}") {
		t.Fatalf("headings were not anchored: %s", result)
	}
	// A fenced line that merely looks like a heading is content, not structure.
	if !strings.Contains(result, "```\n## Not a heading\n```") {
		t.Fatalf("fenced code was rewritten: %s", result)
	}
}

func TestFixMarkdownHeadingAnchorsIsIdempotent(t *testing.T) {
	source := []byte("# One\n\n## Two\n")
	once, added := FixMarkdownHeadingAnchors(source, nil)
	if len(added) != 2 {
		t.Fatalf("expected two fixes, got %#v", added)
	}
	twice, again := FixMarkdownHeadingAnchors(once, nil)
	if len(again) != 0 {
		t.Fatalf("a second pass must change nothing, got %#v", again)
	}
	if string(twice) != string(once) {
		t.Fatalf("a second pass rewrote content:\n%s\n%s", once, twice)
	}
}

// Two headings with the same words must not produce the same anchor, because a
// duplicate anchor is a validation error rather than a cosmetic problem.
func TestFixMarkdownHeadingAnchorsDeduplicates(t *testing.T) {
	fixed, added := FixMarkdownHeadingAnchors([]byte("## Behavior\n\n## Behavior\n\n## Behavior\n"), nil)
	if len(added) != 3 {
		t.Fatalf("expected three fixes, got %#v", added)
	}
	if added[0].Anchor != "behavior" || added[1].Anchor != "behavior-2" || added[2].Anchor != "behavior-3" {
		t.Fatalf("anchors were not uniquified deterministically: %#v", added)
	}
	validation := &Validation{}
	validateMarkdownAnchorsForTest(t, string(fixed), validation)
}

// An anchor an author already wrote further down the file wins; the generated
// one steps aside so the explicit anchor keeps pointing where it did.
func TestFixMarkdownHeadingAnchorsAvoidsExistingAndReservedIDs(t *testing.T) {
	source := "## Submit action\n\n## Later {#submit-action}\n"
	_, added := FixMarkdownHeadingAnchors([]byte(source), nil)
	if len(added) != 1 || added[0].Anchor != "submit-action-2" {
		t.Fatalf("generated anchor collided with an explicit one: %#v", added)
	}

	_, reservedAdded := FixMarkdownHeadingAnchors([]byte("## Submit action\n"), map[string]bool{"submit-action": true})
	if len(reservedAdded) != 1 || reservedAdded[0].Anchor != "submit-action-2" {
		t.Fatalf("generated anchor claimed a reserved landmark id: %#v", reservedAdded)
	}
}

func TestFixMarkdownHeadingAnchorsPreservesLineEndings(t *testing.T) {
	fixed, added := FixMarkdownHeadingAnchors([]byte("# Title\r\n\r\nBody\r\n"), nil)
	if len(added) != 1 {
		t.Fatalf("expected one fix, got %#v", added)
	}
	if string(fixed) != "# Title {#title}\r\n\r\nBody\r\n" {
		t.Fatalf("CRLF endings were not preserved: %q", fixed)
	}
}

func TestFixMarkdownHeadingAnchorsLeavesAnchoredContentByteIdentical(t *testing.T) {
	source := []byte("# Done {#done}\n\nnothing to do\n")
	fixed, added := FixMarkdownHeadingAnchors(source, nil)
	if len(added) != 0 || string(fixed) != string(source) {
		t.Fatalf("content without missing anchors must be returned untouched: %q %#v", fixed, added)
	}
}

func validateMarkdownAnchorsForTest(t *testing.T, content string, result *Validation) {
	t.Helper()
	seen := map[string]bool{}
	for _, heading := range MarkdownHeadings(content) {
		if !heading.Explicit {
			t.Fatalf("heading %q was left without an anchor", heading.Text)
		}
		if !ValidMarkdownAnchor(heading.Anchor) {
			t.Fatalf("heading %q got invalid anchor %q", heading.Text, heading.Anchor)
		}
		if seen[heading.Anchor] {
			t.Fatalf("anchor %q was generated twice", heading.Anchor)
		}
		seen[heading.Anchor] = true
	}
}
