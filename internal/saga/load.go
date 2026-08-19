package saga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitattribution"
)

func Load(root string) (*Saga, Validation, error) {
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

	section, err := loadSection(abs, abs, manifest, true, &validation)
	if err != nil {
		return nil, validation, err
	}
	document := &Saga{Root: abs, Manifest: manifest, Section: section}
	if metadataDirectorySafe(abs, abs, "___review", &validation) {
		reviewDir := filepath.Join(abs, "___review")
		if metadataDirectorySafe(abs, reviewDir, "threads", &validation) {
			document.Threads, err = loadThreads(abs, manifest.ID, &validation)
			if err != nil {
				return nil, validation, err
			}
		}
		if metadataDirectorySafe(abs, reviewDir, "diffs", &validation) {
			document.DiffReviews, err = loadDiffReviews(abs, &validation)
			if err != nil {
				return nil, validation, err
			}
		}
	}
	validateDocument(document, &validation)
	attributeReviewEvents(document)
	validation.Valid = !hasErrors(validation.Issues)
	return document, validation, nil
}

func loadSection(root, dir string, manifest Manifest, isRoot bool, validation *Validation) (*Section, error) {
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

	if metadataDirectorySafe(root, dir, "___diffs", validation) {
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
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "___") {
			if !knownReservedDirectory(name, isRoot) {
				addIssue(validation, "error", displayPath(rel, name), "unknown reserved directory")
			}
			continue
		}
		path := filepath.Join(dir, name)
		if isRoot && !strings.HasSuffix(name, ".fragment") && !strings.HasSuffix(name, ".chapter") {
			addIssue(validation, "error", displayPath(rel, name), "direct saga children must be .chapter or .fragment directories")
		}
		if strings.HasSuffix(name, ".chapter") && !isRoot {
			addIssue(validation, "error", displayPath(rel, name), "chapters must be direct children of the saga root")
		}
		if strings.HasSuffix(name, ".fragment") {
			fragment, err := loadFragment(root, path, manifest.ID, validation)
			if err != nil {
				return nil, err
			}
			section.Fragments = append(section.Fragments, fragment)
			continue
		}
		child, err := loadSection(root, path, manifest, false, validation)
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

func loadFragment(root, dir, sagaID string, validation *Validation) (*Fragment, error) {
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
	validateFragmentManifest(value, relativePath(root, manifestPath), dir, validation)
	if metadataDirectorySafe(root, dir, "___diffs", validation) {
		var err error
		fragment.Diffs, err = loadDiffs(root, filepath.Join(dir, "___diffs"), validation)
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
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "___") && entry.Name() != "___diffs" && entry.Name() != "___approvals" {
			addIssue(validation, "error", relativePath(root, filepath.Join(dir, entry.Name())), "unknown reserved directory in fragment")
		}
	}
	return fragment, nil
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
		if value.Version != CurrentVersion || value.ID == "" || value.CreatedAt.IsZero() || !validReviewState(value.State) {
			addIssue(validation, "error", relativePath(root, path), "review requires version 2, id, created_at, and a valid state")
		}
		value.Path = path
		value.LegacyClaimedAuthor = value.Author
		result = append(result, value)
	})
	return result, err
}

func loadThreads(root, sagaID string, validation *Validation) ([]*Thread, error) {
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
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".thread") {
			continue
		}
		dir := filepath.Join(threadsDir, entry.Name())
		var threadManifest ThreadManifest
		manifestPath := filepath.Join(dir, "thread.json")
		if err := readJSON(manifestPath, &threadManifest); err != nil {
			addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			continue
		}
		thread := Thread{Version: threadManifest.Version, ID: threadManifest.ID, Target: threadManifest.Target, Anchor: threadManifest.Anchor, Kind: threadManifest.Kind, Suggestion: threadManifest.Suggestion, CreatedBy: threadManifest.CreatedBy, LegacyClaimedAuthor: threadManifest.CreatedBy, CreatedAt: threadManifest.CreatedAt, Path: manifestPath}
		thread.Directory = dir
		validateThread(thread, sagaID, relativePath(root, manifestPath), validation)
		thread.Messages, err = loadMessages(root, dir, sagaID, validation)
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
			sort.Slice(thread.Events, func(i, j int) bool { return thread.Events[i].CreatedAt.Before(thread.Events[j].CreatedAt) })
			thread.State = thread.Events[len(thread.Events)-1].State
		}
		threads = append(threads, &thread)
	}
	sort.Slice(threads, func(i, j int) bool { return threads[i].CreatedAt.Before(threads[j].CreatedAt) })
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
		if value.Version != CurrentVersion || !stableID.MatchString(value.ID) || value.CreatedAt.IsZero() || value.State != "reviewed" && value.State != "unreviewed" || uriErr != nil || reference.Kind != "file" {
			addIssue(validation, "error", relativePath(root, path), "diff review requires version 2, id, created_at, reviewed/unreviewed state, and a file diff URI")
		}
		value.Path = path
		value.LegacyClaimedAuthor = value.Author
		reviews = append(reviews, value)
	})
	sort.Slice(reviews, func(i, j int) bool { return reviews[i].CreatedAt.Before(reviews[j].CreatedAt) })
	return reviews, err
}

