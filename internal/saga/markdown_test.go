package saga

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidateAuthoredFragmentReportsEmptyAndGeneratedScaffolds(t *testing.T) {
	entrypoint := filepath.Join(t.TempDir(), "content.md")
	for _, content := range []string{
		"Explain this chapter as an independently reviewable change. Describe its boundary, behavior, and risks.\n",
		"Write this review fragment.\n",
	} {
		if err := os.WriteFile(entrypoint, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		result := Validation{}
		validateAuthoredFragment(entrypoint, "text/markdown", "fragment.json", &result)
		if len(result.Issues) != 1 || result.Issues[0].Severity != "error" {
			t.Fatalf("content %q issues = %#v", content, result.Issues)
		}
	}
	if err := os.WriteFile(entrypoint, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	emptyResult := Validation{}
	validateAuthoredFragment(entrypoint, "text/markdown", "fragment.json", &emptyResult)
	if len(emptyResult.Issues) != 1 || emptyResult.Issues[0].Severity != "warning" {
		t.Fatalf("empty content issues = %#v", emptyResult.Issues)
	}
	if err := os.WriteFile(entrypoint, []byte("# Authored {#authored}\n\nReviewer-facing content.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := Validation{}
	validateAuthoredFragment(entrypoint, "text/markdown", "fragment.json", &result)
	if len(result.Issues) != 0 {
		t.Fatalf("authored content issues = %#v", result.Issues)
	}
}

func TestValidateLandmarkMediaSelectors(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "image.svg")
	if err := os.WriteFile(entrypoint, []byte(`<svg><g id="request-flow"/></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}
	fragment := &Fragment{Directory: dir, Entrypoint: "image.svg", MediaType: "image/svg+xml"}
	valid := Landmark{Version: 2, ID: "request-flow", Label: "Request flow", Selector: LandmarkSelector{Type: "element", ElementID: "request-flow"}}
	result := Validation{}
	validateLandmark(valid, fragment, &result)
	if len(result.Issues) != 0 {
		t.Fatalf("valid landmark issues = %#v", result.Issues)
	}
	invalid := valid
	invalid.ID = "missing"
	invalid.Selector.ElementID = "missing"
	validateLandmark(invalid, fragment, &result)
	if len(result.Issues) != 1 || result.Issues[0].Severity != "error" {
		t.Fatalf("missing landmark issues = %#v", result.Issues)
	}

	textPath := filepath.Join(dir, "content.txt")
	if err := os.WriteFile(textPath, []byte("Before the stable phrase after"), 0o600); err != nil {
		t.Fatal(err)
	}
	textFragment := &Fragment{Directory: dir, Entrypoint: "content.txt", MediaType: "text/plain"}
	textLandmark := Landmark{Version: 2, ID: "stable-phrase", Label: "Stable phrase", Selector: LandmarkSelector{Type: "text", Exact: "stable phrase", Prefix: "the ", Suffix: " after"}}
	textResult := Validation{}
	validateLandmark(textLandmark, textFragment, &textResult)
	if len(textResult.Issues) != 0 {
		t.Fatalf("valid text landmark issues = %#v", textResult.Issues)
	}

	imageFragment := &Fragment{Directory: dir, Entrypoint: "image.png", MediaType: "image/png"}
	regionLandmark := Landmark{Version: 2, ID: "submit-area", Label: "Submit area", Selector: LandmarkSelector{Type: "region", X: .1, Y: .2, Width: .3, Height: .4}}
	regionResult := Validation{}
	validateLandmark(regionLandmark, imageFragment, &regionResult)
	if len(regionResult.Issues) != 0 {
		t.Fatalf("valid region landmark issues = %#v", regionResult.Issues)
	}
}

func TestValidateVisualMappingsReportsMissingLandmarksAndEvidence(t *testing.T) {
	fragment := &Fragment{Path: "map.fragment/fragment.json", MediaType: "image/svg+xml"}
	result := Validation{}
	validateVisualMappings(fragment, &result)
	if len(result.Issues) != 2 || result.Issues[0].Severity != "warning" || result.Issues[1].Severity != "warning" {
		t.Fatalf("visual mapping issues = %#v", result.Issues)
	}
	fragment.Landmarks = []Landmark{{ID: "worker", Description: "The worker executes one queued job.", Diffs: []DiffFile{{Version: 2}}}}
	result = Validation{}
	validateVisualMappings(fragment, &result)
	if len(result.Issues) != 0 {
		t.Fatalf("mapped visual issues = %#v", result.Issues)
	}
}

func TestValidateVisualMappingsWarnsWhenMappedElementCannotAppearOnCanvas(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "diagram.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g id="worker"><rect width="20" height="20"/></g></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := &Fragment{
		Path: "map.fragment/fragment.json", Directory: directory, Entrypoint: "diagram.svg", MediaType: "image/svg+xml",
		Landmarks: []Landmark{{ID: "worker", Path: "map.fragment/___landmarks/worker.landmark/landmark.json", Description: "Worker", Selector: LandmarkSelector{Type: "element", ElementID: "worker"}, Diffs: []DiffFile{{Version: 2}}}},
	}
	result := Validation{}
	validateVisualMappings(fragment, &result)
	if len(result.Issues) != 1 || !strings.Contains(result.Issues[0].Message, "no usable viewBox") {
		t.Fatalf("missing on-canvas warning = %#v", result.Issues)
	}
	if err := os.WriteFile(filepath.Join(directory, "diagram.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g id="worker"><rect width="20" height="20"/></g></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	result = Validation{}
	validateVisualMappings(fragment, &result)
	if len(result.Issues) != 0 {
		t.Fatalf("measurable mapped SVG issues = %#v", result.Issues)
	}
}

func TestValidateNarrativeMappingsWarnsAboutCitationFreeFragmentCoverage(t *testing.T) {
	fragment := &Fragment{
		Path: "overview.fragment/fragment.json", MediaType: "text/markdown",
		Diffs: []DiffFile{{Version: 2}},
	}
	result := Validation{}
	validateNarrativeMappings(fragment, &result)
	if len(result.Issues) != 1 || !strings.Contains(result.Issues[0].Message, "evidence-bearing footnotes") {
		t.Fatalf("citation-free narrative issues = %#v", result.Issues)
	}
	fragment.Landmarks = []Landmark{{Selector: LandmarkSelector{Type: "text", Exact: "Focused implementation statement."}, Diffs: []DiffFile{{Version: 2}}}}
	result = Validation{}
	validateNarrativeMappings(fragment, &result)
	if len(result.Issues) != 0 {
		t.Fatalf("focused citation narrative issues = %#v", result.Issues)
	}
}

func TestValidateNarrativeMappingsRequiresDiffEvidenceForEveryFootnote(t *testing.T) {
	directory := t.TempDir()
	entrypoint := filepath.Join(directory, "content.md")
	if err := os.WriteFile(entrypoint, []byte("The lease renews early.[^lease]\n\n[^lease]: Renewal starts before the lease midpoint.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := &Fragment{
		Path: "lease.fragment", Directory: directory, Entrypoint: "content.md", MediaType: "text/markdown",
	}
	result := Validation{}
	validateNarrativeMappings(fragment, &result)
	if len(result.Issues) != 1 || !strings.Contains(result.Issues[0].Message, "footnote [^lease]") || !strings.Contains(result.Issues[0].Message, "exact-text landmark") {
		t.Fatalf("missing citation landmark issues = %#v", result.Issues)
	}

	fragment.Landmarks = []Landmark{{
		Path:     "lease.fragment/___landmarks/lease.landmark/landmark.json",
		Selector: LandmarkSelector{Type: "text", Exact: "Renewal starts before the lease midpoint."},
	}}
	result = Validation{}
	validateNarrativeMappings(fragment, &result)
	if len(result.Issues) != 1 || !strings.Contains(result.Issues[0].Message, "no linked code") || !strings.Contains(result.Issues[0].Message, "diagram node") {
		t.Fatalf("evidence-free citation issues = %#v", result.Issues)
	}

	fragment.Landmarks[0].Diffs = []DiffFile{{Version: CurrentVersion}}
	result = Validation{}
	validateNarrativeMappings(fragment, &result)
	if len(result.Issues) != 0 {
		t.Fatalf("linked prose citation issues = %#v", result.Issues)
	}
}

func TestMarkdownFootnotesIgnoreFencedExamples(t *testing.T) {
	source := "A claim.[^real]\n\n[^real]: Exact implementation evidence.\n\n```markdown\n[^example]: Not authored content.\n```\n"
	footnotes := MarkdownFootnotes(source)
	if len(footnotes) != 1 || footnotes[0].ID != "real" || footnotes[0].Definition != "Exact implementation evidence." {
		t.Fatalf("footnotes = %#v", footnotes)
	}
}
