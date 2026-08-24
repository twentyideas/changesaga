package saga

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

type loadOptions struct {
	outline      bool
	skipCoverage bool
}

func Load(root string) (*Saga, Validation, error) {
	return load(root, loadOptions{})
}

// LoadOutline reads the narrative and review metadata needed to render the
// reviewer shell without opening coverage records, landmark records, claims,
// verifications, diff reviews, fragment content, or message attachments. It is
// deliberately not a replacement for Load: callers that make readiness or
// mutation decisions must still use the complete validated model.
func LoadOutline(root string) (*Saga, Validation, error) {
	return load(root, loadOptions{outline: true, skipCoverage: true})
}

// LoadNarrative reads the complete reviewable narrative and annotations while
// leaving coverage records and diff-review state unopened. Incremental prose
// endpoints use it so reaching a chapter or fragment cannot trigger coverage
// graph construction.
func LoadNarrative(root string) (*Saga, Validation, error) {
	return load(root, loadOptions{skipCoverage: true})
}

func load(root string, options loadOptions) (*Saga, Validation, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, Validation{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, Validation{}, fmt.Errorf("open saga: %w", err)
	}
	if !info.IsDir() {
		return nil, Validation{}, fmt.Errorf("%s is not a directory", root)
	}

	var manifest Manifest
	if err := readJSON(filepath.Join(abs, "saga.json"), &manifest); err != nil {
		return nil, Validation{}, fmt.Errorf("read saga.json: %w", err)
	}
	validation := Validation{Valid: true, Issues: []Issue{}}
	if !strings.HasSuffix(filepath.Base(abs), ".saga") {
		addIssue(&validation, "error", ".", "saga root directory must end in .saga")
	}
	validateManifest(manifest, &validation)

	section, err := loadSection(abs, abs, manifest, true, options, &validation)
	if err != nil {
		return nil, validation, err
	}
	document := &Saga{Root: abs, Manifest: manifest, Section: section}
	if !options.outline && !options.skipCoverage && metadataDirectorySafe(abs, abs, "___claims", &validation) {
		document.Claims, err = loadClaims(abs, &validation)
		if err != nil {
			return nil, validation, err
		}
	}
	if !options.outline && !options.skipCoverage && metadataDirectorySafe(abs, abs, "___verifications", &validation) {
		document.Verifications, err = loadVerifications(abs, &validation)
		if err != nil {
			return nil, validation, err
		}
	}
	if metadataDirectorySafe(abs, abs, "___review", &validation) {
		reviewDir := filepath.Join(abs, "___review")
		if metadataDirectorySafe(abs, reviewDir, "threads", &validation) {
			if options.outline {
				document.Threads, err = loadThreadSummaries(abs, manifest.ID, &validation)
			} else {
				document.Threads, err = loadThreads(abs, manifest.ID, options, &validation)
			}
			if err != nil {
				return nil, validation, err
			}
		}
		if !options.skipCoverage && metadataDirectorySafe(abs, reviewDir, "diffs", &validation) {
			document.DiffReviews, err = loadDiffReviews(abs, &validation)
			if err != nil {
				return nil, validation, err
			}
		}
	}
	if options.outline {
		validateOutlineDocument(document, &validation)
	} else {
		validateDocument(document, &validation)
	}
	validation.Valid = !hasErrors(validation.Issues)
	return document, validation, nil
}

