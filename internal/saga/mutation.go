package saga

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

// MutationIndex is the small structural contract needed to append review
// records safely. It reads manifests and package names, never coverage mappings
// or authored bodies, so a comment does not parse the full saga diff index.
type MutationIndex struct {
	Root          string
	Manifest      Manifest
	Targets       map[string]string
	ReviewTargets map[string]string
}

// ReviewState is the mutable overlay stored independently from source and
// coverage indexes.
type ReviewState struct {
	Threads     []*Thread
	DiffReviews []DiffReview
	ByTarget    map[string][]Review
}

// MutationIndexFromDocument derives the compact mutation view from an already
// validated structural generation without touching disk again.
func MutationIndexFromDocument(document *Saga) MutationIndex {
	index := MutationIndex{
		Root: document.Root, Manifest: document.Manifest,
		Targets:       map[string]string{},
		ReviewTargets: map[string]string{},
	}
	var walk func(*Section)
	walk = func(section *Section) {
		dir := document.Root
		if section.Path != "" {
			dir = filepath.Join(document.Root, filepath.FromSlash(section.Path))
		}
		index.Targets[section.Target], index.ReviewTargets[section.Target] = dir, dir
		for _, fragment := range section.Fragments {
			index.Targets[fragment.Target], index.ReviewTargets[fragment.Target] = fragment.Directory, fragment.Directory
			for landmarkIndex := range fragment.Landmarks {
				landmark := &fragment.Landmarks[landmarkIndex]
				index.Targets[landmark.Target] = landmark.Directory
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return index
}

// LoadMutationIndex validates the manifest/package skeleton and returns exact
// target directories. It is intentionally bounded by authored hierarchy nodes,
// not by the number of diff mappings or changed atoms.
func LoadMutationIndex(root string) (MutationIndex, Validation, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return MutationIndex{}, Validation{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return MutationIndex{}, Validation{}, fmt.Errorf("open saga: %w", err)
	}
	if !info.IsDir() {
		return MutationIndex{}, Validation{}, fmt.Errorf("%s is not a directory", root)
	}
	var manifest Manifest
	if err := readJSON(filepath.Join(abs, "saga.json"), &manifest); err != nil {
		return MutationIndex{}, Validation{}, fmt.Errorf("read saga.json: %w", err)
	}
	validation := Validation{Valid: true, Issues: []Issue{}}
	if !strings.HasSuffix(filepath.Base(abs), ".saga") {
		addIssue(&validation, "error", ".", "saga root directory must end in .saga")
	}
	validateManifest(manifest, &validation)
	index := MutationIndex{
		Root: abs, Manifest: manifest,
		Targets:       map[string]string{SagaTarget(manifest.ID): abs},
		ReviewTargets: map[string]string{SagaTarget(manifest.ID): abs},
	}
	ids := map[string]string{}
	if err := scanMutationSection(abs, abs, sagaHierarchy, manifest.ID, manifest.Version, &index, ids, &validation); err != nil {
		return MutationIndex{}, validation, err
	}
	if manifest.Version == CurrentSagaVersion {
		designDir := filepath.Join(abs, "___design")
		if info, statErr := os.Lstat(designDir); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := scanMutationSection(abs, designDir, designHierarchy, manifest.ID, manifest.Version, &index, ids, &validation); err != nil {
				return MutationIndex{}, validation, err
			}
		}
	}
	validation.Valid = !hasErrors(validation.Issues)
	return index, validation, nil
}

func scanMutationSection(root, dir string, hierarchy hierarchyRoot, sagaID string, sagaVersion int, index *MutationIndex, ids map[string]string, validation *Validation) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if strings.HasPrefix(name, "___") {
			if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
				addIssue(validation, "error", relativePath(root, path), "reserved metadata path must be a real directory")
			} else if !knownReservedDirectory(name, hierarchy == sagaHierarchy, sagaVersion) {
				addIssue(validation, "error", relativePath(root, path), "unknown reserved directory")
			}
			continue
		}
		if !entry.IsDir() {
			for _, suffix := range []string{".chapter", ".fragment"} {
				if matches, problem := structuralEntry(entry, suffix); matches && problem != "" {
					addIssue(validation, "error", relativePath(root, path), problem)
				}
			}
			continue
		}
		if hierarchy != nestedHierarchy && !strings.HasSuffix(name, ".fragment") && !strings.HasSuffix(name, ".chapter") {
			addIssue(validation, "error", relativePath(root, path), "direct saga children must be .chapter or .fragment directories")
		}
		if strings.HasSuffix(name, ".chapter") && hierarchy == nestedHierarchy {
			addIssue(validation, "error", relativePath(root, path), "chapters must be direct children of the saga root")
		}
		if strings.HasSuffix(name, ".fragment") {
			if err := scanMutationFragment(root, path, sagaID, index, ids, validation); err != nil {
				return err
			}
			continue
		}

		manifestPath := filepath.Join(path, "section.json")
		kind := "section"
		var id string
		if strings.HasSuffix(name, ".chapter") {
			kind, manifestPath = "chapter", filepath.Join(path, "chapter.json")
			var value ChapterManifest
			if err := readJSON(manifestPath, &value); err != nil {
				addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			} else {
				id = value.ID
				validateChapterManifest(value, relativePath(root, manifestPath), validation)
			}
		} else {
			var value SectionManifest
			if err := readJSON(manifestPath, &value); err != nil {
				addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
			} else {
				id = value.ID
				validateSectionManifest(value, relativePath(root, manifestPath), validation)
			}
		}
		if id != "" {
			registerMutationID(id, relativePath(root, manifestPath), validation, ids)
			target := SectionTarget(sagaID, id)
			if kind == "chapter" {
				target = ChapterTarget(sagaID, id)
			}
			index.Targets[target], index.ReviewTargets[target] = path, path
		}
		if err := scanMutationSection(root, path, nestedHierarchy, sagaID, sagaVersion, index, ids, validation); err != nil {
			return err
		}
	}
	return nil
}

