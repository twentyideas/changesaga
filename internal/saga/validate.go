package saga

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/change-saga/change-saga/internal/diffuri"
)

var stableID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func ValidID(value string) bool { return stableID.MatchString(value) }

func validateManifest(manifest Manifest, result *Validation) {
	if manifest.Version != CurrentVersion {
		addIssue(result, "error", "saga.json", fmt.Sprintf("unsupported version %d; expected %d", manifest.Version, CurrentVersion))
	}
	if !stableID.MatchString(manifest.ID) {
		addIssue(result, "error", "saga.json", "id must be a stable 1-128 character identifier")
	}
	if strings.TrimSpace(manifest.Title) == "" {
		addIssue(result, "error", "saga.json", "title is required")
	}
	if manifest.Schema != "" && manifest.Schema != SchemaURL {
		addIssue(result, "warning", "saga.json", fmt.Sprintf("$schema is %q; the current schema is %q", manifest.Schema, SchemaURL))
	}
	if manifest.PR != nil && manifest.PR.Number < 0 {
		addIssue(result, "error", "saga.json", "pr.number cannot be negative")
	}
	repository, err := url.Parse(manifest.Source.Repository)
	if err != nil || !repository.IsAbs() || repository.Host == "" && repository.Scheme != "file" {
		addIssue(result, "error", "saga.json", "source.repository must be an absolute URI")
	}
	if strings.TrimSpace(manifest.Source.Base) == "" || strings.TrimSpace(manifest.Source.Head) == "" {
		addIssue(result, "error", "saga.json", "source.base and source.head are required")
	}
}

func validateSectionManifest(value SectionManifest, path string, result *Validation) {
	if value.Version != CurrentVersion {
		addIssue(result, "error", path, "section version must be 2")
	}
	if !stableID.MatchString(value.ID) {
		addIssue(result, "error", path, "section id must be stable and non-empty")
	}
	if strings.TrimSpace(value.Title) == "" {
		addIssue(result, "error", path, "section title is required")
	}
}

func validateChapterManifest(value ChapterManifest, path string, result *Validation) {
	if value.Version != CurrentVersion {
		addIssue(result, "error", path, "chapter version must be 2")
	}
	if !stableID.MatchString(value.ID) {
		addIssue(result, "error", path, "chapter id must be stable and non-empty")
	}
	if strings.TrimSpace(value.Title) == "" {
		addIssue(result, "error", path, "chapter title is required")
	}
}

func validateFragmentManifest(value FragmentManifest, path, dir string, result *Validation) {
	if value.Version != CurrentVersion {
		addIssue(result, "error", path, "fragment version must be 2")
	}
	if !stableID.MatchString(value.ID) {
		addIssue(result, "error", path, "fragment id must be stable and non-empty")
	}
	if !validMediaType(value.MediaType) {
		addIssue(result, "error", path, "unsupported media_type")
	}
	if value.Entrypoint == "" || filepath.IsAbs(value.Entrypoint) || filepath.Clean(value.Entrypoint) != value.Entrypoint || value.Entrypoint == ".." || strings.HasPrefix(value.Entrypoint, ".."+string(filepath.Separator)) {
		addIssue(result, "error", path, "entrypoint must be a normalized fragment-relative path")
		return
	}
	entry := filepath.Join(dir, value.Entrypoint)
	info, err := os.Stat(entry)
	if err != nil || info.IsDir() {
		addIssue(result, "error", path, "entrypoint must name an existing file")
		return
	}
	realDir, dirErr := filepath.EvalSymlinks(dir)
	realEntry, entryErr := filepath.EvalSymlinks(entry)
	if dirErr != nil || entryErr != nil {
		addIssue(result, "error", path, "entrypoint cannot be resolved safely")
		return
	}
	rel, relErr := filepath.Rel(realDir, realEntry)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		addIssue(result, "error", path, "entrypoint cannot escape its fragment package")
		return
	}
	if value.MediaType == "text/markdown" {
		validateMarkdownHeadingAnchors(realEntry, path, result)
	}
}