func loadSection(root, dir string, manifest Manifest, isRoot bool, options loadOptions, validation *Validation) (*Section, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		rel = ""
	}
	section := &Section{Path: filepath.ToSlash(rel), Kind: "section"}
	if isRoot {
		section.Kind = "saga"
		section.ID = manifest.ID + "-root"
		section.Title = manifest.Title
		section.Target = SagaTarget(manifest.ID)
	} else {
		if strings.HasSuffix(filepath.Base(dir), ".chapter") {
			section.Kind = "chapter"
			var value ChapterManifest
			path := filepath.Join(dir, "chapter.json")
			if err := readJSON(path, &value); err != nil {
				addIssue(validation, "error", relativePath(root, path), err.Error())
				section.ID = strings.TrimSuffix(filepath.Base(dir), ".chapter")
				section.Title = section.ID
			} else {
				section.ID, section.Title, section.Order = value.ID, value.Title, value.Order
				validateChapterManifest(value, relativePath(root, path), validation)
			}
			section.Target = ChapterTarget(manifest.ID, section.ID)
		} else {
			var value SectionManifest
			path := filepath.Join(dir, "section.json")
			if err := readJSON(path, &value); err != nil {
				addIssue(validation, "error", relativePath(root, path), err.Error())
				section.ID = filepath.Base(dir)
				section.Title = filepath.Base(dir)
			} else {
				section.ID, section.Title, section.Order = value.ID, value.Title, value.Order
				validateSectionManifest(value, relativePath(root, path), validation)
			}
			section.Target = SectionTarget(manifest.ID, section.ID)
		}
	}

	if !options.skipCoverage && metadataDirectorySafe(root, dir, "___diffs", validation) {
		section.Diffs, err = loadDiffs(root, filepath.Join(dir, "___diffs"), validation)
		if err != nil {
			return nil, err
		}
	}
	if metadataDirectorySafe(root, dir, "___approvals", validation) {
		section.Reviews, err = loadReviews(root, filepath.Join(dir, "___approvals"), validation)
		if err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			for _, suffix := range []string{".chapter", ".fragment"} {
				if matches, problem := structuralEntry(entry, suffix); matches && problem != "" {
					addIssue(validation, "error", displayPath(rel, name), problem)
				}
			}
			continue
		}
		if strings.HasPrefix(name, "___") {
			if !knownReservedDirectory(name, isRoot) {
				addIssue(validation, "error", displayPath(rel, name), "unknown reserved directory")
			}
			continue
		}
		path := filepath.Join(dir, name)
		if reason := PortabilityWarning(name); reason != "" {
			addIssue(validation, "warning", displayPath(rel, name), fmt.Sprintf("directory name %q %s", name, reason))
		}
		if isRoot && !strings.HasSuffix(name, ".fragment") && !strings.HasSuffix(name, ".chapter") {
			addIssue(validation, "error", displayPath(rel, name), "direct saga children must be .chapter or .fragment directories")
		}
		if strings.HasSuffix(name, ".chapter") && !isRoot {
			addIssue(validation, "error", displayPath(rel, name), "chapters must be direct children of the saga root")
		}
		if strings.HasSuffix(name, ".fragment") {
			fragment, err := loadFragment(root, path, manifest.ID, options, validation)
			if err != nil {
				return nil, err
			}
			section.Fragments = append(section.Fragments, fragment)
			continue
		}
		child, err := loadSection(root, path, manifest, false, options, validation)
		if err != nil {
			return nil, err
		}
		section.Children = append(section.Children, child)
	}
	sort.Slice(section.Fragments, func(i, j int) bool {
		if section.Fragments[i].Order == section.Fragments[j].Order {
			return section.Fragments[i].Path < section.Fragments[j].Path
		}
		return section.Fragments[i].Order < section.Fragments[j].Order
	})
	sort.Slice(section.Children, func(i, j int) bool {
		if section.Children[i].Order == section.Children[j].Order {
			return section.Children[i].Path < section.Children[j].Path
		}
		return section.Children[i].Order < section.Children[j].Order
	})
	return section, nil
}