func scanMutationFragment(root, dir, sagaID string, index *MutationIndex, ids map[string]string, validation *Validation) error {
	manifestPath := filepath.Join(dir, "fragment.json")
	var value FragmentManifest
	if err := readJSON(manifestPath, &value); err != nil {
		addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
		return nil
	}
	path := relativePath(root, manifestPath)
	entrypointProblem := EntrypointError(value.Entrypoint)
	if value.Version != CurrentVersion || !ValidID(value.ID) || !ValidMediaType(value.MediaType) || entrypointProblem != "" {
		addIssue(validation, "error", path, "fragment requires version 2, a stable id, supported media type, and safe entrypoint")
	}
	if entrypointProblem == "" {
		entrypoint := filepath.Join(dir, filepath.FromSlash(value.Entrypoint))
		if info, err := os.Stat(entrypoint); err != nil || info.IsDir() {
			addIssue(validation, "error", path, "entrypoint must name an existing file")
		} else if realDir, dirErr := filepath.EvalSymlinks(dir); dirErr != nil {
			addIssue(validation, "error", path, "entrypoint cannot be resolved safely")
		} else if realEntry, entryErr := filepath.EvalSymlinks(entrypoint); entryErr != nil {
			addIssue(validation, "error", path, "entrypoint cannot be resolved safely")
		} else if relative, relErr := filepath.Rel(realDir, realEntry); relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			addIssue(validation, "error", path, "entrypoint cannot escape its fragment package")
		}
	}
	registerMutationID(value.ID, path, validation, ids)
	target := FragmentTarget(sagaID, value.ID)
	index.Targets[target], index.ReviewTargets[target] = dir, dir

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "___") {
			continue
		}
		entryPath := filepath.Join(dir, entry.Name())
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			addIssue(validation, "error", relativePath(root, entryPath), "reserved metadata path must be a real directory")
		} else if entry.Name() != "___diffs" && entry.Name() != "___landmarks" && entry.Name() != "___approvals" {
			addIssue(validation, "error", relativePath(root, filepath.Join(dir, entry.Name())), "unknown reserved directory in fragment")
		}
	}
	landmarksDir := filepath.Join(dir, "___landmarks")
	if info, err := os.Lstat(landmarksDir); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return nil
	}
	landmarks, err := os.ReadDir(landmarksDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	landmarkIDs := map[string]string{}
	for _, entry := range landmarks {
		matches, problem := structuralEntry(entry, ".landmark")
		if !matches || problem != "" {
			addIssue(validation, "error", relativePath(root, filepath.Join(landmarksDir, entry.Name())), "landmarks must be real <id>.landmark directories")
			continue
		}
		landmarkPath := filepath.Join(landmarksDir, entry.Name(), "landmark.json")
		var landmark Landmark
		if err := readJSON(landmarkPath, &landmark); err != nil {
			addIssue(validation, "error", relativePath(root, landmarkPath), err.Error())
			continue
		}
		if landmark.Version != CurrentVersion || !ValidID(landmark.ID) || strings.TrimSpace(landmark.Label) == "" {
			addIssue(validation, "error", relativePath(root, landmarkPath), "landmark requires version 2, a stable id, and label")
		}
		if strings.TrimSuffix(entry.Name(), ".landmark") != landmark.ID {
			addIssue(validation, "error", relativePath(root, landmarkPath), "landmark id must match its directory")
		}
		if previous, ok := landmarkIDs[landmark.ID]; ok {
			addIssue(validation, "error", relativePath(root, landmarkPath), fmt.Sprintf("landmark id %q is duplicated; first used by %s", landmark.ID, previous))
		} else {
			landmarkIDs[landmark.ID] = relativePath(root, landmarkPath)
		}
		index.Targets[LandmarkTarget(sagaID, value.ID, landmark.ID)] = filepath.Dir(landmarkPath)
	}
	return nil
}