func validateMarkdownHeadingAnchors(entrypoint, path string, result *Validation) {
	content, err := os.ReadFile(entrypoint)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, heading := range MarkdownHeadings(string(content)) {
		if !heading.Explicit {
			addIssue(result, "warning", path, fmt.Sprintf("Markdown heading %q should declare a stable anchor such as {#%s}", heading.Text, markdownAnchorSuggestion(heading.Text)))
			continue
		}
		if !ValidMarkdownAnchor(heading.Anchor) {
			addIssue(result, "error", path, fmt.Sprintf("Markdown heading %q has invalid anchor %q; use lowercase letters, digits, and hyphens", heading.Text, heading.Anchor))
			continue
		}
		if seen[heading.Anchor] {
			addIssue(result, "error", path, fmt.Sprintf("Markdown anchor %q is duplicated within the fragment", heading.Anchor))
		}
		seen[heading.Anchor] = true
	}
}

func markdownAnchorSuggestion(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastHyphen := false
	for _, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if valid {
			result.WriteRune(character)
			lastHyphen = false
		} else if result.Len() > 0 && !lastHyphen {
			result.WriteByte('-')
			lastHyphen = true
		}
		if result.Len() >= 64 {
			break
		}
	}
	suggestion := strings.Trim(result.String(), "-")
	if suggestion == "" || suggestion[0] < 'a' || suggestion[0] > 'z' {
		return "section"
	}
	return suggestion
}

func validMediaType(value string) bool {
	return value == "text/markdown" || value == "text/html" || value == "text/plain" || value == "image/svg+xml" || strings.HasPrefix(value, "image/")
}

func validateLandmark(value Landmark, fragment *Fragment, result *Validation) {
	if value.Version != CurrentVersion {
		addIssue(result, "error", value.Path, "landmark version must be 2")
	}
	if !ValidMarkdownAnchor(value.ID) {
		addIssue(result, "error", value.Path, "landmark id must begin with a lowercase letter and contain only lowercase letters, digits, and hyphens")
	}
	if strings.TrimSpace(value.Label) == "" {
		addIssue(result, "error", value.Path, "landmark label is required")
	}
	selector := value.Selector
	switch selector.Type {
	case "heading":
		if fragment.MediaType != "text/markdown" {
			addIssue(result, "error", value.Path, "heading landmarks require a Markdown fragment")
		}
		if !ValidMarkdownAnchor(selector.HeadingID) {
			addIssue(result, "error", value.Path, "heading landmark requires a stable heading_id")
		} else if selector.HeadingID != value.ID {
			addIssue(result, "error", value.Path, "heading_id must match the landmark id")
		} else if !fragmentHasExplicitHeading(fragment, selector.HeadingID) {
			addIssue(result, "error", value.Path, fmt.Sprintf("heading_id %q does not appear as an explicit Markdown heading anchor", selector.HeadingID))
		}
		if selector.ElementID != "" || selector.Exact != "" || selector.Prefix != "" || selector.Suffix != "" || selector.Width != 0 || selector.Height != 0 || selector.X != 0 || selector.Y != 0 {
			addIssue(result, "error", value.Path, "heading landmark cannot contain element, text, or region fields")
		}
	case "element":
		if fragment.MediaType != "text/html" && fragment.MediaType != "image/svg+xml" {
			addIssue(result, "error", value.Path, "element landmarks require an HTML or SVG fragment")
		}
		if !ValidMarkdownAnchor(selector.ElementID) {
			addIssue(result, "error", value.Path, "element landmark requires a stable element_id")
		} else if content, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil && !containsElementID(string(content), selector.ElementID) {
			addIssue(result, "error", value.Path, fmt.Sprintf("element_id %q does not appear in the fragment entrypoint", selector.ElementID))
		}
		if selector.HeadingID != "" || selector.Exact != "" || selector.Prefix != "" || selector.Suffix != "" || selector.Width != 0 || selector.Height != 0 || selector.X != 0 || selector.Y != 0 {
			addIssue(result, "error", value.Path, "element landmark cannot contain text or region fields")
		}
	case "text":
		if fragment.MediaType != "text/markdown" && fragment.MediaType != "text/plain" {
			addIssue(result, "error", value.Path, "text landmarks require a Markdown or plain-text fragment")
		}
		if selector.Exact == "" {
			addIssue(result, "error", value.Path, "text landmark requires an exact quote")
		} else if content, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil && !textSelectorMatches(string(content), selector.Exact, selector.Prefix, selector.Suffix) {
			addIssue(result, "error", value.Path, "text landmark exact quote does not appear in the fragment entrypoint")
		}
		if selector.ElementID != "" || selector.HeadingID != "" || selector.Width != 0 || selector.Height != 0 || selector.X != 0 || selector.Y != 0 {
			addIssue(result, "error", value.Path, "text landmark cannot contain element or region fields")
		}
	case "region":
		if !strings.HasPrefix(fragment.MediaType, "image/") {
			addIssue(result, "error", value.Path, "region landmarks require an image fragment")
		}
		if !validNormalizedRegion(selector.X, selector.Y, selector.Width, selector.Height) {
			addIssue(result, "error", value.Path, "region landmark coordinates must define a positive normalized rectangle within the image")
		}
		if selector.ElementID != "" || selector.HeadingID != "" || selector.Exact != "" || selector.Prefix != "" || selector.Suffix != "" {
			addIssue(result, "error", value.Path, "region landmark cannot contain element or text fields")
		}
	default:
		addIssue(result, "error", value.Path, "landmark selector type must be heading, element, text, or region")
	}
	if value.Hotspot != nil {
		if fragment.MediaType != "image/svg+xml" && !strings.HasPrefix(fragment.MediaType, "image/") {
			addIssue(result, "error", value.Path, "landmark hotspots require an image or SVG fragment")
		}
		if !validNormalizedRegion(value.Hotspot.X, value.Hotspot.Y, value.Hotspot.Width, value.Hotspot.Height) {
			addIssue(result, "error", value.Path, "landmark hotspot must define a positive normalized rectangle within the fragment")
		}
	}
}