func loadFragment(root, dir, sagaID string, options loadOptions, validation *Validation) (*Fragment, error) {
	manifestPath := filepath.Join(dir, "fragment.json")
	var value FragmentManifest
	if err := readJSON(manifestPath, &value); err != nil {
		addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
		value.ID = strings.TrimSuffix(filepath.Base(dir), ".fragment")
	}
	rel, _ := filepath.Rel(root, dir)
	fragment := &Fragment{
		Path: filepath.ToSlash(rel), Directory: dir, ID: value.ID, Title: value.Title,
		MediaType: value.MediaType, Entrypoint: value.Entrypoint, Order: value.Order,
		Target: FragmentTarget(sagaID, value.ID),
	}
	if options.outline {
		validateFragmentOutlineManifest(value, relativePath(root, manifestPath), dir, validation)
	} else {
		validateFragmentManifest(value, relativePath(root, manifestPath), dir, validation)
	}
	if !options.skipCoverage && metadataDirectorySafe(root, dir, "___diffs", validation) {
		var err error
		fragment.Diffs, err = loadDiffs(root, filepath.Join(dir, "___diffs"), validation)
		if err != nil {
			return nil, err
		}
	}
	if !options.outline && metadataDirectorySafe(root, dir, "___landmarks", validation) {
		var err error
		fragment.Landmarks, err = loadLandmarks(root, filepath.Join(dir, "___landmarks"), sagaID, fragment, options, validation)
		if err != nil {
			return nil, err
		}
	}
	if metadataDirectorySafe(root, dir, "___approvals", validation) {
		var err error
		fragment.Reviews, err = loadReviews(root, filepath.Join(dir, "___approvals"), validation)
		if err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "___") && entry.Name() != "___diffs" && entry.Name() != "___landmarks" && entry.Name() != "___approvals" {
			addIssue(validation, "error", relativePath(root, filepath.Join(dir, entry.Name())), "unknown reserved directory in fragment")
		}
	}
	return fragment, nil
}

func loadLandmarks(root, dir, sagaID string, fragment *Fragment, options loadOptions, validation *Validation) ([]Landmark, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Landmark
	seen := map[string]string{}
	headings := map[string]string{}
	if fragment.MediaType == "text/markdown" {
		if content, readErr := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); readErr == nil {
			for _, heading := range MarkdownHeadings(string(content)) {
				if heading.Explicit && ValidMarkdownAnchor(heading.Anchor) {
					headings[heading.Anchor] = relativePath(root, filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint)))
				}
			}
		}
	}
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if matches, problem := structuralEntry(entry, ".landmark"); !matches || problem != "" {
			addIssue(validation, "error", relativePath(root, entryPath), "landmarks must be real <id>.landmark directories")
			continue
		}
		path := filepath.Join(entryPath, "landmark.json")
		var value Landmark
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			continue
		}
		value.Path = relativePath(root, path)
		value.Directory = entryPath
		value.Target = LandmarkTarget(sagaID, fragment.ID, value.ID)
		if directoryID := strings.TrimSuffix(entry.Name(), ".landmark"); directoryID != value.ID {
			addIssue(validation, "error", value.Path, fmt.Sprintf("landmark id %q must match directory %q", value.ID, directoryID+".landmark"))
		}
		validateLandmark(value, fragment, validation)
		if previous, ok := seen[value.ID]; ok {
			addIssue(validation, "error", value.Path, fmt.Sprintf("landmark id %q is duplicated; first used by %s", value.ID, previous))
		} else if headingPath, ok := headings[value.ID]; ok && value.Selector.Type != "heading" {
			addIssue(validation, "error", value.Path, fmt.Sprintf("landmark id %q conflicts with a Markdown heading in %s", value.ID, headingPath))
		}
		seen[value.ID] = value.Path
		if !options.skipCoverage && metadataDirectorySafe(root, entryPath, "___diffs", validation) {
			value.Diffs, err = loadDiffs(root, filepath.Join(entryPath, "___diffs"), validation)
			if err != nil {
				return nil, err
			}
		}
		landmarkEntries, readErr := os.ReadDir(entryPath)
		if readErr != nil {
			return nil, readErr
		}
		for _, landmarkEntry := range landmarkEntries {
			if landmarkEntry.IsDir() && strings.HasPrefix(landmarkEntry.Name(), "___") && landmarkEntry.Name() != "___diffs" {
				addIssue(validation, "error", relativePath(root, filepath.Join(entryPath, landmarkEntry.Name())), "unknown reserved directory in landmark")
			}
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == result[j].ID {
			return result[i].Path < result[j].Path
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func loadDiffs(root, dir string, validation *Validation) ([]DiffFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []DiffFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var value DiffFile
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			continue
		}
		value.Path = relativePath(root, path)
		validateDiff(value, validation)
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func loadReviews(root, dir string, validation *Validation) ([]Review, error) {
	var result []Review
	err := loadMetaJSON(dir, func(path string) {
		var value Review
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			return
		}
		value.Path = path
		if value.Version != CurrentVersion || !stableID.MatchString(value.ID) || value.CreatedAt.IsZero() || !validReviewState(value.State) {
			addIssue(validation, "error", relativePath(root, path), "review requires version 2, a stable id, created_at, and a valid state")
		}
		result = append(result, value)
	})
	sort.Slice(result, func(i, j int) bool {
		return earlierRecord(result[i].CreatedAt, result[i].ID, result[j].CreatedAt, result[j].ID)
	})
	return result, err
}

func loadClaims(root string, validation *Validation) ([]Claim, error) {
	var claims []Claim
	err := loadFlatRecords(root, filepath.Join(root, "___claims"), "claim", validation, func(path string) {
		var value Claim
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			return
		}
		value.Path = path
		if strings.TrimSuffix(filepath.Base(path), ".json") != value.ID {
			addIssue(validation, "error", relativePath(root, path), fmt.Sprintf("claim id %q must match filename %q", value.ID, filepath.Base(path)))
		}
		validateClaim(value, relativePath(root, path), validation)
		claims = append(claims, value)
	})
	sort.Slice(claims, func(i, j int) bool {
		return earlierRecord(claims[i].CreatedAt, claims[i].ID, claims[j].CreatedAt, claims[j].ID)
	})
	return claims, err
}