func registerMutationID(id, path string, validation *Validation, ids map[string]string) {
	if previous, ok := ids[id]; ok {
		addIssue(validation, "error", path, fmt.Sprintf("duplicate id %q, first used by %s", id, previous))
		return
	}
	ids[id] = path
}

// LoadReviewState reads only review-owned paths named by a validated mutation
// index. It never opens coverage mappings or ordinary authored fragments.
func LoadReviewState(index MutationIndex) (ReviewState, Validation, error) {
	validation := Validation{Valid: true, Issues: []Issue{}}
	state := ReviewState{ByTarget: map[string][]Review{}}
	for target, dir := range index.ReviewTargets {
		if !metadataDirectorySafe(index.Root, dir, "___approvals", &validation) {
			continue
		}
		reviews, err := loadReviews(index.Root, filepath.Join(dir, "___approvals"), &validation)
		if err != nil {
			return ReviewState{}, validation, err
		}
		if len(reviews) > 0 {
			state.ByTarget[target] = reviews
		}
	}
	if metadataDirectorySafe(index.Root, index.Root, "___review", &validation) {
		reviewDir := filepath.Join(index.Root, "___review")
		if metadataDirectorySafe(index.Root, reviewDir, "threads", &validation) {
			threads, err := loadThreads(index.Root, index.Manifest.ID, loadOptions{}, &validation)
			if err != nil {
				return ReviewState{}, validation, err
			}
			state.Threads = threads
		}
		if metadataDirectorySafe(index.Root, reviewDir, "diffs", &validation) {
			reviews, err := loadDiffReviews(index.Root, &validation)
			if err != nil {
				return ReviewState{}, validation, err
			}
			state.DiffReviews = reviews
		}
	}
	repository, _ := diffuri.CanonicalRepository(index.Manifest.Source.Repository)
	for _, thread := range state.Threads {
		path := relativePath(index.Root, filepath.Join(thread.Directory, "thread.json"))
		if _, ok := index.Targets[thread.Target]; !ok {
			addIssue(&validation, "error", path, "thread target does not exist")
		}
		if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil {
			if reference, err := diffuri.Parse(thread.Anchor.Diff.URI); err == nil && reference.Repository != repository {
				addIssue(&validation, "error", path, "thread diff anchor belongs to a different source repository")
			}
		}
	}
	validation.Valid = !hasErrors(validation.Issues)
	return state, validation, nil
}
