package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/snapshotcache"
)

// reviewSnapshot is the immutable, expensive half of the review application:
// the saga structure, source comparison, coverage result, and reverse indexes.
// Review records are deliberately absent from document. They live in a small
// review generation and are projected onto the structure without rebuilding
// any of these fields.
type reviewSnapshot struct {
	document        *saga.Saga
	validation      saga.Validation
	changes         gitdiff.ChangeSet
	report          coverage.Report
	diffErr         error
	changesByTarget map[string][]gitdiff.Atom
}

// reviewState contains only mutable review-overlay records. Keeping it apart
// from the structural/source snapshot is the central mutation boundary: a
// comment can advance this generation without touching coverage or Git diffs.
type reviewState struct {
	threads     []*saga.Thread
	diffReviews []saga.DiffReview
	byTarget    map[string][]saga.Review
	fingerprint string
}

type reviewGeneration struct {
	fingerprint string
	document    *saga.Saga
}

// snapshotCache owns two independently advancing generations. current is the
// expensive structural/source generation; review is a compact projection of
// append-only review records onto that immutable structure.
type snapshotCache struct {
	mutex sync.Mutex

	saga    string
	source  string
	current *reviewSnapshot
	review  reviewGeneration

	building bool
	buildErr error

	// builds counts structural/source work only. Review mutations must not move
	// it; tests use that fact to guard the lifecycle boundary.
	builds       int
	reviewBuilds int
}

// snapshot returns a ready request generation. The ordinary in-process test
// and embedding path builds synchronously on a miss. ListenManaged starts the
// same build asynchronously, in which case requests receive nil while the
// explicit building response is served instead of waiting for a full page.
func (a *app) snapshot(ctx context.Context) *reviewSnapshot {
	a.cache.mutex.Lock()
	if a.cache.building {
		a.cache.mutex.Unlock()
		return nil
	}
	if current := a.cache.current; current != nil {
		sagaPrint, sourcePrint := a.fingerprints(ctx, current.document.Manifest)
		if sagaPrint != "" && sagaPrint == a.cache.saga && sourcePrint != "" && sourcePrint == a.cache.source {
			if reviewPrint, err := reviewFingerprint(ctx, a.root); err == nil && reviewPrint == a.cache.review.fingerprint && a.cache.review.document != nil {
				result := snapshotWithReviews(current, a.cache.review.document)
				a.cache.mutex.Unlock()
				return result
			}
			if err := a.reloadReviewsLocked(ctx, current); err == nil {
				result := snapshotWithReviews(current, a.cache.review.document)
				a.cache.mutex.Unlock()
				return result
			}
			// A review-only read failure is not allowed to poison or replace the
			// last complete generation. Report it as unavailable and retry later.
			a.cache.mutex.Unlock()
			return nil
		}
	}
	a.cache.current, a.cache.review = nil, reviewGeneration{}
	a.cache.saga, a.cache.source = "", ""
	a.cache.building, a.cache.buildErr = true, nil
	a.cache.mutex.Unlock()

	return a.finishSnapshotBuild(ctx)
}

// startSnapshotBuild makes cold-start work observable without blocking the
// listener. It returns false when a generation is already ready or building.
func (a *app) startSnapshotBuild(ctx context.Context, done func(error)) bool {
	a.cache.mutex.Lock()
	if a.cache.current != nil || a.cache.building {
		a.cache.mutex.Unlock()
		return false
	}
	a.cache.building, a.cache.buildErr = true, nil
	a.cache.mutex.Unlock()
	go func() {
		result := a.finishSnapshotBuild(ctx)
		a.cache.mutex.Lock()
		err := a.cache.buildErr
		a.cache.mutex.Unlock()
		if result == nil && err == nil {
			err = fmt.Errorf("review cache build did not publish a generation")
		}
		if done != nil {
			done(err)
		}
	}()
	return true
}

