package saga

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

var stableID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var svgViewBoxValidationPattern = regexp.MustCompile(`(?i)\bviewBox\s*=\s*["']([^"']+)["']`)

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
	if manifest.PR != nil {
		if manifest.PR.Number != nil && *manifest.PR.Number < 1 {
			addIssue(result, "error", "saga.json", "pr.number must be a positive pull request number")
		}
		if manifest.PR.URL != "" {
			if parsed, err := url.Parse(manifest.PR.URL); err != nil || !parsed.IsAbs() {
				addIssue(result, "error", "saga.json", "pr.url must be an absolute URI")
			}
		}
	}
	validateRepositoryIdentity(manifest.Source.Repository, "saga.json", result)
	if strings.TrimSpace(manifest.Source.Base) == "" || strings.TrimSpace(manifest.Source.Head) == "" {
		addIssue(result, "error", "saga.json", "source.base and source.head are required")
	}
}

// validateRepositoryIdentity enforces the portable identity rule shared by
// SPEC.md and schema/v2/saga.schema.json: an absolute URI that never carries
// URL userinfo credentials. Noncanonical-but-resolvable spellings stay loadable
// so existing v2 sagas keep working, but they are reported so authors can fix
// the identity before it is published.
func validateRepositoryIdentity(value, path string, result *Validation) {
	repository, err := url.Parse(value)
	if err != nil || !repository.IsAbs() || repository.Host == "" && repository.Scheme != "file" {
		addIssue(result, "error", path, "source.repository must be an absolute URI")
		return
	}
	if repository.User != nil {
		// Every builder in this codebase strips userinfo, so a manifest that
		// keeps it would disagree with its own diff URIs about the repository
		// identity even before the credential-leak problem.
		stripped := *repository
		stripped.User = nil
		addIssue(result, "error", path, fmt.Sprintf("source.repository must not contain URL userinfo; use %q", stripped.String()))
		return
	}
	canonical, err := diffuri.CanonicalRepository(value)
	if err != nil {
		addIssue(result, "error", path, fmt.Sprintf("source.repository is not a usable repository identity: %v", err))
		return
	}
	if canonical != value {
		addIssue(result, "warning", path, fmt.Sprintf("source.repository %q is not canonical; the canonical identity is %q", value, canonical))
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
	validateFragmentManifestMode(value, path, dir, true, result)
}

func validateFragmentOutlineManifest(value FragmentManifest, path, dir string, result *Validation) {
	validateFragmentManifestMode(value, path, dir, false, result)
}

func validateFragmentManifestMode(value FragmentManifest, path, dir string, validateContent bool, result *Validation) {
	if value.Version != CurrentVersion {
		addIssue(result, "error", path, "fragment version must be 2")
	}
	if !stableID.MatchString(value.ID) {
		addIssue(result, "error", path, "fragment id must be stable and non-empty")
	}
	if !ValidMediaType(value.MediaType) {
		addIssue(result, "error", path, "unsupported media_type")
	}
	if reason := EntrypointError(value.Entrypoint); reason != "" {
		addIssue(result, "error", path, reason)
		return
	}
	if reason := PortablePathWarning(value.Entrypoint); reason != "" {
		addIssue(result, "warning", path, fmt.Sprintf("entrypoint %q %s", value.Entrypoint, reason))
	}
	entry := filepath.Join(dir, filepath.FromSlash(value.Entrypoint))
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
	if !validateContent {
		return
	}
	if value.MediaType == "text/markdown" {
		validateMarkdownHeadingAnchors(realEntry, path, result)
	}
	validateAuthoredFragment(realEntry, value.MediaType, path, result)
}

func validateAuthoredFragment(entrypoint, mediaType, path string, result *Validation) {
	content, err := os.ReadFile(entrypoint)
	if err != nil {
		return
	}
	trimmed := strings.TrimSpace(string(content))
	if (mediaType == "text/markdown" || mediaType == "text/plain") && trimmed == "" {
		addIssue(result, "warning", path, "fragment content is empty; author it or remove the fragment before handoff")
		return
	}
	for _, scaffold := range []string{
		"Explain the change as a whole. Lead with the context that makes the rest of the saga easier to navigate.",
		"Explain this chapter as an independently reviewable change. Describe its boundary, behavior, and risks.",
		"Write this review fragment.",
	} {
		if trimmed == scaffold {
			addIssue(result, "error", path, "fragment still contains generated authoring instructions; replace them with reviewer-facing content")
			return
		}
	}
	if strings.Contains(trimmed, "Replace with a useful diagram") || strings.Contains(trimmed, "<h1>Interactive fragment</h1>") {
		addIssue(result, "error", path, "fragment still contains the generated example; replace it with reviewer-facing content")
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
		} else if content, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); err == nil && !containsElementID(string(content), selector.ElementID) {
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
		} else if content, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); err == nil && !textSelectorMatches(string(content), selector.Exact, selector.Prefix, selector.Suffix) {
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
		if fragment.MediaType != "text/html" && !strings.HasPrefix(fragment.MediaType, "image/") {
			addIssue(result, "error", value.Path, "landmark hotspots require an HTML, image, or SVG fragment")
		}
		if !validNormalizedRegion(value.Hotspot.X, value.Hotspot.Y, value.Hotspot.Width, value.Hotspot.Height) {
			addIssue(result, "error", value.Path, "landmark hotspot must define a positive normalized rectangle within the fragment")
		}
	}
}

