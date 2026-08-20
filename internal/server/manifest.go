package server

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/review-saga/review-saga/internal/coverage"
	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/saga"
)

// CoverageManifestView is the human-auditable, bidirectional projection of
// coverage.Report. Files prove code-to-narrative ownership; Targets prove the
// reverse narrative-to-code relationship from the same atom assignments.
type CoverageManifestView struct {
	Complete     bool
	Total        int
	Covered      int
	Uncovered    int
	Overlapping  int
	Orphaned     int
	MappingCount int
	Files        []*ManifestFileView
	Tree         []*ManifestTreeNode
	Targets      []*ManifestTargetView
	Orphans      []*ManifestOrphanView
}

// ManifestTreeNode presents coverage as the repository the reviewer already
// knows. The tree ships fully expanded so every changed file and its coverage
// state is visible without hunting, while folders remain collapsible.
type ManifestTreeNode struct {
	Name      string
	Path      string
	Kind      string
	Depth     int
	Children  []*ManifestTreeNode
	File      *ManifestFileView
	AtomCount int
	Added     int
	Deleted   int
	Uncovered int
}

type ManifestFileView struct {
	Path      string
	AtomCount int
	Added     int
	Deleted   int
	Events    int
	Covered   int
	Uncovered int
	Chunks    []*ManifestChunkView
}

type ManifestChunkView struct {
	Label     string
	Kind      string
	Side      string
	Path      string
	Excerpt   string
	Href      string
	AtomCount int
	Covered   bool
	Owners    []*ManifestOwnerView
	ownerKey  string
	startLine int
}

type ManifestOwnerView struct {
	Target  string
	Title   string
	Kind    string
	Chapter string
	Href    string
}

type ManifestTargetView struct {
	ManifestOwnerView
	AtomCount int
	Chunks    []*ManifestChunkView
}

type ManifestOrphanView struct {
	Owner  *ManifestOwnerView
	URI    string
	Reason string
}

type manifestTargetLocation struct {
	ManifestOwnerView
	order int
}

func makeCoverageManifestView(document *saga.Saga, changes gitdiff.ChangeSet, report coverage.Report) *CoverageManifestView {
	view := &CoverageManifestView{
		Complete: report.Complete, Total: report.Summary.Total, Covered: report.Summary.Covered,
		Uncovered: report.Summary.Uncovered, Overlapping: report.Summary.Overlapping, Orphaned: report.Summary.Orphaned,
	}
	locations := indexManifestTargets(document)
	files := map[string]*ManifestFileView{}
	fileAtoms := map[string][]gitdiff.Atom{}
	targetAtoms := map[string][]gitdiff.Atom{}
	for _, atom := range changes.Atoms {
		path := effectiveAtomPath(atom)
		file := files[path]
		if file == nil {
			file = &ManifestFileView{Path: path}
			files[path] = file
		}
		fileAtoms[path] = append(fileAtoms[path], atom)
		file.AtomCount++
		switch {
		case atom.Kind == "event":
			file.Events++
		case atom.Side == "old":
			file.Deleted++
		default:
			file.Added++
		}
		assignments := distinctAssignments(report.Ownership[atom.Key])
		if len(assignments) == 0 {
			file.Uncovered++
		} else {
			file.Covered++
			view.MappingCount += len(assignments)
			for _, assignment := range assignments {
				targetAtoms[assignment.Target] = append(targetAtoms[assignment.Target], atom)
			}
		}
	}
	for path, atoms := range fileAtoms {
		file := files[path]
		file.Chunks = makeManifestChunks(atoms, report.Ownership, locations, true)
		view.Files = append(view.Files, file)
	}
	sort.SliceStable(view.Files, func(i, j int) bool { return view.Files[i].Path < view.Files[j].Path })
	view.Tree = makeManifestTree(view.Files)

	targets := make([]string, 0, len(targetAtoms))
	for target := range targetAtoms {
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		left, leftOK := locations[targets[i]]
		right, rightOK := locations[targets[j]]
		if leftOK && rightOK && left.order != right.order {
			return left.order < right.order
		}
		return targets[i] < targets[j]
	})
	for _, target := range targets {
		owner := manifestOwner(target, locations)
		atoms := targetAtoms[target]
		view.Targets = append(view.Targets, &ManifestTargetView{
			ManifestOwnerView: *owner, AtomCount: len(atoms),
			Chunks: makeManifestChunks(atoms, nil, locations, false),
		})
	}
	for _, orphan := range report.Orphans {
		view.Orphans = append(view.Orphans, &ManifestOrphanView{
			Owner: manifestOwner(orphan.Assignment.Target, locations), URI: orphan.Reference.URI, Reason: orphan.Reason,
		})
	}
	return view
}

