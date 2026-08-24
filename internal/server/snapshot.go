package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
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
)

// reviewSnapshot is everything a request needs that depends only on the saga on
// disk and the source comparison: the loaded document with Git attribution
// applied, the changed atoms, and the coverage report over them. Building one
// costs a saga load, a Git history walk, two `git diff` invocations, and a full
// coverage evaluation, and none of that changes between requests that observe
// the same bytes. Diff bodies are now served per file instead of being inlined
// into the page, so this work sits on the interactive path and not only on
// first load.
//
// Every field is immutable once published. Handlers read a snapshot
// concurrently and must not mutate the document, the change set, or the report.
type reviewSnapshot struct {
	document      *saga.Saga
	validation    saga.Validation
	changes       gitdiff.ChangeSet
	report        coverage.Report
	diffErr       error
	identity      string
	fileOrder     []string
	fileAtoms     map[string][]int
	fileLines     map[string][]int
	atomByKey     map[string]int
	atomPathByURI map[string]string
	targetAtoms   map[string][]int
	targetOrder   []string
	fileReviews   map[string]saga.DiffReview
	reviewedFiles int
	fileOwners    map[string][]string
	fileSummaries map[string]FileDiffView
	fileCoverage  map[string]ManifestFileView
	locations     map[string]manifestTargetLocation
}

// snapshotCache serves a snapshot for as long as its inputs are observably
// unchanged. A miss is always safe; a hit must never be stale, so the recorded
// fingerprint commits to the saga bytes, to the saga repository's history that
// review identity is read from, and to the exact source comparison identity.
// Any input it cannot describe exactly disables caching.
type snapshotCache struct {
	mutex   sync.Mutex
	saga    string
	source  string
	current *reviewSnapshot
	// builds counts how many times the expensive work actually ran. Reuse is a
	// correctness contract as much as a speed one — a reviewer must never read a
	// stale saga — so the count is observable and asserted by the budget tests.
	builds int
}

// snapshot returns the cached snapshot while its recorded fingerprints still
// hold, and otherwise rebuilds it. The lock is held across the rebuild so a
// burst of requests after a mutation performs the work once rather than once
// per request; a loopback reviewer server has a single reader, and serializing
// costs less than the duplicated Git work it prevents.
func (a *app) snapshot(ctx context.Context) *reviewSnapshot {
	a.cache.mutex.Lock()
	defer a.cache.mutex.Unlock()
	if current := a.cache.current; current != nil {
		sagaPrint, sourcePrint := a.fingerprints(ctx, current.document.Manifest)
		if sagaPrint != "" && sagaPrint == a.cache.saga && sourcePrint != "" && sourcePrint == a.cache.source {
			return current
		}
	}
	a.cache.saga, a.cache.source, a.cache.current = "", "", nil
	built, err := a.loadComparison(ctx)
	if err != nil {
		// A load failure is reported by the handler, never cached: the next
		// request must see the repaired saga immediately.
		return nil
	}
	// Fingerprint after the build so any write that raced the load is observed
	// as a difference on the next request rather than being captured as current.
	sagaPrint, sourcePrint := a.fingerprints(ctx, built.document.Manifest)
	if sagaPrint != "" && sourcePrint != "" {
		a.cache.saga, a.cache.source, a.cache.current = sagaPrint, sourcePrint, built
	}
	return built
}

func (a *app) loadComparison(ctx context.Context) (*reviewSnapshot, error) {
	if a.comparisonLoader != nil {
		return a.comparisonLoader(ctx)
	}
	return a.buildSnapshot(ctx)
}

// fingerprints reads both halves of the freshness check at once. Every request
// pays for this check, including the ones that only need a redirect, so the
// saga tree walk and the Git identity reads run together rather than in turn.
// Either half is empty when it could not be described exactly, which disables
// caching rather than risking a stale review.
func (a *app) fingerprints(ctx context.Context, manifest saga.Manifest) (sagaPrint, sourcePrint string) {
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		if print, err := treeFingerprint(a.root); err == nil {
			// Attribution is read from the saga's own repository, and committing
			// review records changes who a comment is attributed to without
			// changing a single byte under the saga root. A saga that is not in
			// a repository at all is legitimate; record the absence.
			head, _ := gitOutput(ctx, a.root, "rev-parse", "HEAD")
			sagaPrint = print + "\x00" + head
		}
	}()
	if print, err := a.sourceFingerprint(ctx, manifest); err == nil {
		sourcePrint = print
	}
	group.Wait()
	return sagaPrint, sourcePrint
}

func (a *app) buildSnapshot(ctx context.Context) (*reviewSnapshot, error) {
	a.cache.builds++
	document, validation, err := saga.Load(a.root)
	if err != nil {
		return nil, err
	}
	// Review identity belongs to the repository containing the saga, which can
	// be different from the source checkout used to evaluate product diffs.
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	built := &reviewSnapshot{document: document, validation: validation}
	built.changes, built.diffErr = gitdiff.Read(ctx, a.sourceDir, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
	if built.diffErr != nil {
		return built, nil
	}
	built.report = coverage.Evaluate(document, validation, built.changes)
	built.targetAtoms = map[string][]int{}
	for index := range built.changes.Atoms {
		atom := &built.changes.Atoms[index]
		seen := map[string]bool{}
		for _, owner := range built.report.Ownership[atom.Key] {
			if !seen[owner.Target] {
				built.targetAtoms[owner.Target] = append(built.targetAtoms[owner.Target], index)
				seen[owner.Target] = true
			}
		}
	}
	digest := sha256.Sum256([]byte(built.changes.Repository + "\x00" + built.changes.BaseOID + "\x00" + built.changes.HeadOID))
	built.identity = hex.EncodeToString(digest[:16])
	built.indexComparison()
	return built, nil
}

func (s *reviewSnapshot) indexComparison() {
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
}

// treeFingerprint hashes the name, size, and modification time of every entry
// beneath root. Saga mutations go through the API and land on disk, so this
// changes whenever a thread, reply, or review decision is recorded, and it also
// notices edits made outside the server.
func treeFingerprint(root string) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
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
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// sourceFingerprint identifies the exact comparison `gitdiff.Read` would
// produce. The diff of two commits is a function of the two resolved object IDs
// and of the declared repository identity that read verifies, all of which
// resolve in a few milliseconds. A `WORKTREE` head instead depends on
// uncommitted file contents, which no cheap probe describes exactly, so it
// returns the empty string and is never cached.
func (a *app) sourceFingerprint(ctx context.Context, manifest saga.Manifest) (string, error) {
	if manifest.Source.Head == "WORKTREE" {
		return "", nil
	}
	// A changed origin must invalidate the snapshot too: `gitdiff.Read` refuses a
	// comparison whose checkout no longer matches the declared repository, and a
	// cached change set would otherwise skip that check. Both reads are short and
	// independent, so they run together.
	var remote string
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		// A checkout without an origin is legitimate; record its absence.
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
