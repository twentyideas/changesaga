package saga

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownHeadingsRecognizeStableExplicitAnchors(t *testing.T) {
	headings := MarkdownHeadings("# Purpose {#purpose}\n\n```md\n# Not content {#ignored}\n```\n## Risk\n###### Detail {#detail}\n")
	if len(headings) != 3 {
		t.Fatalf("headings = %#v", headings)
	}
	if headings[0].Text != "Purpose" || headings[0].Anchor != "purpose" || !headings[0].Explicit || headings[0].Level != 1 {
		t.Fatalf("explicit heading = %#v", headings[0])
	}
	if headings[1].Text != "Risk" || headings[1].Explicit {
		t.Fatalf("generated heading = %#v", headings[1])
	}
	if headings[2].Level != 6 || headings[2].Anchor != "detail" {
		t.Fatalf("deep heading = %#v", headings[2])
	}
}

func TestValidMarkdownAnchor(t *testing.T) {
	for _, value := range []string{"purpose", "request-flow", "risk-2"} {
		if !ValidMarkdownAnchor(value) {
			t.Fatalf("expected %q to be valid", value)
		}
	}
	for _, value := range []string{"", "Purpose", "2-risk", "risk_space", "risk/flow"} {
		if ValidMarkdownAnchor(value) {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestValidateMarkdownHeadingAnchorsReportsAuthoringProblems(t *testing.T) {
	entrypoint := filepath.Join(t.TempDir(), "content.md")
	if err := os.WriteFile(entrypoint, []byte("# Missing\n# One {#same}\n# Two {#same}\n# Bad {#Not-Stable}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := Validation{}
	validateMarkdownHeadingAnchors(entrypoint, "fragment.json", &result)
	if len(result.Issues) != 3 || result.Issues[0].Severity != "warning" || result.Issues[1].Severity != "error" || result.Issues[2].Severity != "error" {
		t.Fatalf("issues = %#v", result.Issues)
	}
}