// makeManifestTree folds the flat per-file coverage rows into the repository
// hierarchy. Folder aggregates carry the same change and gap metadata as their
// files so an unexplained change is visible before the folder is opened.
func makeManifestTree(files []*ManifestFileView) []*ManifestTreeNode {
	root := &ManifestTreeNode{Kind: "folder"}
	for _, file := range files {
		current := root
		parts := strings.Split(file.Path, "/")
		for index, name := range parts {
			if index == len(parts)-1 {
				current.Children = append(current.Children, &ManifestTreeNode{Name: name, Path: file.Path, Kind: "file", File: file})
				continue
			}
			folderPath := strings.Join(parts[:index+1], "/")
			var folder *ManifestTreeNode
			for _, child := range current.Children {
				if child.Kind == "folder" && child.Name == name {
					folder = child
					break
				}
			}
			if folder == nil {
				folder = &ManifestTreeNode{Name: name, Path: folderPath, Kind: "folder"}
				current.Children = append(current.Children, folder)
			}
			current = folder
		}
	}
	finalizeManifestNode(root, -1)
	return root.Children
}

func finalizeManifestNode(node *ManifestTreeNode, depth int) {
	node.Depth = depth
	if node.File != nil {
		node.AtomCount, node.Added, node.Deleted, node.Uncovered = node.File.AtomCount, node.File.Added, node.File.Deleted, node.File.Uncovered
		return
	}
	sort.SliceStable(node.Children, func(i, j int) bool {
		if node.Children[i].Kind != node.Children[j].Kind {
			return node.Children[i].Kind == "folder"
		}
		return node.Children[i].Name < node.Children[j].Name
	})
	for _, child := range node.Children {
		finalizeManifestNode(child, depth+1)
		node.AtomCount += child.AtomCount
		node.Added += child.Added
		node.Deleted += child.Deleted
		node.Uncovered += child.Uncovered
	}
}

func distinctAssignments(assignments []coverage.Assignment) []coverage.Assignment {
	seen := map[string]bool{}
	result := make([]coverage.Assignment, 0, len(assignments))
	for _, assignment := range assignments {
		if seen[assignment.Target] {
			continue
		}
		seen[assignment.Target] = true
		result = append(result, assignment)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Target < result[j].Target })
	return result
}

func makeManifestChunks(atoms []gitdiff.Atom, ownership map[string][]coverage.Assignment, locations map[string]manifestTargetLocation, includeOwners bool) []*ManifestChunkView {
	var result []*ManifestChunkView
	for _, atom := range atoms {
		owners := []*ManifestOwnerView(nil)
		ownerKey := ""
		if includeOwners {
			assignments := distinctAssignments(ownership[atom.Key])
			ownerTargets := make([]string, 0, len(assignments))
			for _, assignment := range assignments {
				ownerTargets = append(ownerTargets, assignment.Target)
				owners = append(owners, manifestOwner(assignment.Target, locations))
			}
			ownerKey = strings.Join(ownerTargets, "\x00")
		}
		if len(result) > 0 && canExtendManifestChunk(result[len(result)-1], atom, ownerKey) {
			chunk := result[len(result)-1]
			chunk.AtomCount++
			chunk.Label = manifestRangeLabel(atom.Side, chunk.startLine, atom.Line)
			chunk.Href = manifestRangeHref(chunk.Href, atom.Line)
			continue
		}
		chunk := &ManifestChunkView{
			Kind: atom.Kind, Side: atom.Side, Path: effectiveAtomPath(atom), AtomCount: 1, Owners: owners, Covered: !includeOwners || len(owners) > 0,
			Excerpt: manifestExcerpt(atom), Href: CodeDiffURL(effectiveAtomPath(atom), atom.URI), ownerKey: ownerKey, startLine: atom.Line,
		}
		if atom.Kind == "event" {
			chunk.Label = manifestEventLabel(atom.Event)
		} else {
			chunk.Label = manifestRangeLabel(atom.Side, atom.Line, atom.Line)
		}
		result = append(result, chunk)
	}
	return result
}