func loadMessages(root, threadDir, sagaID string, validation *Validation) ([]*Message, error) {
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
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".message") {
			continue
		}
		messageDir := filepath.Join(dir, entry.Name())
		var manifest MessageManifest
		manifestPath := filepath.Join(messageDir, "message.json")
		if err := readJSON(manifestPath, &manifest); err != nil {
			addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			continue
		}
		if manifest.Version != CurrentVersion || manifest.ID == "" || manifest.CreatedAt.IsZero() {
			addIssue(validation, "error", relativePath(root, manifestPath), "message requires version 2, id, and created_at")
		}
		message := &Message{ID: manifest.ID, Author: manifest.Author, LegacyClaimedAuthor: manifest.Author, CreatedAt: manifest.CreatedAt, Path: manifestPath}
		children, err := os.ReadDir(messageDir)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.IsDir() && strings.HasSuffix(child.Name(), ".fragment") {
				fragment, err := loadFragment(root, filepath.Join(messageDir, child.Name()), sagaID, validation)
				if err != nil {
					return nil, err
				}
				message.Fragments = append(message.Fragments, fragment)
			}
		}
		if len(message.Fragments) == 0 {
			addIssue(validation, "error", relativePath(root, manifestPath), "message must contain at least one fragment")
		}
		sort.Slice(message.Fragments, func(i, j int) bool { return message.Fragments[i].Order < message.Fragments[j].Order })
		messages = append(messages, message)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].CreatedAt.Before(messages[j].CreatedAt) })
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
		if value.Version != CurrentVersion || value.ID == "" || value.CreatedAt.IsZero() || value.State != "open" && value.State != "resolved" {
			addIssue(validation, "error", relativePath(root, path), "thread event requires version 2, id, created_at, and open/resolved state")
		}
		value.Path = path
		value.LegacyClaimedAuthor = value.Author
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

func attributeReviewEvents(document *Saga) {
	var paths []string
	for _, review := range allReviews(document.Section) {
		paths = append(paths, review.Path)
	}
	for _, review := range document.DiffReviews {
		paths = append(paths, review.Path)
	}
	for _, thread := range document.Threads {
		paths = append(paths, thread.Path)
		for _, message := range thread.Messages {
			paths = append(paths, message.Path)
		}
		for _, event := range thread.Events {
			paths = append(paths, event.Path)
		}
	}
	resolved := gitattribution.Resolve(context.Background(), document.Root, paths)
	for _, review := range allReviews(document.Section) {
		review.Attribution = resolved[filepath.Clean(review.Path)]
	}
	for i := range document.DiffReviews {
		document.DiffReviews[i].Attribution = resolved[filepath.Clean(document.DiffReviews[i].Path)]
	}
	for _, thread := range document.Threads {
		thread.Attribution = resolved[filepath.Clean(thread.Path)]
		for _, message := range thread.Messages {
			message.Attribution = resolved[filepath.Clean(message.Path)]
		}
		for i := range thread.Events {
			thread.Events[i].Attribution = resolved[filepath.Clean(thread.Events[i].Path)]
		}
		thread.StateAttribution = thread.Attribution
		if len(thread.Events) > 0 {
			thread.StateAttribution = thread.Events[len(thread.Events)-1].Attribution
		}
	}
}

func allReviews(section *Section) []*Review {
	result := make([]*Review, 0)
	for i := range section.Reviews {
		result = append(result, &section.Reviews[i])
	}
	for _, fragment := range section.Fragments {
		for i := range fragment.Reviews {
			result = append(result, &fragment.Reviews[i])
		}
	}
	for _, child := range section.Children {
		result = append(result, allReviews(child)...)
	}
	return result
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

func knownReservedDirectory(name string, root bool) bool {
	return name == "___diffs" || name == "___approvals" || root && name == "___review"
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
		addIssue(validation, "warning", value.Path, "diff file has no references")
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

func addIssue(validation *Validation, severity, path, message string) {
	validation.Issues = append(validation.Issues, Issue{Severity: severity, Path: path, Message: message})
}

func validReviewState(state string) bool {
	return state == "approved" || state == "rejected" || state == "closed" || state == "open"
}