// finishSnapshotBuild performs one structural/source build and atomically
// publishes both its immutable snapshot and the review generation observed
// alongside it. Callers must have changed cache.building from false to true.
func (a *app) finishSnapshotBuild(ctx context.Context) *reviewSnapshot {
	built, reviews, err := a.buildSnapshot(ctx)
	var sagaPrint, sourcePrint string
	if err == nil {
		sagaPrint, sourcePrint = a.fingerprints(ctx, built.document.Manifest)
	}

	a.cache.mutex.Lock()
	defer a.cache.mutex.Unlock()
	a.cache.building = false
	a.cache.buildErr = err
	if err != nil {
		return nil
	}
	a.cache.saga, a.cache.source, a.cache.current = sagaPrint, sourcePrint, built
	a.cache.review = reviewGeneration{fingerprint: reviews.fingerprint, document: composeReviewDocument(built.document, reviews)}
	return snapshotWithReviews(built, a.cache.review.document)
}

func snapshotWithReviews(structural *reviewSnapshot, document *saga.Saga) *reviewSnapshot {
	result := *structural
	result.document = document
	return &result
}

// snapshotState is intentionally tiny because it is used by the HTTP status
// path while the large generation does not exist yet.
func (a *app) snapshotState() (state string, err error) {
	a.cache.mutex.Lock()
	defer a.cache.mutex.Unlock()
	switch {
	case a.cache.building:
		return "building", nil
	case a.cache.current != nil:
		return "ready", nil
	case a.cache.buildErr != nil:
		return "error", a.cache.buildErr
	default:
		return "absent", nil
	}
}

// refreshReviewsAfterMutation is called only after reviewstore has durably
// committed a mutation. It never publishes speculative memory state. If no
// structural generation exists yet there is nothing to refresh; the cold build
// will read the just-committed records from disk.
func (a *app) refreshReviewsAfterMutation(ctx context.Context) error {
	a.cache.mutex.Lock()
	defer a.cache.mutex.Unlock()
	if a.cache.current == nil {
		return nil
	}
	return a.reloadReviewsLocked(ctx, a.cache.current)
}

func (a *app) reloadReviewsLocked(ctx context.Context, structural *reviewSnapshot) error {
	document, validation, print, err := a.loadDocumentWithStableReviews(ctx)
	if err != nil {
		return err
	}
	if !validation.Valid {
		return fmt.Errorf("saga became structurally invalid while loading review state")
	}
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	state := extractReviewState(document)
	a.cache.review = reviewGeneration{fingerprint: print, document: composeReviewDocument(structural.document, state)}
	a.cache.reviewBuilds++
	return nil
}

// fingerprints describes only structural saga bytes and the source comparison.
// Review records have their own generation and are intentionally excluded.
func (a *app) fingerprints(ctx context.Context, manifest saga.Manifest) (sagaPrint, sourcePrint string) {
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		sagaPrint, _ = structuralFingerprint(a.root)
	}()
	if print, err := a.sourceFingerprint(ctx, manifest); err == nil {
		sourcePrint = print
	}
	group.Wait()
	return sagaPrint, sourcePrint
}

func (a *app) buildSnapshot(ctx context.Context) (*reviewSnapshot, reviewState, error) {
	document, validation, reviewPrint, err := a.loadDocumentWithStableReviews(ctx)
	if err != nil {
		return nil, reviewState{}, err
	}
	if !validation.Valid {
		return nil, reviewState{}, fmt.Errorf("saga is structurally invalid; run change-saga validate")
	}
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	reviews := extractReviewState(document)
	reviews.fingerprint = reviewPrint
	structural := structuralDocument(document)
	built := &reviewSnapshot{document: structural, validation: validation}
	if a.generations == nil {
		_ = a.populateDerivedSnapshot(ctx, built)
		return built, reviews, nil
	}

	treePrint, sourcePrint := a.fingerprints(ctx, structural.Manifest)
	key := snapshotcache.Key{Saga: a.root, Tree: treePrint, Source: sourcePrint}
	populated := false
	dir, _, buildErr := a.generations.Build(key, func(stage string) error {
		if err := a.populateDerivedSnapshot(ctx, built); err != nil {
			return err
		}
		populated = true
		return writeDerivedSnapshot(filepath.Join(stage, derivedSnapshotName), built)
	})
	if buildErr != nil {
		built.diffErr = buildErr
		return built, reviews, nil
	}
	if !populated {
		if err := readDerivedSnapshot(filepath.Join(dir, derivedSnapshotName), built); err != nil {
			built.diffErr = fmt.Errorf("read cached review index: %w", err)
		}
	}
	if key.Valid() {
		_ = a.generations.Prune(key, 3)
	}
	return built, reviews, nil
}

