package saga

import (
	"strconv"
	"strings"
)

// AddedAnchor records one heading that gained a stable anchor.
type AddedAnchor struct {
	Line    int    `json:"line"`
	Heading string `json:"heading"`
	Anchor  string `json:"anchor"`
}

// FixMarkdownHeadingAnchors appends " {#anchor}" to every heading that does not
// already declare one and returns the rewritten content. It is deliberately the
// narrowest possible edit: headings that already carry an anchor keep it,
// fenced code is skipped, and every other byte — including indentation, blank
// lines, and CRLF endings — is preserved. Nothing is renamed, so an anchor that
// a landmark or link already points at cannot move.
//
// reserved names identifiers the generated anchors must avoid, which is how a
// non-heading landmark keeps its id from being claimed by a heading that would
// then conflict with it.
func FixMarkdownHeadingAnchors(content []byte, reserved map[string]bool) ([]byte, []AddedAnchor) {
	source := string(content)
	taken := map[string]bool{}
	for id := range reserved {
		taken[id] = true
	}
	// Anchors already written anywhere in the file are claimed before any are
	// generated, so a generated anchor can never shadow one further down.
	for _, heading := range MarkdownHeadings(source) {
		if heading.Explicit {
			taken[heading.Anchor] = true
		}
	}

	lines := strings.Split(source, "\n")
	var added []AddedAnchor
	inCode := false
	for index, line := range lines {
		body, ending := line, ""
		if strings.HasSuffix(body, "\r") {
			body, ending = body[:len(body)-1], "\r"
		}
		if strings.HasPrefix(strings.TrimSpace(body), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		heading, ok := ParseMarkdownHeading(body)
		if !ok || heading.Explicit {
			continue
		}
		anchor := uniqueAnchor(markdownAnchorSuggestion(heading.Text), taken)
		taken[anchor] = true
		lines[index] = strings.TrimRight(body, " \t") + " {#" + anchor + "}" + ending
		added = append(added, AddedAnchor{Line: index + 1, Heading: heading.Text, Anchor: anchor})
	}
	if len(added) == 0 {
		return content, nil
	}
	return []byte(strings.Join(lines, "\n")), added
}

// uniqueAnchor keeps the suffix deterministic: the same document always yields
// the same anchors, so re-running --fix is a no-op rather than a churn source.
func uniqueAnchor(base string, taken map[string]bool) string {
	if !ValidMarkdownAnchor(base) {
		base = "section"
	}
	if !taken[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := base + "-" + strconv.Itoa(suffix)
		if len(candidate) > 64 {
			base = "section"
			candidate = base + "-" + strconv.Itoa(suffix)
		}
		if !taken[candidate] {
			return candidate
		}
	}
}