func validNormalizedRegion(x, y, width, height float64) bool {
	return x >= 0 && y >= 0 && width > 0 && height > 0 && x+width <= 1 && y+height <= 1
}

func fragmentHasExplicitHeading(fragment *Fragment, id string) bool {
	content, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint))
	if err != nil {
		return false
	}
	for _, heading := range MarkdownHeadings(string(content)) {
		if heading.Explicit && heading.Anchor == id {
			return true
		}
	}
	return false
}

func containsElementID(content, id string) bool {
	return strings.Contains(content, `id="`+id+`"`) || strings.Contains(content, `id='`+id+`'`)
}

func textSelectorMatches(content, exact, prefix, suffix string) bool {
	for offset := 0; ; {
		index := strings.Index(content[offset:], exact)
		if index < 0 {
			return false
		}
		index += offset
		before, after := content[:index], content[index+len(exact):]
		if (prefix == "" || strings.HasSuffix(before, prefix)) && (suffix == "" || strings.HasPrefix(after, suffix)) {
			return true
		}
		offset = index + len(exact)
	}
}

func validateThread(thread Thread, sagaID, path string, result *Validation) {
	if thread.Version != CurrentVersion || !stableID.MatchString(thread.ID) || thread.CreatedAt.IsZero() {
		addIssue(result, "error", path, "thread requires version 2, id, and created_at")
	}
	prefix := "urn:change-saga:" + sagaID + ":"
	if !strings.HasPrefix(thread.Target, prefix) {
		addIssue(result, "error", path, "thread target must be a URN in this saga")
	}
	if err := ValidateAnchor(thread.Anchor); err != nil {
		addIssue(result, "error", path, err.Error())
	}
	if thread.Kind != "" && thread.Kind != "comment" && thread.Kind != "suggestion" {
		addIssue(result, "error", path, "thread kind must be comment or suggestion")
	}
	if thread.Kind == "suggestion" && (thread.Suggestion == nil || strings.TrimSpace(thread.Suggestion.Replacement) == "") {
		addIssue(result, "error", path, "suggestion thread requires replacement content")
	}
	if thread.Kind != "suggestion" && thread.Suggestion != nil {
		addIssue(result, "error", path, "only suggestion threads may contain a suggestion")
	}
}

// MaxNoteRunes bounds sticky note text so a single annotation stays a compact
// canvas note rather than an unbounded document; schema/v2 enforces the same
// limit as maxLength.
const MaxNoteRunes = 2000

func ValidAnnotationColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if character < '0' || character > '9' {
			lower := character | 0x20
			if lower < 'a' || lower > 'f' {
				return false
			}
		}
	}
	return true
}