func (a *app) populateDerivedSnapshot(ctx context.Context, built *reviewSnapshot) error {
	a.cache.mutex.Lock()
	a.cache.builds++
	a.cache.mutex.Unlock()
	built.changes, built.diffErr = gitdiff.Read(ctx, a.sourceDir, built.document.Manifest.Source.Repository, built.document.Manifest.Source.Base, built.document.Manifest.Source.Head)
	if built.diffErr != nil {
		return built.diffErr
	}
	built.report = coverage.Evaluate(built.document, built.validation, built.changes)
	built.changesByTarget = map[string][]gitdiff.Atom{}
	for _, atom := range built.changes.Atoms {
		seen := map[string]bool{}
		for _, owner := range built.report.Ownership[atom.Key] {
			if !seen[owner.Target] {
				built.changesByTarget[owner.Target] = append(built.changesByTarget[owner.Target], atom)
				seen[owner.Target] = true
			}
		}
	}
	return nil
}

const derivedSnapshotName = "review-index.json"

type persistedDerivedSnapshot struct {
	Version         int                              `json:"version"`
	Changes         gitdiff.ChangeSet                `json:"changes"`
	DisplayLines    []gitdiff.DisplayLine            `json:"display_lines"`
	Report          coverage.Report                  `json:"report"`
	Ownership       map[string][]coverage.Assignment `json:"ownership"`
	ChangesByTarget map[string][]gitdiff.Atom        `json:"changes_by_target"`
}