func validateClaim(value Claim, path string, result *Validation) {
	if value.Version != CurrentVersion || !ValidID(value.ID) || value.CreatedAt.IsZero() {
		addIssue(result, "error", path, "claim requires version 2, a stable id, and created_at")
	}
	if strings.TrimSpace(value.Target) == "" {
		addIssue(result, "error", path, "claim target is required")
	}
	if !ValidClaimKind(value.Kind) {
		addIssue(result, "error", path, "claim kind must be behavior, invariant, performance, compatibility, security, data, ux, or test")
	}
	if strings.TrimSpace(value.Statement) == "" {
		addIssue(result, "error", path, "claim statement is required")
	}
	if len(value.Evidence) == 0 {
		addIssue(result, "error", path, "claim must cite at least one exact line or event diff URI")
	}
	seen := map[string]bool{}
	for index, uri := range value.Evidence {
		reference, err := diffuri.Parse(uri)
		if err != nil || reference.Kind == "file" {
			addIssue(result, "error", path, fmt.Sprintf("claim evidence %d must be a canonical line or event diff URI", index+1))
		}
		if seen[uri] {
			addIssue(result, "error", path, fmt.Sprintf("claim evidence %d duplicates an earlier URI", index+1))
		}
		seen[uri] = true
	}
}

func validateVerification(value Verification, path string, result *Validation) {
	if value.Version != CurrentVersion || !ValidID(value.ID) || value.CreatedAt.IsZero() {
		addIssue(result, "error", path, "verification requires version 2, a stable id, and created_at")
	}
	if !ValidID(value.Claim) {
		addIssue(result, "error", path, "verification claim must be a stable claim id")
	}
	if !ValidVerificationStatus(value.Status) {
		addIssue(result, "error", path, "verification status must be unverified, verified, failed, or inconclusive")
	}
	if value.Status != "unverified" && !ValidVerificationMethod(value.Method) {
		addIssue(result, "error", path, "a verification result requires method test, command, measurement, inspection, or analysis")
	}
	if value.Status == "unverified" && value.Method != "" && !ValidVerificationMethod(value.Method) {
		addIssue(result, "error", path, "verification method must be test, command, measurement, inspection, or analysis")
	}
	if strings.TrimSpace(value.Summary) == "" {
		addIssue(result, "error", path, "verification summary is required")
	}
}

func ValidClaimKind(value string) bool {
	switch value {
	case "behavior", "invariant", "performance", "compatibility", "security", "data", "ux", "test":
		return true
	default:
		return false
	}
}