func loadVerifications(root string, validation *Validation) ([]Verification, error) {
	var verifications []Verification
	err := loadFlatRecords(root, filepath.Join(root, "___verifications"), "verification", validation, func(path string) {
		var value Verification
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			return
		}
		value.Path = path
		if strings.TrimSuffix(filepath.Base(path), ".json") != value.ID {
			addIssue(validation, "error", relativePath(root, path), fmt.Sprintf("verification id %q must match filename %q", value.ID, filepath.Base(path)))
		}
		validateVerification(value, relativePath(root, path), validation)
		verifications = append(verifications, value)
	})
	sort.Slice(verifications, func(i, j int) bool {
		return earlierRecord(verifications[i].CreatedAt, verifications[i].ID, verifications[j].CreatedAt, verifications[j].ID)
	})
	return verifications, err
}

func loadFlatRecords(root, dir, kind string, validation *Validation, fn func(string)) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			addIssue(validation, "error", relativePath(root, path), kind+" records must be regular .json files, not directories or symlinks")
			continue
		}
		fn(path)
	}
	return nil
}

func loadThreads(root, sagaID string, options loadOptions, validation *Validation) ([]*Thread, error) {
	threadsDir := filepath.Join(root, "___review", "threads")
	entries, err := os.ReadDir(threadsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var threads []*Thread
	for _, entry := range entries {
		matches, problem := structuralEntry(entry, ".thread")
		if problem != "" {
			addIssue(validation, "error", relativePath(root, filepath.Join(threadsDir, entry.Name())), problem)
			continue
		}
		if !matches {
			continue
		}
		dir := filepath.Join(threadsDir, entry.Name())
		var threadManifest ThreadManifest
		manifestPath := filepath.Join(dir, "thread.json")
		if err := readJSON(manifestPath, &threadManifest); err != nil {
			addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			continue
		}
		// The reply, state, and anchor commands address a thread by its
		// directory name while the overlay identity comes from thread.json. A
		// disagreement would let one identifier resolve to a different record
		// than the other, so it is rejected rather than silently preferred.
		if directoryID := strings.TrimSuffix(entry.Name(), ".thread"); directoryID != threadManifest.ID {
			addIssue(validation, "error", relativePath(root, manifestPath), fmt.Sprintf("thread id %q must match directory %q", threadManifest.ID, directoryID+".thread"))
		}
		thread := Thread{Version: threadManifest.Version, ID: threadManifest.ID, Target: threadManifest.Target, Anchor: threadManifest.Anchor, Kind: threadManifest.Kind, Suggestion: threadManifest.Suggestion, CreatedBy: threadManifest.CreatedBy, CreatedAt: threadManifest.CreatedAt}
		thread.Directory = dir
		validateThread(thread, sagaID, relativePath(root, manifestPath), validation)
		thread.Messages, err = loadMessages(root, dir, sagaID, options, validation)
		if err != nil {
			return nil, err
		}
		if len(thread.Messages) == 0 {
			addIssue(validation, "error", relativePath(root, manifestPath), "thread must contain at least one message")
		}
		thread.Events, err = loadThreadEvents(root, dir, validation)
		if err != nil {
			return nil, err
		}
		thread.State = "open"
		if len(thread.Events) > 0 {
			sort.Slice(thread.Events, func(i, j int) bool {
				return earlierRecord(thread.Events[i].CreatedAt, thread.Events[i].ID, thread.Events[j].CreatedAt, thread.Events[j].ID)
			})
			for _, event := range thread.Events {
				if event.State != "" {
					thread.State = event.State
				}
				if event.Anchor != nil {
					thread.Anchor = *event.Anchor
				}
			}
		}
		threads = append(threads, &thread)
	}
	sort.Slice(threads, func(i, j int) bool {
		return earlierRecord(threads[i].CreatedAt, threads[i].ID, threads[j].CreatedAt, threads[j].ID)
	})
	return threads, nil
}

// loadThreadSummaries resolves only the pieces of a thread that affect shell
// navigation: its target, anchor, and latest state. Message bodies and attached
// fragments arrive with the focused narrative endpoint and are intentionally
// absent here.
func loadThreadSummaries(root, sagaID string, validation *Validation) ([]*Thread, error) {
	threadsDir := filepath.Join(root, "___review", "threads")
	entries, err := os.ReadDir(threadsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var threads []*Thread
	for _, entry := range entries {
		matches, problem := structuralEntry(entry, ".thread")
		if problem != "" {
			addIssue(validation, "error", relativePath(root, filepath.Join(threadsDir, entry.Name())), problem)
			continue
		}
		if !matches {
			continue
		}
		dir := filepath.Join(threadsDir, entry.Name())
		manifestPath := filepath.Join(dir, "thread.json")
		var manifest ThreadManifest
		if err := readJSON(manifestPath, &manifest); err != nil {
			addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			continue
		}
		if directoryID := strings.TrimSuffix(entry.Name(), ".thread"); directoryID != manifest.ID {
			addIssue(validation, "error", relativePath(root, manifestPath), fmt.Sprintf("thread id %q must match directory %q", manifest.ID, directoryID+".thread"))
		}
		thread := &Thread{Version: manifest.Version, ID: manifest.ID, Target: manifest.Target, Anchor: manifest.Anchor, Kind: manifest.Kind, Suggestion: manifest.Suggestion, CreatedBy: manifest.CreatedBy, CreatedAt: manifest.CreatedAt, Directory: dir, State: "open"}
		validateThread(*thread, sagaID, relativePath(root, manifestPath), validation)
		thread.Events, err = loadThreadEvents(root, dir, validation)
		if err != nil {
			return nil, err
		}
		sort.Slice(thread.Events, func(i, j int) bool {
			return earlierRecord(thread.Events[i].CreatedAt, thread.Events[i].ID, thread.Events[j].CreatedAt, thread.Events[j].ID)
		})
		for _, event := range thread.Events {
			if event.State != "" {
				thread.State = event.State
			}
			if event.Anchor != nil {
				thread.Anchor = *event.Anchor
			}
		}
		threads = append(threads, thread)
	}
	sort.Slice(threads, func(i, j int) bool {
		return earlierRecord(threads[i].CreatedAt, threads[i].ID, threads[j].CreatedAt, threads[j].ID)
	})
	return threads, nil
}

func loadDiffReviews(root string, validation *Validation) ([]DiffReview, error) {
	var reviews []DiffReview
	err := loadMetaJSON(filepath.Join(root, "___review", "diffs"), func(path string) {
		var value DiffReview
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			return
		}
		reference, uriErr := diffuri.Parse(value.URI)
		value.Path = path
		if value.Version != CurrentVersion || !stableID.MatchString(value.ID) || value.CreatedAt.IsZero() || value.State != "reviewed" && value.State != "unreviewed" || uriErr != nil || reference.Kind != "file" {
			addIssue(validation, "error", relativePath(root, path), "diff review requires version 2, id, created_at, reviewed/unreviewed state, and a file diff URI")
		}
		reviews = append(reviews, value)
	})
	sort.Slice(reviews, func(i, j int) bool {
		return earlierRecord(reviews[i].CreatedAt, reviews[i].ID, reviews[j].CreatedAt, reviews[j].ID)
	})
	return reviews, err
}

func loadMessages(root, threadDir, sagaID string, options loadOptions, validation *Validation) ([]*Message, error) {
	dir := filepath.Join(threadDir, "messages")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []*Message
	for _, entry := range entries {
		matches, problem := structuralEntry(entry, ".message")
		if problem != "" {
			addIssue(validation, "error", relativePath(root, filepath.Join(dir, entry.Name())), problem)
			continue
		}
		if !matches {
			continue
		}
		messageDir := filepath.Join(dir, entry.Name())
		var manifest MessageManifest
		manifestPath := filepath.Join(messageDir, "message.json")
		if err := readJSON(manifestPath, &manifest); err != nil {
			addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			continue
		}
		if manifest.Version != CurrentVersion || !stableID.MatchString(manifest.ID) || manifest.CreatedAt.IsZero() {
			addIssue(validation, "error", relativePath(root, manifestPath), "message requires version 2, a stable id, and created_at")
		}
		if directoryID := strings.TrimSuffix(entry.Name(), ".message"); directoryID != manifest.ID {
			addIssue(validation, "error", relativePath(root, manifestPath), fmt.Sprintf("message id %q must match directory %q", manifest.ID, directoryID+".message"))
		}
		message := &Message{Path: manifestPath, ID: manifest.ID, Author: manifest.Author, CreatedAt: manifest.CreatedAt}
		children, err := os.ReadDir(messageDir)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			matches, problem := structuralEntry(child, ".fragment")
			if problem != "" {
				addIssue(validation, "error", relativePath(root, filepath.Join(messageDir, child.Name())), problem)
				continue
			}
			if !matches {
				continue
			}
			fragment, err := loadFragment(root, filepath.Join(messageDir, child.Name()), sagaID, options, validation)
			if err != nil {
				return nil, err
			}
			message.Fragments = append(message.Fragments, fragment)
		}
		if len(message.Fragments) == 0 {
			addIssue(validation, "error", relativePath(root, manifestPath), "message must contain at least one fragment")
		}
		sort.Slice(message.Fragments, func(i, j int) bool {
			if message.Fragments[i].Order == message.Fragments[j].Order {
				return message.Fragments[i].Path < message.Fragments[j].Path
			}
			return message.Fragments[i].Order < message.Fragments[j].Order
		})
		messages = append(messages, message)
	}
	sort.Slice(messages, func(i, j int) bool {
		return earlierRecord(messages[i].CreatedAt, messages[i].ID, messages[j].CreatedAt, messages[j].ID)
	})
	return messages, nil
}