func writeDerivedSnapshot(path string, snapshot *reviewSnapshot) error {
	value := persistedDerivedSnapshot{
		Version: 1, Changes: snapshot.changes, DisplayLines: snapshot.changes.DisplayLines,
		Report: snapshot.report, Ownership: snapshot.report.Ownership, ChangesByTarget: snapshot.changesByTarget,
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readDerivedSnapshot(path string, snapshot *reviewSnapshot) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var value persistedDerivedSnapshot
	if err := json.NewDecoder(file).Decode(&value); err != nil {
		return err
	}
	if value.Version != 1 {
		return fmt.Errorf("unsupported review index version %d", value.Version)
	}
	value.Changes.DisplayLines = value.DisplayLines
	value.Report.Ownership = value.Ownership
	snapshot.changes, snapshot.report, snapshot.changesByTarget = value.Changes, value.Report, value.ChangesByTarget
	snapshot.diffErr = nil
	return nil
}

func extractReviewState(document *saga.Saga) reviewState {
	state := reviewState{
		threads: document.Threads, diffReviews: document.DiffReviews,
		byTarget: map[string][]saga.Review{},
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if len(section.Reviews) > 0 {
			state.byTarget[section.Target] = section.Reviews
		}
		for _, fragment := range section.Fragments {
			if len(fragment.Reviews) > 0 {
				state.byTarget[fragment.Target] = fragment.Reviews
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return state
}

func structuralDocument(document *saga.Saga) *saga.Saga {
	empty := reviewState{byTarget: map[string][]saga.Review{}}
	return composeReviewDocument(document, empty)
}

// composeReviewDocument copies only section and fragment headers. Large diff,
// landmark, claim, and content structures remain shared with the immutable
// structural generation, keeping review generations compact.
func composeReviewDocument(structural *saga.Saga, reviews reviewState) *saga.Saga {
	result := *structural
	result.Threads = reviews.threads
	result.DiffReviews = reviews.diffReviews
	result.Section = composeReviewSection(structural.Section, reviews.byTarget)
	return &result
}

func composeReviewSection(section *saga.Section, reviews map[string][]saga.Review) *saga.Section {
	if section == nil {
		return nil
	}
	result := *section
	result.Reviews = reviews[section.Target]
	result.Fragments = make([]*saga.Fragment, len(section.Fragments))
	for index, fragment := range section.Fragments {
		copy := *fragment
		copy.Reviews = reviews[fragment.Target]
		result.Fragments[index] = &copy
	}
	result.Children = make([]*saga.Section, len(section.Children))
	for index, child := range section.Children {
		result.Children[index] = composeReviewSection(child, reviews)
	}
	return &result
}

// structuralFingerprint hashes every saga entry except review-overlay
// directories. A comment therefore cannot select a new structural generation.
func structuralFingerprint(root string) (string, error) {
	return filteredTreeFingerprint(root, func(_ string, entry fs.DirEntry) (include, skip bool) {
		if entry.IsDir() && (entry.Name() == "___review" || entry.Name() == "___approvals") {
			return false, true
		}
		return true, false
	})
}

// treeFingerprint keeps the historical helper contract for callers and tests
// that need the complete saga tree. Structural cache keys use the filtered
// variant above.
func treeFingerprint(root string) (string, error) {
	return filteredTreeFingerprint(root, func(string, fs.DirEntry) (bool, bool) { return true, false })
}

func reviewFingerprint(ctx context.Context, root string) (string, error) {
	print, err := filteredTreeFingerprint(root, func(relative string, _ fs.DirEntry) (include, skip bool) {
		return reviewOverlayPath(relative), false
	})
	if err != nil {
		return "", err
	}
	// Attribution changes when review files are committed even though their
	// bytes do not. The saga may legitimately live outside Git; absence is a
	// stable value.
	head, _ := gitOutput(ctx, root, "rev-parse", "HEAD")
	return print + "\x00" + head, nil
}

// loadDocumentWithStableReviews brackets the load with review fingerprints.
// If a supported writer commits during the read, the mismatched fingerprints
// force a retry rather than publishing old memory under the new disk identity.
func (a *app) loadDocumentWithStableReviews(ctx context.Context) (*saga.Saga, saga.Validation, string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		before, err := reviewFingerprint(ctx, a.root)
		if err != nil {
			return nil, saga.Validation{}, "", err
		}
		document, validation, err := saga.Load(a.root)
		if err != nil {
			return nil, validation, "", err
		}
		after, err := reviewFingerprint(ctx, a.root)
		if err != nil {
			return nil, validation, "", err
		}
		if before == after {
			return document, validation, after, nil
		}
	}
	return nil, saga.Validation{}, "", fmt.Errorf("review state kept changing while it was being loaded")
}

func reviewOverlayPath(relative string) bool {
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "___review" || part == "___approvals" {
			return true
		}
	}
	return false
}

func filteredTreeFingerprint(root string, selectEntry func(string, fs.DirEntry) (include, skip bool)) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		include, skip := selectEntry(relative, entry)
		if skip && entry.IsDir() {
			return filepath.SkipDir
		}
		if !include {
			return nil
		}
		if entry.IsDir() {
			fmt.Fprintf(digest, "d\x00%s\x00", filepath.ToSlash(relative))
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "f\x00%s\x00%d\x00%d\x00", filepath.ToSlash(relative), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// sourceFingerprint identifies the exact comparison gitdiff.Read would
// produce. WORKTREE remains uncacheable because no cheap identity pins all of
// its bytes exactly.
func (a *app) sourceFingerprint(ctx context.Context, manifest saga.Manifest) (string, error) {
	if manifest.Source.Head == "WORKTREE" {
		return "", nil
	}
	var remote string
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		remote, _ = gitOutput(ctx, a.sourceDir, "config", "--get", "remote.origin.url")
	}()
	revisions, err := gitOutput(ctx, a.sourceDir, "rev-parse", manifest.Source.Base+"^{commit}", manifest.Source.Head+"^{commit}")
	group.Wait()
	if err != nil {
		return "", err
	}
	return manifest.Source.Repository + "\x00" + strings.Join(strings.Fields(revisions), " ") + "\x00" + remote, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