func ValidVerificationStatus(value string) bool {
	switch value {
	case "unverified", "verified", "failed", "inconclusive":
		return true
	default:
		return false
	}
}

func ValidVerificationMethod(value string) bool {
	switch value {
	case "test", "command", "measurement", "inspection", "analysis":
		return true
	default:
		return false
	}
}

func validNormalizedRegion(x, y, width, height float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(width) || math.IsNaN(height) {
		return false
	}
	return x >= 0 && y >= 0 && width > 0 && height > 0 && x+width <= 1 && y+height <= 1
}

// normalized rejects NaN explicitly: every NaN comparison is false, so a plain
// range test would silently accept it as an in-range coordinate.
func normalized(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1
}

func fragmentHasExplicitHeading(fragment *Fragment, id string) bool {
	content, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint)))
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
			if !normalized(shape.X) || !normalized(shape.Y) || !normalized(shape.Width) || !normalized(shape.Height) {
				return fmt.Errorf("shape coordinates must be normalized between 0 and 1")
			}
			for _, point := range shape.Points {
				if !normalized(point.X) || !normalized(point.Y) {
					return fmt.Errorf("shape points must be normalized between 0 and 1")
				}
			}
			if shape.StrokeWidth < 0 || math.IsNaN(shape.StrokeWidth) {
				return fmt.Errorf("shape stroke_width cannot be negative")
			}
			if shape.Color != "" && !ValidAnnotationColor(shape.Color) {
				return fmt.Errorf("shape color must be a #rrggbb value")
			}
		}
	case "text":
		if anchor.Text == nil || anchor.Text.Exact == "" || len(anchor.Shapes) != 0 || anchor.Note != nil || anchor.Diff != nil {
			return fmt.Errorf("text anchor requires an exact quote")
		}
		if anchor.Text.Start < 0 || anchor.Text.End < 0 || anchor.Text.End < anchor.Text.Start {
			return fmt.Errorf("text anchor positions must be non-negative with end at or after start")
		}
		if anchor.Text.Color != "" && !ValidAnnotationColor(anchor.Text.Color) {
			return fmt.Errorf("text highlight color must be a #rrggbb value")
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
		if !normalized(anchor.Note.X) || !normalized(anchor.Note.Y) {
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
			validateVisualMappings(fragment, result)
			validateNarrativeMappings(fragment, result)
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
	claimIDs := map[string]string{}
	for _, claim := range document.Claims {
		path := relativePath(document.Root, claim.Path)
		if previous, exists := claimIDs[claim.ID]; exists {
			addIssue(result, "error", path, fmt.Sprintf("claim id %q is duplicated; first used by %s", claim.ID, previous))
		} else {
			claimIDs[claim.ID] = path
		}
		if !targets[claim.Target] {
			addIssue(result, "error", path, "claim target does not exist")
		}
		repository, _ := diffuri.CanonicalRepository(document.Manifest.Source.Repository)
		for index, uri := range claim.Evidence {
			if reference, err := diffuri.Parse(uri); err == nil && reference.Repository != repository {
				addIssue(result, "error", path, fmt.Sprintf("claim evidence %d belongs to a different source repository", index+1))
			}
		}
	}
	verificationIDs := map[string]string{}
	verifiedClaims := map[string]bool{}
	for _, verification := range document.Verifications {
		path := relativePath(document.Root, verification.Path)
		if previous, exists := verificationIDs[verification.ID]; exists {
			addIssue(result, "error", path, fmt.Sprintf("verification id %q is duplicated; first used by %s", verification.ID, previous))
		} else {
			verificationIDs[verification.ID] = path
		}
		if _, exists := claimIDs[verification.Claim]; !exists {
			addIssue(result, "error", path, fmt.Sprintf("verification references unknown claim %q", verification.Claim))
		} else {
			verifiedClaims[verification.Claim] = true
		}
	}
	for _, claim := range document.Claims {
		if !verifiedClaims[claim.ID] {
			addIssue(result, "warning", relativePath(document.Root, claim.Path), "claim has no explicit verification record; append an unverified result when it has not been checked")
		}
	}
}

func validateOutlineDocument(document *Saga, result *Validation) {
	ids := map[string]string{}
	var walk func(*Section)
	walk = func(section *Section) {
		if section.Path != "" {
			if previous, exists := ids[section.ID]; exists {
				addIssue(result, "error", section.Path, fmt.Sprintf("duplicate id %q, first used by %s", section.ID, previous))
			} else {
				ids[section.ID] = section.Path
			}
		}
		for _, fragment := range section.Fragments {
			if previous, exists := ids[fragment.ID]; exists {
				addIssue(result, "error", fragment.Path, fmt.Sprintf("duplicate id %q, first used by %s", fragment.ID, previous))
			} else {
				ids[fragment.ID] = fragment.Path
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
}

func validateVisualMappings(fragment *Fragment, result *Validation) {
	visual := fragment.MediaType == "text/html" || strings.HasPrefix(fragment.MediaType, "image/")
	if !visual {
		return
	}
	if len(fragment.Landmarks) == 0 {
		addIssue(result, "warning", fragment.Path, "visual fragment has no addressable landmarks; mark meaningful nodes or regions so reviewers can link them to code")
	}
	mapped := len(fragment.Diffs) > 0
	for _, landmark := range fragment.Landmarks {
		if strings.TrimSpace(landmark.Description) == "" {
			addIssue(result, "warning", landmark.Path, "visual landmark has no semantic description; add what this element means so non-visual consumers do not have to parse its geometry")
		}
		if len(landmark.Diffs) > 0 {
			mapped = true
			if landmark.Selector.Type == "element" && landmark.Hotspot == nil && fragment.MediaType == "text/html" {
				addIssue(result, "warning", landmark.Path, "mapped HTML element has no on-canvas hit area; add hotspot geometry so reviewers can open its linked code directly from the content")
			}
			if landmark.Selector.Type == "element" && landmark.Hotspot == nil && fragment.MediaType == "image/svg+xml" && !fragmentHasUsableSVGViewBox(fragment) {
				addIssue(result, "warning", landmark.Path, "mapped SVG element cannot be positioned automatically because the SVG has no usable viewBox; add a viewBox or explicit hotspot geometry")
			}
		}
	}
	if !mapped {
		addIssue(result, "warning", fragment.Path, "visual fragment has no directly linked code; attach exact diff evidence to the fragment or its landmarks")
	}
}

func validateNarrativeMappings(fragment *Fragment, result *Validation) {
	if fragment.MediaType != "text/markdown" || len(fragment.Diffs) == 0 {
		return
	}
	for _, landmark := range fragment.Landmarks {
		if len(landmark.Diffs) > 0 && (landmark.Selector.Type == "text" || landmark.Selector.Type == "heading") {
			return
		}
	}
	addIssue(result, "warning", fragment.Path, "Markdown fragment owns code only at fragment scope; cite concrete implementation claims with evidence-bearing footnotes or attach the evidence to focused heading landmarks")
}

func fragmentHasUsableSVGViewBox(fragment *Fragment) bool {
	content, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint)))
	if err != nil {
		return false
	}
	match := svgViewBoxValidationPattern.FindStringSubmatch(string(content))
	if len(match) != 2 {
		return false
	}
	parts := strings.Fields(strings.ReplaceAll(match[1], ",", " "))
	if len(parts) != 4 {
		return false
	}
	width, widthErr := strconv.ParseFloat(parts[2], 64)
	height, heightErr := strconv.ParseFloat(parts[3], 64)
	return widthErr == nil && heightErr == nil && width > 0 && height > 0 && !math.IsInf(width, 0) && !math.IsInf(height, 0) && !math.IsNaN(width) && !math.IsNaN(height)
}

func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}