func loadThreadEvents(root, threadDir string, validation *Validation) ([]ThreadEvent, error) {
	var events []ThreadEvent
	err := loadMetaJSON(filepath.Join(threadDir, "events"), func(path string) {
		var value ThreadEvent
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			return
		}
		value.Path = path
		validState := value.State == "" || value.State == "open" || value.State == "resolved" || value.State == "withdrawn"
		if value.Version != CurrentVersion || !stableID.MatchString(value.ID) || value.CreatedAt.IsZero() || !validState || value.State == "" && value.Anchor == nil {
			addIssue(validation, "error", relativePath(root, path), "thread event requires version 2, a stable id, created_at, and a valid state or anchor")
		}
		if value.Anchor != nil {
			if err := ValidateAnchor(*value.Anchor); err != nil {
				addIssue(validation, "error", relativePath(root, path), err.Error())
			}
		}
		events = append(events, value)
	})
	return events, err
}

func loadMetaJSON(dir string, fn func(string)) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			fn(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse %s: more than one JSON value", filepath.Base(path))
		}
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

func displayPath(section, name string) string {
	if section == "" {
		return name
	}
	return filepath.ToSlash(filepath.Join(section, name))
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// structuralEntry classifies a directory entry that is expected to be a real
// entity directory. os.ReadDir reports a symlink as a non-directory, so an
// unchecked "skip anything that is not a directory" loop would silently drop a
// symlinked chapter, fragment, thread, or message and still report the saga as
// valid.
func structuralEntry(entry fs.DirEntry, suffix string) (matches bool, problem string) {
	if !strings.HasSuffix(entry.Name(), suffix) {
		return false, ""
	}
	if entry.Type()&fs.ModeSymlink != 0 {
		return true, fmt.Sprintf("%s entries must be real directories, not symlinks", suffix)
	}
	if !entry.IsDir() {
		return true, fmt.Sprintf("%s entries must be directories", suffix)
	}
	return true, ""
}

func knownReservedDirectory(name string, root bool) bool {
	return name == "___diffs" || name == "___approvals" || root && (name == "___review" || name == "___claims" || name == "___verifications")
}

func metadataDirectorySafe(root, sectionDir, name string, validation *Validation) bool {
	path := filepath.Join(sectionDir, name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		addIssue(validation, "error", relativePath(root, path), err.Error())
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		addIssue(validation, "error", relativePath(root, path), "reserved metadata path must be a real directory")
		return false
	}
	return true
}

func validateDiff(value DiffFile, validation *Validation) {
	if value.Version != CurrentVersion {
		addIssue(validation, "error", value.Path, fmt.Sprintf("unsupported version %d; expected %d", value.Version, CurrentVersion))
	}
	if len(value.Diffs) == 0 {
		// schema/v2/diff.schema.json requires diffs with minItems 1: an
		// evidence file that selects nothing is not a valid record.
		addIssue(validation, "error", value.Path, "diff file must contain at least one diff reference")
	}
	for i, reference := range value.Diffs {
		parsed, err := diffuri.Parse(reference.URI)
		if err != nil {
			addIssue(validation, "error", value.Path, fmt.Sprintf("diff %d: %v", i+1, err))
		} else if parsed.Kind == "file" {
			addIssue(validation, "error", value.Path, fmt.Sprintf("diff %d: coverage links must address lines or events, not files", i+1))
		}
	}
}

// earlierRecord defines the total order the format uses for append-only review
// records. SPEC.md resolves state from the latest record by created_at; two
// records can legitimately share a timestamp, so the record id breaks the tie.
// Without it, "the latest event" would depend on directory listing order and a
// thread could resolve differently on two machines holding the same commit.
func earlierRecord(leftTime time.Time, leftID string, rightTime time.Time, rightID string) bool {
	if !leftTime.Equal(rightTime) {
		return leftTime.Before(rightTime)
	}
	return leftID < rightID
}

func addIssue(validation *Validation, severity, path, message string) {
	validation.Issues = append(validation.Issues, Issue{Severity: severity, Path: path, Message: message})
}

func validReviewState(state string) bool {
	return state == "approved" || state == "rejected" || state == "closed" || state == "open"
}