func ValidateAnchor(anchor Anchor) error {
	switch anchor.Type {
	case "target":
		if len(anchor.Shapes) != 0 || anchor.Text != nil || anchor.Note != nil || anchor.Diff != nil {
			return fmt.Errorf("target anchor cannot contain shapes, text, note, or diff data")
		}
	case "region", "drawing":
		if anchor.Coordinate != "normalized" || len(anchor.Shapes) == 0 || anchor.Text != nil || anchor.Note != nil || anchor.Diff != nil {
			return fmt.Errorf("region/drawing anchor requires normalized shapes")
		}
		for _, shape := range anchor.Shapes {
			if shape.Type != "rect" && shape.Type != "ellipse" && shape.Type != "line" && shape.Type != "path" {
				return fmt.Errorf("unsupported annotation shape %q", shape.Type)
			}
			if shape.X < 0 || shape.X > 1 || shape.Y < 0 || shape.Y > 1 || shape.Width < 0 || shape.Width > 1 || shape.Height < 0 || shape.Height > 1 {
				return fmt.Errorf("shape coordinates must be normalized between 0 and 1")
			}
			for _, point := range shape.Points {
				if point.X < 0 || point.X > 1 || point.Y < 0 || point.Y > 1 {
					return fmt.Errorf("shape points must be normalized between 0 and 1")
				}
			}
		}
	case "text":
		if anchor.Text == nil || anchor.Text.Exact == "" || len(anchor.Shapes) != 0 || anchor.Note != nil || anchor.Diff != nil || anchor.Text.End < anchor.Text.Start {
			return fmt.Errorf("text anchor requires an exact quote and valid positions")
		}
	case "note":
		if anchor.Note == nil || len(anchor.Shapes) != 0 || anchor.Text != nil || anchor.Diff != nil {
			return fmt.Errorf("note anchor requires exactly one sticky note")
		}
		if anchor.Coordinate != "normalized" {
			return fmt.Errorf("note anchor requires normalized placement")
		}
		if strings.TrimSpace(anchor.Note.Text) == "" || len([]rune(anchor.Note.Text)) > MaxNoteRunes {
			return fmt.Errorf("note text must contain 1 to %d characters", MaxNoteRunes)
		}
		if anchor.Note.X < 0 || anchor.Note.X > 1 || anchor.Note.Y < 0 || anchor.Note.Y > 1 {
			return fmt.Errorf("note placement must be normalized between 0 and 1")
		}
		if anchor.Note.Color != "" && !ValidAnnotationColor(anchor.Note.Color) {
			return fmt.Errorf("note color must be a #rrggbb value")
		}
	case "diff":
		if anchor.Diff == nil || len(anchor.Shapes) != 0 || anchor.Text != nil || anchor.Note != nil {
			return fmt.Errorf("diff anchor requires exactly one diff URI")
		}
		reference, err := diffuri.Parse(anchor.Diff.URI)
		if err != nil || reference.Kind == "file" {
			return fmt.Errorf("diff anchor requires a valid line or event diff URI")
		}
	default:
		return fmt.Errorf("anchor type must be target, region, drawing, text, note, or diff")
	}
	return nil
}

func validateDocument(document *Saga, result *Validation) {
	ids := map[string]string{}
	targets := map[string]bool{SagaTarget(document.Manifest.ID): true}
	var visitFragment func(*Fragment)
	visitFragment = func(fragment *Fragment) {
		if previous, exists := ids[fragment.ID]; exists {
			addIssue(result, "error", fragment.Path, fmt.Sprintf("duplicate id %q, first used by %s", fragment.ID, previous))
		} else {
			ids[fragment.ID] = fragment.Path
		}
		targets[fragment.Target] = true
		for index := range fragment.Landmarks {
			landmark := &fragment.Landmarks[index]
			targets[landmark.Target] = true
		}
	}
	var walkSection func(*Section)
	walkSection = func(section *Section) {
		if section.Path != "" {
			if previous, exists := ids[section.ID]; exists {
				addIssue(result, "error", section.Path, fmt.Sprintf("duplicate id %q, first used by %s", section.ID, previous))
			} else {
				ids[section.ID] = section.Path
			}
			targets[section.Target] = true
		}
		for _, fragment := range section.Fragments {
			visitFragment(fragment)
		}
		for _, child := range section.Children {
			walkSection(child)
		}
	}
	walkSection(document.Section)
	for _, thread := range document.Threads {
		for _, message := range thread.Messages {
			for _, fragment := range message.Fragments {
				visitFragment(fragment)
			}
		}
	}
	for _, thread := range document.Threads {
		if !targets[thread.Target] {
			addIssue(result, "error", relativePath(document.Root, filepath.Join(thread.Directory, "thread.json")), "thread target does not exist")
		}
	}
}

func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}
