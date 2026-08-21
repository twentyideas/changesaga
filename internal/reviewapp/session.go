package reviewapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/change-saga/change-saga/internal/coverage"
	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
)

type targetEntry struct {
	node     Node
	children []string
	diffs    []saga.DiffFile
	reviews  []saga.Review
	fragment *saga.Fragment
}

type selectorEntry struct {
	selector ResolvedSelector
	stale    *StaleSelector
	diff     int
}

type fragmentValue struct {
	data   []byte
	assets []AssetSummary
}

type session struct {
	snapshot        string
	document        *saga.Saga
	changes         gitdiff.ChangeSet
	report          coverage.Report
	targets         map[string]*targetEntry
	selectors       map[string][]selectorEntry
	selectorsByAtom map[string][]DiffOwner
	atomByURI       map[string]gitdiff.Atom
	fragments       map[string]fragmentValue
	reviewItems     []ReviewItem
	threads         map[string]ReviewThread
	threadsByDiff   map[string][]ReviewThread
}

func Open(ctx context.Context, options OpenOptions) (Session, error) {
	if strings.TrimSpace(options.SagaRoot) == "" {
		return nil, invalidArgument("saga_root is required")
	}
	absRoot, err := filepath.Abs(options.SagaRoot)
	if err != nil {
		return nil, invalidArgument("saga_root could not be resolved")
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, notFound("saga", filepath.Base(filepath.Clean(options.SagaRoot)))
		}
		return nil, newError(CodeInvalidSaga, "the saga root could not be resolved safely", false, nil, err)
	}
	document, validation, err := saga.Load(resolvedRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, notFound("saga", filepath.Base(filepath.Clean(options.SagaRoot)))
		}
		return nil, newError(CodeInvalidSaga, "the saga could not be loaded", false, nil, err)
	}
	if !validation.Valid {
		return nil, newError(CodeInvalidSaga, "the saga is invalid", false, map[string]any{"issues": sanitizeIssues(validation.Issues)}, nil)
	}
	sourceDir := options.SourceDir
	if strings.TrimSpace(sourceDir) == "" {
		sourceDir = document.Root
	}
	changes, err := gitdiff.Read(ctx, sourceDir, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
	if err != nil {
		return nil, newError(CodeSourceUnavailable, "the source comparison is unavailable", true, nil, err)
	}
	report := coverage.Evaluate(document, validation, changes)
	snapshot, err := buildSnapshot(ctx, document.Root, changes)
	if err != nil {
		return nil, newError(CodeInternal, "the session snapshot could not be created", false, nil, err)
	}
	s := &session{
		snapshot: snapshot, document: document, changes: changes, report: report,
		targets: map[string]*targetEntry{}, selectors: map[string][]selectorEntry{},
		selectorsByAtom: map[string][]DiffOwner{}, atomByURI: map[string]gitdiff.Atom{},
		fragments: map[string]fragmentValue{}, threads: map[string]ReviewThread{}, threadsByDiff: map[string][]ReviewThread{},
	}
	if err := s.build(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *session) Snapshot() string { return s.snapshot }

func (s *session) build(ctx context.Context) error {
	for _, atom := range s.changes.Atoms {
		s.atomByURI[atom.URI] = atom
	}
	s.indexSection(s.document.Section, "")
	// coverage.Report.Ownership is the single selector-resolution index. Both
	// traversal directions below are projections of it, so they cannot drift
	// from coverage's overlap and stale decisions.
	for _, atom := range s.changes.Atoms {
		for _, assignment := range s.report.Ownership[atom.Key] {
			entries := s.selectors[assignment.Target]
			for i := range entries {
				entry := &entries[i]
				if entry.selector.EvidenceFile != cleanDiagnosticPath(assignment.DiffFile) || entry.diff != assignment.Diff {
					continue
				}
				entry.selector.Atoms = append(entry.selector.Atoms, atom)
				s.selectorsByAtom[atom.URI] = append(s.selectorsByAtom[atom.URI], DiffOwner{
					Target: assignment.Target, Selector: entry.selector.URI, Note: entry.selector.Note, EvidenceFile: entry.selector.EvidenceFile,
				})
				break
			}
			s.selectors[assignment.Target] = entries
		}
	}
	for target, entries := range s.selectors {
		for i := range entries {
			entry := &entries[i]
			if len(entry.selector.Atoms) == 0 {
				entry.selector.Status = "stale"
				reason := "diff URI does not match the current source comparison"
				for _, orphan := range s.report.Orphans {
					if orphan.Assignment.Target == target && orphan.Assignment.DiffFile == entry.selector.EvidenceFile && orphan.Assignment.Diff == entry.diff {
						reason = orphan.Reason
						break
					}
				}
				entry.stale = &StaleSelector{URI: entry.selector.URI, Note: entry.selector.Note, Target: target, EvidenceFile: entry.selector.EvidenceFile, Reason: reason}
			} else {
				entry.selector.Status = "current"
			}
		}
		s.selectors[target] = entries
	}
	for uri := range s.selectorsByAtom {
		sort.SliceStable(s.selectorsByAtom[uri], func(i, j int) bool {
			left, right := s.selectorsByAtom[uri][i], s.selectorsByAtom[uri][j]
			if left.Target != right.Target {
				return left.Target < right.Target
			}
			if left.EvidenceFile != right.EvidenceFile {
				return left.EvidenceFile < right.EvidenceFile
			}
			return left.Selector < right.Selector
		})
	}
	s.indexReviewItems(ctx)
	return nil
}

func (s *session) indexSection(section *saga.Section, parent string) {
	entry := &targetEntry{
		node:  Node{Kind: section.Kind, Target: section.Target, Parent: parent, ID: section.ID, Title: section.Title, Order: section.Order},
		diffs: section.Diffs, reviews: section.Reviews,
	}
	s.targets[section.Target] = entry
	s.indexDiffs(section.Target, section.Diffs)
	for _, fragment := range section.Fragments {
		fragmentEntry := &targetEntry{
			node:  Node{Kind: "fragment", Target: fragment.Target, Parent: section.Target, ID: fragment.ID, Title: fragment.Title, Order: fragment.Order, MediaType: fragment.MediaType},
			diffs: fragment.Diffs, reviews: fragment.Reviews, fragment: fragment,
		}
		if fragmentEntry.node.Title == "" {
			fragmentEntry.node.Title = fragment.ID
		}
		if data, err := readContainedFile(fragment.Directory, fragment.Entrypoint); err == nil {
			fragmentEntry.node.Bytes = int64(len(data))
			assets, _ := listAssets(fragment)
			s.fragments[fragment.Target] = fragmentValue{data: data, assets: assets}
		}
		s.targets[fragment.Target] = fragmentEntry
		s.indexDiffs(fragment.Target, fragment.Diffs)
		entry.children = append(entry.children, fragment.Target)
		for i := range fragment.Landmarks {
			landmark := &fragment.Landmarks[i]
			landmarkEntry := &targetEntry{node: Node{
				Kind: "landmark", Target: landmark.Target, Parent: fragment.Target, ID: landmark.ID, Title: landmark.Label,
				Selector: landmarkValue(landmark.Selector),
			}, diffs: landmark.Diffs}
			s.targets[landmark.Target] = landmarkEntry
			s.indexDiffs(landmark.Target, landmark.Diffs)
			fragmentEntry.children = append(fragmentEntry.children, landmark.Target)
		}
		fragmentEntry.node.HasChildren = len(fragmentEntry.children) > 0
	}
	for _, child := range section.Children {
		s.indexSection(child, section.Target)
		entry.children = append(entry.children, child.Target)
	}
	sort.SliceStable(entry.children, func(i, j int) bool {
		left, right := s.targets[entry.children[i]].node, s.targets[entry.children[j]].node
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Target < right.Target
	})
	entry.node.HasChildren = len(entry.children) > 0
}

func (s *session) indexDiffs(target string, files []saga.DiffFile) {
	for _, file := range files {
		for index, reference := range file.Diffs {
			s.selectors[target] = append(s.selectors[target], selectorEntry{selector: ResolvedSelector{
				URI: reference.URI, Note: reference.Note, Target: target, EvidenceFile: cleanDiagnosticPath(file.Path),
			}, diff: index + 1})
		}
	}
}

func (s *session) finishNode(target string, recursive bool) Node {
	entry := s.targets[target]
	node := entry.node
	if reviews := entry.reviews; len(reviews) > 0 {
		node.Review.LatestState = reviews[len(reviews)-1].State
	}
	for _, thread := range s.document.Threads {
		if thread.Target == target && thread.State == "open" {
			node.Review.OpenThreads++
		}
	}
	for _, selector := range s.selectors[target] {
		if selector.stale != nil {
			node.Diffs.Stale++
		} else {
			node.Diffs.Current += len(selector.selector.Atoms)
		}
	}
	if recursive {
		for _, child := range entry.children {
			childNode := s.finishNode(child, true)
			node.Review.OpenThreads += childNode.Review.OpenThreads
			node.Diffs.Current += childNode.Diffs.Current
			node.Diffs.Stale += childNode.Diffs.Stale
		}
	}
	return node
}

func (s *session) Overview(ctx context.Context, _ OverviewQuery) (Overview, error) {
	if err := ctx.Err(); err != nil {
		return Overview{}, err
	}
	root := s.document.Section
	result := Overview{
		Saga:              SagaIdentity{ID: s.document.Manifest.ID, Title: s.document.Manifest.Title, PR: s.document.Manifest.PR},
		Source:            SourceSnapshot{Repository: s.changes.Repository, Base: s.changes.Base, Head: s.changes.Head, BaseOID: s.changes.BaseOID, HeadOID: s.changes.HeadOID},
		Root:              s.finishNode(root.Target, false),
		OverviewFragments: []Node{},
		Chapters:          []ChapterSummary{},
		Coverage: CoverageSummary{Complete: s.report.Complete, Total: s.report.Summary.Total, Covered: s.report.Summary.Covered,
			Uncovered: s.report.Summary.Uncovered, Overlapping: s.report.Summary.Overlapping, Stale: s.report.Summary.Orphaned},
	}
	for _, fragment := range root.Fragments {
		result.OverviewFragments = append(result.OverviewFragments, s.finishNode(fragment.Target, false))
	}
	for _, child := range root.Children {
		if child.Kind != "chapter" {
			continue
		}
		node := s.finishNode(child.Target, true)
		result.Chapters = append(result.Chapters, ChapterSummary{Node: node, ChildCount: len(child.Children), FragmentCount: len(child.Fragments), OwnsCurrent: node.Diffs.Current > 0, OwnsStale: node.Diffs.Stale > 0})
	}
	return result, nil
}

func (s *session) Children(ctx context.Context, query ChildrenQuery) (ChildrenPage, error) {
	if err := ctx.Err(); err != nil {
		return ChildrenPage{}, err
	}
	if err := s.validateTargetArgument(query.Parent); err != nil {
		return ChildrenPage{}, err
	}
	entry := s.targets[query.Parent]
	if entry == nil {
		return ChildrenPage{}, notFound("target", query.Parent)
	}
	start, end, page, err := s.page("children", query.Parent, query.Cursor, query.Limit, len(entry.children))
	if err != nil {
		return ChildrenPage{}, err
	}
	result := ChildrenPage{Parent: query.Parent, Children: []Node{}, Page: page}
	for _, target := range entry.children[start:end] {
		result.Children = append(result.Children, s.finishNode(target, false))
	}
	return result, nil
}

func (s *session) ReadFragment(ctx context.Context, query FragmentQuery) (FragmentContent, error) {
	if err := ctx.Err(); err != nil {
		return FragmentContent{}, err
	}
	if err := s.validateTargetArgument(query.Target); err != nil {
		return FragmentContent{}, err
	}
	entry := s.targets[query.Target]
	if entry == nil {
		return FragmentContent{}, notFound("fragment", query.Target)
	}
	if entry.fragment == nil {
		return FragmentContent{}, invalidArgument("target must identify a fragment")
	}
	if query.Offset < 0 {
		return FragmentContent{}, invalidArgument("offset must not be negative")
	}
	limit := query.Limit
	if limit == 0 {
		limit = DefaultFragmentLimit
	}
	if limit < 1 || limit > MaxFragmentLimit {
		return FragmentContent{}, invalidArgument(fmt.Sprintf("limit must be between 1 and %d", MaxFragmentLimit))
	}
	value, ok := s.fragments[query.Target]
	if !ok {
		return FragmentContent{}, newError(CodeUnsafePath, "the fragment entrypoint cannot be read safely", false, nil, nil)
	}
	if query.Offset > int64(len(value.data)) {
		return FragmentContent{}, invalidArgument("offset exceeds fragment size")
	}
	end := query.Offset + int64(limit)
	if end > int64(len(value.data)) {
		end = int64(len(value.data))
	}
	part := value.data[query.Offset:end]
	encoding := "base64"
	data := base64.StdEncoding.EncodeToString(part)
	if strings.HasPrefix(entry.fragment.MediaType, "text/") {
		if !utf8.Valid(value.data) {
			return FragmentContent{}, newError(CodeUnsupportedMedia, "text fragment content must be valid UTF-8", false, map[string]any{"media_type": entry.fragment.MediaType}, nil)
		}
		if query.Offset < int64(len(value.data)) && !utf8.RuneStart(value.data[query.Offset]) {
			return FragmentContent{}, invalidArgument("offset must be on a UTF-8 character boundary")
		}
		for end < int64(len(value.data)) && end > query.Offset && !utf8.RuneStart(value.data[end]) {
			end--
		}
		if end == query.Offset && end < int64(len(value.data)) {
			return FragmentContent{}, invalidArgument("limit is too small to contain the next UTF-8 character")
		}
		part = value.data[query.Offset:end]
		encoding, data = "utf-8", string(part)
	}
	digest := sha256.Sum256(value.data)
	chunk := FragmentChunk{Encoding: encoding, Data: data, Offset: query.Offset, Bytes: int64(len(value.data)), SHA256: "sha256:" + hex.EncodeToString(digest[:])}
	if end < int64(len(value.data)) {
		next := end
		chunk.NextOffset = &next
	}
	return FragmentContent{Target: query.Target, ID: entry.fragment.ID, Title: entry.node.Title, MediaType: entry.fragment.MediaType, Content: chunk, Assets: append([]AssetSummary{}, value.assets...)}, nil
}

func (s *session) FragmentDiffs(ctx context.Context, query FragmentDiffQuery) (FragmentDiffs, error) {
	if err := ctx.Err(); err != nil {
		return FragmentDiffs{}, err
	}
	if err := s.validateTargetArgument(query.Target); err != nil {
		return FragmentDiffs{}, err
	}
	if s.targets[query.Target] == nil {
		return FragmentDiffs{}, notFound("target", query.Target)
	}
	selectors := s.selectors[query.Target]
	start, end, page, err := s.page("fragment-diffs", query.Target, query.Cursor, query.Limit, len(selectors))
	if err != nil {
		return FragmentDiffs{}, err
	}
	result := FragmentDiffs{Target: query.Target, Selectors: []ResolvedSelector{}, Atoms: []gitdiff.Atom{}, Stale: []StaleSelector{}, Page: page}
	seen := map[string]bool{}
	for _, entry := range selectors[start:end] {
		result.Selectors = append(result.Selectors, entry.selector)
		if entry.stale != nil {
			result.Stale = append(result.Stale, *entry.stale)
		}
		for _, atom := range entry.selector.Atoms {
			if !seen[atom.URI] {
				result.Atoms = append(result.Atoms, atom)
				seen[atom.URI] = true
			}
		}
	}
	sortAtoms(result.Atoms)
	return result, nil
}

func (s *session) DiffOwners(ctx context.Context, query DiffOwnerQuery) (DiffOwnership, error) {
	if err := ctx.Err(); err != nil {
		return DiffOwnership{}, err
	}
	reference, err := diffuri.Parse(query.Diff)
	if err != nil {
		return DiffOwnership{}, invalidArgument("diff must be a canonical atom, event, or file URI")
	}
	if reference.Repository != s.changes.Repository || reference.Base != s.changes.BaseOID || reference.Head != s.changes.HeadOID {
		return DiffOwnership{}, notFound("diff", query.Diff)
	}
	var atoms []gitdiff.Atom
	if reference.Kind == "file" {
		for _, atom := range s.changes.Atoms {
			if atomFilePath(atom) == reference.Path {
				atoms = append(atoms, atom)
			}
		}
	} else if atom, ok := s.atomByURI[query.Diff]; ok {
		atoms = append(atoms, atom)
	}
	if len(atoms) == 0 {
		return DiffOwnership{}, notFound("diff", query.Diff)
	}
	sortAtoms(atoms)
	start, end, page, pageErr := s.page("diff-owners", query.Diff, query.Cursor, query.Limit, len(atoms))
	if pageErr != nil {
		return DiffOwnership{}, pageErr
	}
	result := DiffOwnership{Diff: query.Diff, Kind: reference.Kind, Atoms: []OwnedAtom{}, Page: page}
	for _, atom := range atoms[start:end] {
		owned := OwnedAtom{Atom: atom, Owners: append([]DiffOwner{}, s.selectorsByAtom[atom.URI]...), Threads: append([]ReviewThread{}, s.threadsByDiff[atom.URI]...)}
		result.Atoms = append(result.Atoms, owned)
	}
	return result, nil
}

func (s *session) Reviews(ctx context.Context, query ReviewQuery) (ReviewPage, error) {
	if err := ctx.Err(); err != nil {
		return ReviewPage{}, err
	}
	if query.Target != "" {
		if err := s.validateTargetArgument(query.Target); err != nil {
			return ReviewPage{}, err
		}
		if s.targets[query.Target] == nil {
			return ReviewPage{}, notFound("target", query.Target)
		}
	}
	if query.Thread != "" {
		if !saga.ValidID(query.Thread) {
			return ReviewPage{}, invalidArgument("thread must be a stable ID")
		}
		if _, ok := s.threads[query.Thread]; !ok {
			return ReviewPage{}, notFound("thread", query.Thread)
		}
	}
	if query.State != "" && !validReviewState(query.State) {
		return ReviewPage{}, invalidArgument("state is not a recognized review or thread state")
	}
	var items []ReviewItem
	for _, item := range s.reviewItems {
		if reviewItemMatches(item, query) {
			items = append(items, item)
		}
	}
	key := query.Target + "\x00" + query.Thread + "\x00" + query.State
	start, end, page, err := s.page("reviews", key, query.Cursor, query.Limit, len(items))
	if err != nil {
		return ReviewPage{}, err
	}
	return ReviewPage{Items: append([]ReviewItem{}, items[start:end]...), Page: page}, nil
}

func (s *session) Gaps(ctx context.Context, query GapQuery) (GapPage, error) {
	if err := ctx.Err(); err != nil {
		return GapPage{}, err
	}
	if query.Kind != "" && query.Kind != "uncovered" && query.Kind != "stale" && query.Kind != "overlap" {
		return GapPage{}, invalidArgument("kind must be uncovered, stale, or overlap")
	}
	var gaps []Gap
	if query.Kind == "" || query.Kind == "uncovered" {
		atoms := append([]gitdiff.Atom(nil), s.report.Uncovered...)
		sortAtoms(atoms)
		for _, atom := range atoms {
			gaps = append(gaps, Gap{Kind: "uncovered", Uncovered: &UncoveredGap{Atom: atom}})
		}
	}
	if query.Kind == "" || query.Kind == "stale" {
		var stale []StaleSelector
		for _, selectors := range s.selectors {
			for _, selector := range selectors {
				if selector.stale != nil {
					stale = append(stale, *selector.stale)
				}
			}
		}
		sort.SliceStable(stale, func(i, j int) bool {
			if stale[i].Target != stale[j].Target {
				return stale[i].Target < stale[j].Target
			}
			if stale[i].EvidenceFile != stale[j].EvidenceFile {
				return stale[i].EvidenceFile < stale[j].EvidenceFile
			}
			return stale[i].URI < stale[j].URI
		})
		for i := range stale {
			value := stale[i]
			gaps = append(gaps, Gap{Kind: "stale", Stale: &value})
		}
	}
	if query.Kind == "" || query.Kind == "overlap" {
		overlaps := append([]coverage.Overlap(nil), s.report.Overlaps...)
		sort.SliceStable(overlaps, func(i, j int) bool { return atomLess(overlaps[i].Atom, overlaps[j].Atom) })
		for _, overlap := range overlaps {
			owners := append([]DiffOwner(nil), s.selectorsByAtom[overlap.Atom.URI]...)
			gaps = append(gaps, Gap{Kind: "overlap", Overlap: &OverlapGap{Atom: overlap.Atom, Owners: owners}})
		}
	}
	start, end, page, err := s.page("gaps", query.Kind, query.Cursor, query.Limit, len(gaps))
	if err != nil {
		return GapPage{}, err
	}
	return GapPage{Gaps: append([]Gap{}, gaps[start:end]...), Page: page}, nil
}

func landmarkValue(value saga.LandmarkSelector) *LandmarkValue {
	return &LandmarkValue{Type: value.Type, ElementID: value.ElementID, HeadingID: value.HeadingID, Exact: value.Exact, Prefix: value.Prefix, Suffix: value.Suffix, X: value.X, Y: value.Y, Width: value.Width, Height: value.Height}
}

func cleanDiagnosticPath(value string) string {
	if value == "" || filepath.IsAbs(value) {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func (s *session) validateTargetArgument(value string) error {
	if strings.TrimSpace(value) == "" || !strings.HasPrefix(value, "urn:change-saga:") {
		return invalidArgument("target must be a Change Saga target URN")
	}
	return nil
}

func sanitizeIssues(issues []saga.Issue) []saga.Issue {
	result := make([]saga.Issue, len(issues))
	copy(result, issues)
	for i := range result {
		result[i].Path = cleanDiagnosticPath(result[i].Path)
		if result[i].Path == "" {
			result[i].Path = "."
		}
	}
	return result
}

func atomFilePath(atom gitdiff.Atom) string {
	if atom.Event == "rename" && atom.NewPath != "" {
		return atom.NewPath
	}
	if atom.Path != "" {
		return atom.Path
	}
	if atom.NewPath != "" {
		return atom.NewPath
	}
	return atom.OldPath
}

func sortAtoms(atoms []gitdiff.Atom) {
	sort.SliceStable(atoms, func(i, j int) bool { return atomLess(atoms[i], atoms[j]) })
}

func atomLess(left, right gitdiff.Atom) bool {
	leftPath, rightPath := atomFilePath(left), atomFilePath(right)
	if leftPath != rightPath {
		return leftPath < rightPath
	}
	if left.Side != right.Side {
		return left.Side < right.Side
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.URI < right.URI
}

func readContainedFile(root, name string) ([]byte, error) {
	rel := filepath.Clean(filepath.FromSlash(name))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe relative path")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	contained, err := filepath.Rel(realRoot, realPath)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes fragment")
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("fragment content must be a regular file")
	}
	return os.ReadFile(realPath)
}

func listAssets(fragment *saga.Fragment) ([]AssetSummary, error) {
	var assets []AssetSummary
	err := filepath.WalkDir(fragment.Directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(fragment.Directory, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for _, part := range parts {
			if strings.HasPrefix(part, "___") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || filepath.ToSlash(rel) == fragment.Entrypoint || filepath.Base(rel) == "fragment.json" {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return infoErr
		}
		mediaType := assetMediaType(filepath.Ext(rel))
		assets = append(assets, AssetSummary{Name: filepath.ToSlash(rel), MediaType: mediaType, Bytes: info.Size()})
		return nil
	})
	sort.SliceStable(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return assets, err
}

func assetMediaType(extension string) string {
	switch strings.ToLower(extension) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js", ".mjs":
		return "text/javascript"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func buildSnapshot(ctx context.Context, root string, changes gitdiff.ChangeSet) (string, error) {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "change-saga-reviewapp-v1\x00%s\x00%s\x00", changes.BaseOID, changes.HeadOID)
	if output, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "HEAD").Output(); err == nil {
		_, _ = hash.Write(bytesTrimSpace(output))
		_, _ = hash.Write([]byte{0})
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = hash.Write([]byte("symlink\x00" + target + "\x00"))
			return nil
		}
		if entry.IsDir() {
			_, _ = hash.Write([]byte("dir\x00"))
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			_, _ = hash.Write([]byte("special\x00"))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte("file\x00"))
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func bytesTrimSpace(value []byte) []byte { return []byte(strings.TrimSpace(string(value))) }

type cursorToken struct {
	Version   int    `json:"v"`
	Operation string `json:"op"`
	Key       string `json:"key"`
	Snapshot  string `json:"snapshot"`
	Offset    int    `json:"offset"`
	Checksum  string `json:"checksum"`
}

func cursorChecksum(token cursorToken) string {
	token.Checksum = ""
	data, _ := json.Marshal(token)
	digest := sha256.Sum256(append([]byte("change-saga-cursor-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

func (s *session) page(operation, key, cursor string, limit, total int) (int, int, Page, error) {
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaxPageLimit {
		return 0, 0, Page{}, invalidArgument(fmt.Sprintf("limit must be between 1 and %d", MaxPageLimit))
	}
	start := 0
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return 0, 0, Page{}, invalidArgument("cursor is invalid")
		}
		var token cursorToken
		unmarshalErr := json.Unmarshal(data, &token)
		canonical, marshalErr := json.Marshal(token)
		if unmarshalErr != nil || marshalErr != nil || !bytes.Equal(data, canonical) || len(token.Checksum) != sha256.Size*2 ||
			subtle.ConstantTimeCompare([]byte(token.Checksum), []byte(cursorChecksum(token))) != 1 ||
			token.Version != 1 || token.Operation != operation || token.Key != key {
			return 0, 0, Page{}, invalidArgument("cursor does not apply to this query")
		}
		if token.Snapshot != s.snapshot {
			return 0, 0, Page{}, newError(CodeStaleSnapshot, "the cursor belongs to a different snapshot", true, map[string]any{"expected": token.Snapshot, "actual": s.snapshot}, nil)
		}
		if token.Offset < 0 || token.Offset > total {
			return 0, 0, Page{}, invalidArgument("cursor does not apply to this query")
		}
		start = token.Offset
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := Page{}
	if end < total {
		token := cursorToken{Version: 1, Operation: operation, Key: key, Snapshot: s.snapshot, Offset: end}
		token.Checksum = cursorChecksum(token)
		data, _ := json.Marshal(token)
		next := base64.RawURLEncoding.EncodeToString(data)
		page.NextCursor = &next
	}
	return start, end, page, nil
}

func validReviewState(value string) bool {
	switch value {
	case "open", "closed", "resolved", "withdrawn", "approved", "rejected", "reviewed", "unreviewed":
		return true
	default:
		return false
	}
}

func reviewItemMatches(item ReviewItem, query ReviewQuery) bool {
	if item.Thread != nil {
		if query.Thread != "" && item.Thread.ID != query.Thread {
			return false
		}
		if query.Target != "" && item.Thread.Target != query.Target {
			return false
		}
		return query.State == "" || item.Thread.State == query.State
	}
	if query.Thread != "" || item.Event == nil {
		return false
	}
	if query.Target != "" && item.Event.Target != query.Target {
		return false
	}
	return query.State == "" || item.Event.State == query.State
}

func recordTime(item ReviewItem) time.Time {
	if item.Thread != nil {
		return item.Thread.CreatedAt
	}
	return item.Event.CreatedAt
}

func recordID(item ReviewItem) string {
	if item.Thread != nil {
		return item.Thread.ID
	}
	return item.Event.ID
}