func canExtendManifestChunk(chunk *ManifestChunkView, atom gitdiff.Atom, ownerKey string) bool {
	if chunk.Kind != "line" || atom.Kind != "line" || chunk.Side != atom.Side || chunk.Path != effectiveAtomPath(atom) || chunk.ownerKey != ownerKey {
		return false
	}
	return atom.Line == chunk.startLine+chunk.AtomCount
}

func manifestEventLabel(event string) string {
	if event == "" {
		return "File event"
	}
	runes := []rune(event)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func manifestRangeHref(existing string, end int) string {
	parsed, err := url.Parse(existing)
	if err != nil {
		return existing
	}
	value := parsed.Query().Get("diff")
	reference, err := diffuri.Parse(value)
	if err != nil {
		return existing
	}
	reference.End = end
	value, err = diffuri.Build(reference)
	if err != nil {
		return existing
	}
	query := parsed.Query()
	query.Set("diff", value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func manifestRangeLabel(side string, start, end int) string {
	prefix := "+"
	if side == "old" {
		prefix = "−"
	}
	if start == end {
		return fmt.Sprintf("%s%d", prefix, start)
	}
	return fmt.Sprintf("%s%d–%d", prefix, start, end)
}

func manifestExcerpt(atom gitdiff.Atom) string {
	value := strings.TrimSpace(atom.Content)
	if atom.Kind == "event" {
		value = atom.Event
		if atom.OldPath != "" || atom.NewPath != "" {
			value += " " + atom.OldPath + " → " + atom.NewPath
		}
	}
	if utf8.RuneCountInString(value) <= 100 {
		return value
	}
	runes := []rune(value)
	return string(runes[:99]) + "…"
}

func manifestOwner(target string, locations map[string]manifestTargetLocation) *ManifestOwnerView {
	if location, ok := locations[target]; ok {
		owner := location.ManifestOwnerView
		return &owner
	}
	return &ManifestOwnerView{Target: target, Title: target, Kind: "Target"}
}

func indexManifestTargets(document *saga.Saga) map[string]manifestTargetLocation {
	result := map[string]manifestTargetLocation{}
	order := 0
	add := func(target, title, kind, chapter, href string) {
		order++
		result[target] = manifestTargetLocation{ManifestOwnerView: ManifestOwnerView{
			Target: target, Title: title, Kind: kind, Chapter: chapter, Href: href,
		}, order: order}
	}
	add(document.Section.Target, document.Manifest.Title, "Saga", "Overview", "/#"+domID(document.Section.Target))
	var walk func(*saga.Section, string, string)
	walk = func(section *saga.Section, chapter, chapterHref string) {
		if section.Kind == "chapter" {
			chapter, chapterHref = section.Title, "/chapters/"+url.PathEscape(section.ID)
			add(section.Target, section.Title, "Chapter", chapter, chapterHref+"#"+domID(section.Target))
		} else if section.Kind == "section" {
			add(section.Target, section.Title, "Section", chapter, chapterHref+"#"+domID(section.Target))
		}
		for _, fragment := range section.Fragments {
			title := fragment.Title
			if title == "" {
				title = fragment.ID
			}
			fragmentHref := chapterHref + "#" + domID(fragment.Target)
			add(fragment.Target, title, "Fragment", chapter, fragmentHref)
			for index := range fragment.Landmarks {
				landmark := &fragment.Landmarks[index]
				add(landmark.Target, landmark.Label, "Landmark", chapter, fragmentHref+"--"+landmark.ID)
			}
		}
		for _, child := range section.Children {
			walk(child, chapter, chapterHref)
		}
	}
	walk(document.Section, "Overview", "/")
	return result
}
