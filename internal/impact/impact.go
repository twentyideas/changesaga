// Package impact projects an incoming Git comparison onto the source evidence
// owned by an existing Change Saga. It never compares authored content.
package impact

import (
	"sort"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

const Schema = "change-saga.impact/v1"

type SourceIdentity struct {
	Repository string `json:"repository"`
	Base       string `json:"base"`
	Head       string `json:"head"`
	BaseOID    string `json:"base_oid"`
	HeadOID    string `json:"head_oid"`
}

type BaselineStatus struct {
	SagaID      string         `json:"saga_id"`
	Title       string         `json:"title"`
	Source      SourceIdentity `json:"source"`
	Complete    bool           `json:"complete"`
	Covered     int            `json:"covered"`
	Total       int            `json:"total"`
	Uncovered   int            `json:"uncovered"`
	Stale       int            `json:"stale"`
	Overlapping int            `json:"overlapping"`
}

type IncomingStatus struct {
	SagaID string         `json:"saga_id,omitempty"`
	Title  string         `json:"title,omitempty"`
	Source SourceIdentity `json:"source"`
}

type Summary struct {
	IncomingAtoms         int `json:"incoming_atoms"`
	DirectIntersections   int `json:"direct_intersections"`
	ContextualAdditions   int `json:"contextual_additions"`
	NewContentRequired    int `json:"new_content_required"`
	TargetsMustUpdate     int `json:"targets_must_update"`
	TargetsConsiderUpdate int `json:"targets_consider_update"`
}

type Change struct {
	Relationship string       `json:"relationship"`
	Atom         gitdiff.Atom `json:"atom"`
}

type TargetImpact struct {
	Target        string   `json:"target"`
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Location      string   `json:"location,omitempty"`
	ContentPath   string   `json:"content_path,omitempty"`
	Action        string   `json:"action"`
	EvidenceFiles []string `json:"evidence_files"`
	Changes       []Change `json:"changes"`
}

type UnownedChange struct {
	Atom   gitdiff.Atom `json:"atom"`
	Reason string       `json:"reason"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Schema          string          `json:"schema"`
	Mode            string          `json:"mode"`
	Basis           string          `json:"basis"`
	ContentCompared bool            `json:"content_compared"`
	Baseline        BaselineStatus  `json:"baseline"`
	Incoming        IncomingStatus  `json:"incoming"`
	Summary         Summary         `json:"summary"`
	Targets         []TargetImpact  `json:"targets"`
	NewContent      []UnownedChange `json:"new_content"`
	Diagnostics     []Diagnostic    `json:"diagnostics"`
}

type targetLocation struct {
	Kind        string
	Title       string
	Location    string
	ContentPath string
}

type targetAccumulator struct {
	location targetLocation
	action   string
	evidence map[string]bool
	changes  map[string]Change
}

type ownerSet map[string][]coverage.Assignment

// Analyze projects incoming changed atoms onto the baseline Saga's ownership
// graph. baseline must describe the repository tree at incoming.BaseOID.
func Analyze(document *saga.Saga, baseline gitdiff.ChangeSet, report coverage.Report, incoming gitdiff.ChangeSet, mode string, incomingSaga *saga.Saga) Result {
	result := Result{
		Schema: Schema, Mode: mode, Basis: "source_diffs_only", ContentCompared: false,
		Baseline: BaselineStatus{
			SagaID: document.Manifest.ID, Title: document.Manifest.Title, Source: sourceIdentity(baseline),
			Complete: report.Complete, Covered: report.Summary.Covered, Total: report.Summary.Total,
			Uncovered: report.Summary.Uncovered, Stale: report.Summary.Orphaned, Overlapping: report.Summary.Overlapping,
		},
		Incoming: IncomingStatus{Source: sourceIdentity(incoming)},
		Targets:  []TargetImpact{}, NewContent: []UnownedChange{}, Diagnostics: []Diagnostic{},
	}
	if incomingSaga != nil {
		result.Incoming.SagaID = incomingSaga.Manifest.ID
		result.Incoming.Title = incomingSaga.Manifest.Title
	}
	if !report.Complete {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "baseline_incomplete",
			Message: "the maintained Saga does not completely map the incoming comparison base; impact results may omit affected content",
		})
	}

	locations := indexTargets(document)
	currentLines := map[string]map[int]gitdiff.Atom{}
	fileOwners := map[string]ownerSet{}
	for _, atom := range baseline.Atoms {
		if atom.Kind == "line" && atom.Side == "new" {
			if currentLines[atom.Path] == nil {
				currentLines[atom.Path] = map[int]gitdiff.Atom{}
			}
			currentLines[atom.Path][atom.Line] = atom
		}
		if !baselineAtomExistsAtHead(atom) {
			continue
		}
		for _, path := range atomPaths(atom) {
			if fileOwners[path] == nil {
				fileOwners[path] = ownerSet{}
			}
			mergeAssignments(fileOwners[path], report.Ownership[atom.Key])
		}
	}

	directOwners := map[string]ownerSet{}
	for _, atom := range incoming.Atoms {
		owners := ownerSet{}
		switch {
		case atom.Kind == "line" && atom.Side == "old":
			if baselineAtom, ok := currentLines[atom.Path][atom.Line]; ok && baselineAtom.Content == atom.Content {
				mergeAssignments(owners, report.Ownership[baselineAtom.Key])
			}
		case atom.Kind == "event" && atom.Event != "add":
			path := atom.Path
			if atom.Event == "rename" && atom.OldPath != "" {
				path = atom.OldPath
			}
			mergeOwnerSets(owners, fileOwners[path])
		}
		if len(owners) > 0 {
			directOwners[atom.Key] = owners
		}
	}

	incomingByKey := map[string]gitdiff.Atom{}
	linePosition := map[string]int{}
	for _, atom := range incoming.Atoms {
		incomingByKey[atom.Key] = atom
	}
	for index, line := range incoming.DisplayLines {
		if line.AtomKey != "" {
			linePosition[line.AtomKey] = index
		}
	}

	contextualOwners := map[string]ownerSet{}
	for _, atom := range incoming.Atoms {
		if atom.Kind != "line" || atom.Side != "new" {
			continue
		}
		owners := ownerSet{}
		if position, ok := linePosition[atom.Key]; ok {
			mergeOwnerSets(owners, replacementOwners(position, incoming.DisplayLines, incomingByKey, directOwners))
			if len(owners) == 0 {
				mergeOwnerSets(owners, adjacentOwners(position, incoming.DisplayLines, currentLines, report.Ownership))
			}
		}
		if len(owners) > 0 {
			contextualOwners[atom.Key] = owners
		}
	}

	accumulators := map[string]*targetAccumulator{}
	directAtoms := map[string]bool{}
	contextualAtoms := map[string]bool{}
	ownedAtoms := map[string]bool{}
	for _, atom := range incoming.Atoms {
		if owners := directOwners[atom.Key]; len(owners) > 0 {
			directAtoms[atom.Key], ownedAtoms[atom.Key] = true, true
			addTargetChanges(accumulators, locations, owners, atom, "conflicting_intersection", "must_update")
		}
		if owners := contextualOwners[atom.Key]; len(owners) > 0 {
			contextualAtoms[atom.Key], ownedAtoms[atom.Key] = true, true
			addTargetChanges(accumulators, locations, owners, atom, "additive_near_owned_code", "consider_update")
		}
		if !ownedAtoms[atom.Key] {
			result.NewContent = append(result.NewContent, UnownedChange{Atom: atom, Reason: unownedReason(atom)})
		}
	}

	for target, accumulator := range accumulators {
		impact := TargetImpact{
			Target: target, Kind: accumulator.location.Kind, Title: accumulator.location.Title,
			Location: accumulator.location.Location, ContentPath: accumulator.location.ContentPath,
			Action: accumulator.action, EvidenceFiles: keys(accumulator.evidence),
		}
		for _, change := range accumulator.changes {
			impact.Changes = append(impact.Changes, change)
		}
		sortChanges(impact.Changes)
		result.Targets = append(result.Targets, impact)
	}
	sort.Slice(result.Targets, func(i, j int) bool {
		if result.Targets[i].Action != result.Targets[j].Action {
			return result.Targets[i].Action == "must_update"
		}
		return result.Targets[i].Target < result.Targets[j].Target
	})
	sort.Slice(result.NewContent, func(i, j int) bool { return atomLess(result.NewContent[i].Atom, result.NewContent[j].Atom) })

	result.Summary.IncomingAtoms = len(incoming.Atoms)
	result.Summary.DirectIntersections = len(directAtoms)
	result.Summary.ContextualAdditions = len(contextualAtoms)
	result.Summary.NewContentRequired = len(result.NewContent)
	for _, target := range result.Targets {
		if target.Action == "must_update" {
			result.Summary.TargetsMustUpdate++
		} else {
			result.Summary.TargetsConsiderUpdate++
		}
	}
	return result
}

func sourceIdentity(changes gitdiff.ChangeSet) SourceIdentity {
	return SourceIdentity{Repository: changes.Repository, Base: changes.Base, Head: changes.Head, BaseOID: changes.BaseOID, HeadOID: changes.HeadOID}
}

func baselineAtomExistsAtHead(atom gitdiff.Atom) bool {
	if atom.Kind == "line" {
		return atom.Side == "new"
	}
	return atom.Event != "delete"
}

func atomPaths(atom gitdiff.Atom) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range []string{atom.Path, atom.NewPath} {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func mergeAssignments(destination ownerSet, assignments []coverage.Assignment) {
	for _, assignment := range assignments {
		duplicate := false
		for _, existing := range destination[assignment.Target] {
			if existing.DiffFile == assignment.DiffFile && existing.Diff == assignment.Diff {
				duplicate = true
				break
			}
		}
		if !duplicate {
			destination[assignment.Target] = append(destination[assignment.Target], assignment)
		}
	}
}

func mergeOwnerSets(destination, source ownerSet) {
	for _, assignments := range source {
		mergeAssignments(destination, assignments)
	}
}

func replacementOwners(position int, lines []gitdiff.DisplayLine, atoms map[string]gitdiff.Atom, direct map[string]ownerSet) ownerSet {
	owners := ownerSet{}
	for index := position - 1; index >= 0; index-- {
		line := lines[index]
		if line.Kind == "context" || line.Kind == "event" || line.Path != lines[position].Path {
			break
		}
		if atom := atoms[line.AtomKey]; atom.Side == "old" {
			mergeOwnerSets(owners, direct[atom.Key])
		}
	}
	for index := position + 1; index < len(lines); index++ {
		line := lines[index]
		if line.Kind == "context" || line.Kind == "event" || line.Path != lines[position].Path {
			break
		}
		if atom := atoms[line.AtomKey]; atom.Side == "old" {
			mergeOwnerSets(owners, direct[atom.Key])
		}
	}
	return owners
}

func adjacentOwners(position int, lines []gitdiff.DisplayLine, current map[string]map[int]gitdiff.Atom, ownership map[string][]coverage.Assignment) ownerSet {
	owners := ownerSet{}
	path := lines[position].Path
	for index := position - 1; index >= 0; index-- {
		line := lines[index]
		if line.Path != path || line.Kind == "event" {
			break
		}
		if line.Kind == "context" {
			if atom, ok := current[path][line.OldLine]; ok {
				mergeAssignments(owners, ownership[atom.Key])
			}
			break
		}
	}
	for index := position + 1; index < len(lines); index++ {
		line := lines[index]
		if line.Path != path || line.Kind == "event" {
			break
		}
		if line.Kind == "context" {
			if atom, ok := current[path][line.OldLine]; ok {
				mergeAssignments(owners, ownership[atom.Key])
			}
			break
		}
	}
	return owners
}

func addTargetChanges(accumulators map[string]*targetAccumulator, locations map[string]targetLocation, owners ownerSet, atom gitdiff.Atom, relationship, action string) {
	for target, assignments := range owners {
		accumulator := accumulators[target]
		if accumulator == nil {
			accumulator = &targetAccumulator{location: locations[target], action: action, evidence: map[string]bool{}, changes: map[string]Change{}}
			accumulators[target] = accumulator
		}
		if action == "must_update" {
			accumulator.action = action
		}
		for _, assignment := range assignments {
			if assignment.DiffFile != "" {
				accumulator.evidence[assignment.DiffFile] = true
			}
		}
		key := atom.Key
		if existing, ok := accumulator.changes[key]; !ok || existing.Relationship != "conflicting_intersection" {
			accumulator.changes[key] = Change{Relationship: relationship, Atom: atom}
		}
	}
}

func indexTargets(document *saga.Saga) map[string]targetLocation {
	result := map[string]targetLocation{
		document.Section.Target: {Kind: "saga", Title: document.Manifest.Title, Location: "."},
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section != document.Section {
			result[section.Target] = targetLocation{Kind: section.Kind, Title: section.Title, Location: section.Path}
		}
		for _, fragment := range section.Fragments {
			contentPath := fragment.Path + "/" + fragment.Entrypoint
			result[fragment.Target] = targetLocation{Kind: "fragment", Title: first(fragment.Title, fragment.ID), Location: fragment.Path, ContentPath: contentPath}
			for index := range fragment.Landmarks {
				landmark := &fragment.Landmarks[index]
				result[landmark.Target] = targetLocation{Kind: "landmark", Title: landmark.Label, Location: landmark.Path, ContentPath: contentPath}
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return result
}

func unownedReason(atom gitdiff.Atom) string {
	if atom.Kind == "event" && atom.Event == "add" {
		return "new file has no existing Saga owner"
	}
	if atom.Kind == "line" && atom.Side == "new" {
		return "addition is not adjacent to source evidence owned by the existing Saga"
	}
	return "changed source does not intersect evidence owned by the existing Saga"
}

func keys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool { return atomLess(changes[i].Atom, changes[j].Atom) })
}

func atomLess(left, right gitdiff.Atom) bool {
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	if left.Side != right.Side {
		return left.Side < right.Side
	}
	return left.URI < right.URI
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
