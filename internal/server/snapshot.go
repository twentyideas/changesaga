package server

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/diffuri"
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
	identity        string
	fileOrder       []string
	fileAtoms       map[string][]int
	fileLines       map[string][]int
	atomByKey       map[string]int
	atomPathByURI   map[string]string
	targetAtoms     map[string][]int
	targetOrder     []string
	targetFiles     map[string][]string
	targetFileAtoms map[string]map[string][]int
	fileReviews     map[string]saga.DiffReview
	reviewedFiles   int
	fileOwners      map[string][]string
	fileSummaries   map[string]FileDiffView
	fileCoverage    map[string]ManifestFileView
	locations       map[string]manifestTargetLocation
	mutationIndex   saga.MutationIndex
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
			if reviewPrint, err := indexedReviewFingerprint(ctx, current.mutationIndex); err == nil && reviewPrint == a.cache.review.fingerprint && a.cache.review.document != nil {
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
	built, reviews, err := a.loadComparison(ctx)
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

func (a *app) loadComparison(ctx context.Context) (*reviewSnapshot, reviewState, error) {
	if a.comparisonLoader != nil {
		built, err := a.comparisonLoader(ctx)
		if err != nil {
			return nil, reviewState{}, err
		}
		reviews := extractReviewState(built.document)
		structural := *built
		structural.document = structuralDocument(built.document)
		if structural.mutationIndex.Root == "" {
			structural.mutationIndex = saga.MutationIndexFromDocument(structural.document)
		}
		return &structural, reviews, nil
	}
	return a.buildSnapshot(ctx)
}

func snapshotWithReviews(structural *reviewSnapshot, document *saga.Saga) *reviewSnapshot {
	result := *structural
	result.document = document
	result.fileReviews = map[string]saga.DiffReview{}
	result.reviewedFiles = 0
	result.fileSummaries = make(map[string]FileDiffView, len(structural.fileSummaries))
	for path, summary := range structural.fileSummaries {
		result.fileSummaries[path] = summary
	}
	for _, review := range document.DiffReviews {
		reference, err := diffuri.Parse(review.URI)
		if err != nil || reference.Kind != "file" {
			continue
		}
		previous, ok := result.fileReviews[reference.Path]
		if !ok || previous.CreatedAt.Before(review.CreatedAt) || previous.CreatedAt.Equal(review.CreatedAt) && previous.ID < review.ID {
			result.fileReviews[reference.Path] = review
		}
	}
	for path, review := range result.fileReviews {
		if review.State == "reviewed" {
			result.reviewedFiles++
		}
		if summary, ok := result.fileSummaries[path]; ok {
			summary.Reviewed = review.State == "reviewed"
			summary.Reviewer = review.Author
			summary.ReviewerDetail = review.AttributionDetail
			result.fileSummaries[path] = summary
		}
	}
	return &result
}

// cachedCoverageTotals reads an already-published generation without starting
// or waiting for a build. The root shell can therefore report ready totals but
// stays bounded and responsive while the comparison index is cold.
func (a *app) cachedCoverageTotals() *coverageTotalsView {
	a.cache.mutex.Lock()
	defer a.cache.mutex.Unlock()
	current := a.cache.current
	if current == nil || current.diffErr != nil {
		return nil
	}
	return &coverageTotalsView{
		Files: len(current.fileOrder), Total: current.report.Summary.Total,
		Covered: current.report.Summary.Covered, Uncovered: current.report.Summary.Uncovered,
		Overlapping: current.report.Summary.Overlapping, Orphaned: current.report.Summary.Orphaned,
		Complete: current.report.Complete,
	}
}

// requestSnapshot is the shared cold-cache boundary for comparison endpoints.
// A managed server builds in the background, so these surfaces ask the browser
// to retry instead of blocking a request or misreporting a transient cold cache
// as a broken saga.
func (a *app) requestSnapshot(w http.ResponseWriter, r *http.Request) *reviewSnapshot {
	current := a.snapshot(r.Context())
	if current != nil {
		return current
	}
	if state, _ := a.snapshotState(); state == "building" {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Building review cache", http.StatusAccepted)
		return nil
	}
	http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
	return nil
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
	if a.reviewRefreshHook != nil {
		return a.reviewRefreshHook()
	}
	return a.reloadReviewsLocked(ctx, a.cache.current)
}

// publishReviewsAfterMutation acknowledges disk as the source of truth. A
// failed in-memory refresh invalidates only the overlay generation so the next
// read retries it; it must not turn an already durable mutation into an HTTP
// failure that invites the client to submit a duplicate record.
func (a *app) publishReviewsAfterMutation(ctx context.Context) bool {
	if err := a.refreshReviewsAfterMutation(ctx); err == nil {
		return true
	}
	a.cache.mutex.Lock()
	a.cache.review.fingerprint = ""
	a.cache.mutex.Unlock()
	return false
}

func (a *app) reloadReviewsLocked(ctx context.Context, structural *reviewSnapshot) error {
	loaded, validation, print, err := a.loadReviewStateWithStableFingerprint(ctx, structural.mutationIndex)
	if err != nil {
		return err
	}
	if !validation.Valid {
		return fmt.Errorf("saga became structurally invalid while loading review state")
	}
	state := reviewState{threads: loaded.Threads, diffReviews: loaded.DiffReviews, byTarget: loaded.ByTarget, fingerprint: print}
	document := composeReviewDocument(structural.document, state)
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	a.cache.review = reviewGeneration{fingerprint: print, document: document}
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
	built := &reviewSnapshot{document: structural, validation: validation, mutationIndex: saga.MutationIndexFromDocument(structural)}
	if a.generations == nil {
		_ = a.populateDerivedSnapshot(ctx, built)
		return built, reviews, nil
	}

	treePrint, sourcePrint := a.fingerprints(ctx, structural.Manifest)
	// The derived representation is disposable. Include its format in the
	// content key so a release that changes the on-disk shape builds a fresh
	// generation instead of opening an older generation and failing in place.
	key := snapshotcache.Key{Saga: a.root, Tree: derivedSnapshotFormat + "\x00" + treePrint, Source: sourcePrint}
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
	digest := sha256.Sum256([]byte(built.changes.Repository + "\x00" + built.changes.BaseOID + "\x00" + built.changes.HeadOID))
	built.identity = hex.EncodeToString(digest[:16])
	built.indexComparison()
	return nil
}

func (s *reviewSnapshot) indexComparison() {
	s.targetAtoms = map[string][]int{}
	for index := range s.changes.Atoms {
		atom := &s.changes.Atoms[index]
		seen := map[string]bool{}
		for _, owner := range s.report.Ownership[atom.Key] {
			if !seen[owner.Target] {
				s.targetAtoms[owner.Target] = append(s.targetAtoms[owner.Target], index)
				seen[owner.Target] = true
			}
		}
	}
	s.fileAtoms = map[string][]int{}
	s.fileLines = map[string][]int{}
	s.atomByKey = make(map[string]int, len(s.changes.Atoms))
	s.atomPathByURI = make(map[string]string, len(s.changes.Atoms))
	s.fileReviews = map[string]saga.DiffReview{}
	renameTo := map[string]string{}
	for index := range s.changes.Atoms {
		atom := &s.changes.Atoms[index]
		if atom.Kind == "event" && atom.Event == "rename" && atom.OldPath != "" && atom.NewPath != "" {
			renameTo[atom.OldPath] = atom.NewPath
		}
	}
	for index := range s.changes.Atoms {
		atom := &s.changes.Atoms[index]
		path := atom.Path
		if path == "" {
			path = atom.NewPath
		}
		if renamed := renameTo[path]; renamed != "" {
			path = renamed
		}
		s.fileAtoms[path] = append(s.fileAtoms[path], index)
		s.atomByKey[atom.Key], s.atomPathByURI[atom.URI] = index, path
	}
	for index := range s.changes.DisplayLines {
		line := &s.changes.DisplayLines[index]
		path := line.Path
		if renamed := renameTo[path]; renamed != "" {
			path = renamed
		}
		line.Path = path
		s.fileLines[path] = append(s.fileLines[path], index)
	}
	for path := range s.fileAtoms {
		s.fileOrder = append(s.fileOrder, path)
	}
	sort.Strings(s.fileOrder)
	for _, review := range s.document.DiffReviews {
		reference, err := diffuri.Parse(review.URI)
		if err != nil || reference.Kind != "file" {
			continue
		}
		previous, ok := s.fileReviews[reference.Path]
		if !ok || previous.CreatedAt.Before(review.CreatedAt) || previous.CreatedAt.Equal(review.CreatedAt) && previous.ID < review.ID {
			s.fileReviews[reference.Path] = review
		}
	}
	for _, review := range s.fileReviews {
		if review.State == "reviewed" {
			s.reviewedFiles++
		}
	}
	s.fileSummaries = make(map[string]FileDiffView, len(s.fileOrder))
	s.fileCoverage = make(map[string]ManifestFileView, len(s.fileOrder))
	for _, path := range s.fileOrder {
		uri, _ := diffuri.Build(diffuri.Reference{Repository: s.changes.Repository, Base: s.changes.BaseOID, Head: s.changes.HeadOID, Kind: "file", Path: path})
		digest := sha256.Sum256([]byte(path))
		file := FileDiffView{ID: fmt.Sprintf("diff-%x", digest[:8]), Path: path, URI: uri}
		for _, index := range s.fileAtoms[path] {
			atom := &s.changes.Atoms[index]
			if atom.Side == "new" {
				file.Added++
			} else if atom.Side == "old" {
				file.Deleted++
			}
		}
		if review, ok := s.fileReviews[path]; ok {
			file.Reviewed, file.Reviewer, file.ReviewerDetail = review.State == "reviewed", review.Author, review.AttributionDetail
		}
		s.fileSummaries[path] = file
		coverageFile := ManifestFileView{Path: path, HasDiff: true}
		for _, index := range s.fileAtoms[path] {
			atom := &s.changes.Atoms[index]
			coverageFile.AtomCount++
			if atom.Kind == "event" {
				coverageFile.Events++
			} else if atom.Side == "old" {
				coverageFile.Deleted++
			} else {
				coverageFile.Added++
			}
			if len(s.report.Ownership[atom.Key]) == 0 {
				coverageFile.Uncovered++
			} else {
				coverageFile.Covered++
			}
		}
		s.fileCoverage[path] = coverageFile
	}
	s.locations = indexManifestTargets(s.document)
	s.fileOwners = map[string][]string{}
	for path, indexes := range s.fileAtoms {
		seen := map[string]bool{}
		for _, index := range indexes {
			atom := &s.changes.Atoms[index]
			for _, assignment := range s.report.Ownership[atom.Key] {
				seen[assignment.Target] = true
			}
		}
		for target := range seen {
			s.fileOwners[path] = append(s.fileOwners[path], target)
		}
		sort.SliceStable(s.fileOwners[path], func(i, j int) bool {
			left, leftOK := s.locations[s.fileOwners[path][i]]
			right, rightOK := s.locations[s.fileOwners[path][j]]
			if leftOK && rightOK && left.order != right.order {
				return left.order < right.order
			}
			return s.fileOwners[path][i] < s.fileOwners[path][j]
		})
	}
	for target := range s.targetAtoms {
		s.targetOrder = append(s.targetOrder, target)
	}
	sort.SliceStable(s.targetOrder, func(i, j int) bool {
		left, leftOK := s.locations[s.targetOrder[i]]
		right, rightOK := s.locations[s.targetOrder[j]]
		if leftOK && rightOK && left.order != right.order {
			return left.order < right.order
		}
		return s.targetOrder[i] < s.targetOrder[j]
	})
	s.targetFiles = make(map[string][]string, len(s.targetAtoms))
	s.targetFileAtoms = make(map[string]map[string][]int, len(s.targetAtoms))
	for target, indexes := range s.targetAtoms {
		byPath := map[string][]int{}
		for _, index := range indexes {
			path := effectiveAtomPath(s.changes.Atoms[index])
			byPath[path] = append(byPath[path], index)
		}
		paths := make([]string, 0, len(byPath))
		for path := range byPath {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		s.targetFiles[target] = paths
		s.targetFileAtoms[target] = byPath
	}
}

const (
	derivedSnapshotFormat = "review-index-v3"
	derivedSnapshotName   = derivedSnapshotFormat + ".json.gz"
)

type persistedDerivedSnapshot struct {
	Version      int                              `json:"version"`
	Changes      gitdiff.ChangeSet                `json:"changes"`
	DisplayLines []gitdiff.DisplayLine            `json:"display_lines"`
	Report       coverage.Report                  `json:"report"`
	Ownership    map[string][]coverage.Assignment `json:"ownership"`
}

func writeDerivedSnapshot(path string, snapshot *reviewSnapshot) error {
	value := persistedDerivedSnapshot{
		Version: 3, Changes: snapshot.changes, DisplayLines: snapshot.changes.DisplayLines,
		Report: snapshot.report, Ownership: snapshot.report.Ownership,
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	compressed, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		_ = file.Close()
		return err
	}
	encoder := json.NewEncoder(compressed)
	if err := encoder.Encode(value); err != nil {
		_ = compressed.Close()
		_ = file.Close()
		return err
	}
	if err := compressed.Close(); err != nil {
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
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	var value persistedDerivedSnapshot
	if err := json.NewDecoder(compressed).Decode(&value); err != nil {
		return err
	}
	if value.Version != 3 {
		return fmt.Errorf("unsupported review index version %d", value.Version)
	}
	value.Changes.DisplayLines = value.DisplayLines
	value.Report.Ownership = value.Ownership
	snapshot.changes, snapshot.report = value.Changes, value.Report
	snapshot.diffErr = nil
	digest := sha256.Sum256([]byte(snapshot.changes.Repository + "\x00" + snapshot.changes.BaseOID + "\x00" + snapshot.changes.HeadOID))
	snapshot.identity = hex.EncodeToString(digest[:16])
	snapshot.indexComparison()
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
			for landmarkIndex := range fragment.Landmarks {
				landmark := &fragment.Landmarks[landmarkIndex]
				if len(landmark.Reviews) > 0 {
					state.byTarget[landmark.Target] = landmark.Reviews
				}
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
		copy.Landmarks = append([]saga.Landmark(nil), fragment.Landmarks...)
		for landmarkIndex := range copy.Landmarks {
			copy.Landmarks[landmarkIndex].Reviews = reviews[copy.Landmarks[landmarkIndex].Target]
		}
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
	index, validation, err := saga.LoadMutationIndex(root)
	if err != nil {
		return "", err
	}
	if !validation.Valid {
		return "", fmt.Errorf("saga is structurally invalid")
	}
	return indexedReviewFingerprint(ctx, index)
}

// indexedReviewFingerprint visits only mutable review roots named by the
// compact mutation index. It does not traverse coverage mappings, fragment
// bodies, or diff evidence merely to decide whether a comment changed.
func indexedReviewFingerprint(ctx context.Context, index saga.MutationIndex) (string, error) {
	if index.Manifest.Version == saga.SlideSagaVersion {
		return flatReviewFingerprint(ctx, index)
	}
	dirs := []string{filepath.Join(index.Root, "___review")}
	seen := map[string]bool{dirs[0]: true}
	for _, targetDir := range index.ReviewTargets {
		dir := filepath.Join(targetDir, "___approvals")
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	digest := sha256.New()
	for _, dir := range dirs {
		relativeDir, err := filepath.Rel(index.Root, dir)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "r\x00%s\x00", filepath.ToSlash(relativeDir))
		err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(index.Root, path)
			if err != nil {
				return err
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
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprint(digest, "missing\x00")
			continue
		}
		if err != nil {
			return "", err
		}
	}
	// Attribution changes when review files are committed even though their
	// bytes do not. The saga may legitimately live outside Git; absence is a
	// stable value.
	head, _ := gitOutput(ctx, index.Root, "rev-parse", "HEAD")
	return hex.EncodeToString(digest.Sum(nil)) + "\x00" + head, nil
}

// flatReviewFingerprint observes only the root-level records owned by the v4
// review overlay. V4 targets point at ordinary flat files or the Saga root;
// treating those locations as legacy package directories produces ENOTDIR and
// prevents every snapshot-backed surface from loading.
func flatReviewFingerprint(ctx context.Context, index saga.MutationIndex) (string, error) {
	entries, err := os.ReadDir(index.Root)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	fmt.Fprint(digest, "flat-review\x00")
	for _, entry := range entries {
		if !saga.IsFlatReviewRecord(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "f\x00%s\x00%d\x00%d\x00", entry.Name(), info.Size(), info.ModTime().UnixNano())
	}
	head, _ := gitOutput(ctx, index.Root, "rev-parse", "HEAD")
	return hex.EncodeToString(digest.Sum(nil)) + "\x00" + head, nil
}

// loadDocumentWithStableReviews brackets the load with review fingerprints.
// If a supported writer commits during the read, the mismatched fingerprints
// force a retry rather than publishing old memory under the new disk identity.
func (a *app) loadDocumentWithStableReviews(ctx context.Context) (*saga.Saga, saga.Validation, string, error) {
	index, validation, err := saga.LoadMutationIndex(a.root)
	if err != nil || !validation.Valid {
		return nil, validation, "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		before, err := indexedReviewFingerprint(ctx, index)
		if err != nil {
			return nil, saga.Validation{}, "", err
		}
		document, validation, err := saga.Load(a.root)
		if err != nil {
			return nil, validation, "", err
		}
		after, err := indexedReviewFingerprint(ctx, index)
		if err != nil {
			return nil, validation, "", err
		}
		if before == after {
			return document, validation, after, nil
		}
	}
	return nil, saga.Validation{}, "", fmt.Errorf("review state kept changing while it was being loaded")
}

func (a *app) loadReviewStateWithStableFingerprint(ctx context.Context, index saga.MutationIndex) (saga.ReviewState, saga.Validation, string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		before, err := indexedReviewFingerprint(ctx, index)
		if err != nil {
			return saga.ReviewState{}, saga.Validation{}, "", err
		}
		state, validation, err := saga.LoadReviewState(index)
		if err != nil {
			return saga.ReviewState{}, validation, "", err
		}
		after, err := indexedReviewFingerprint(ctx, index)
		if err != nil {
			return saga.ReviewState{}, validation, "", err
		}
		if before == after {
			return state, validation, after, nil
		}
	}
	return saga.ReviewState{}, saga.Validation{}, "", fmt.Errorf("review state kept changing while it was being loaded")
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
