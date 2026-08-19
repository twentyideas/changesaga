package saga

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/review-saga/review-saga/internal/diffuri"
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
	}
}

func validMediaType(value string) bool {
	return value == "text/markdown" || value == "text/html" || value == "text/plain" || value == "image/svg+xml" || strings.HasPrefix(value, "image/")
}

func validateThread(thread Thread, sagaID, path string, result *Validation) {
	if thread.Version != CurrentVersion || !stableID.MatchString(thread.ID) || thread.CreatedAt.IsZero() {
		addIssue(result, "error", path, "thread requires version 2, id, and created_at")
	}
	prefix := "urn:review-saga:" + sagaID + ":"
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

func ValidateAnchor(anchor Anchor) error {
	switch anchor.Type {
	case "target":
		if len(anchor.Shapes) != 0 || anchor.Text != nil || anchor.Diff != nil {
			return fmt.Errorf("target anchor cannot contain shapes, text, or diff data")
		}
	case "region", "drawing":
		if anchor.Coordinate != "normalized" || len(anchor.Shapes) == 0 || anchor.Text != nil || anchor.Diff != nil {
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
		if anchor.Text == nil || anchor.Text.Exact == "" || len(anchor.Shapes) != 0 || anchor.Diff != nil || anchor.Text.End < anchor.Text.Start {
			return fmt.Errorf("text anchor requires an exact quote and valid positions")
		}
	case "diff":
		if anchor.Diff == nil || len(anchor.Shapes) != 0 || anchor.Text != nil {
			return fmt.Errorf("diff anchor requires exactly one diff URI")
		}
		reference, err := diffuri.Parse(anchor.Diff.URI)
		if err != nil || reference.Kind == "file" {
			return fmt.Errorf("diff anchor requires a valid line or event diff URI")
		}
	default:
		return fmt.Errorf("anchor type must be target, region, drawing, text, or diff")
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
