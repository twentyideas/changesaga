package coverage

import (
	"fmt"
	"sort"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

type Summary struct {
	Total       int `json:"total"`
	Covered     int `json:"covered"`
	Uncovered   int `json:"uncovered"`
	Overlapping int `json:"overlapping"`
	Orphaned    int `json:"orphaned"`
	SagaChanges int `json:"saga_changes"`
}

type Assignment struct {
	Target   string `json:"target"`
	DiffFile string `json:"diff_file"`
	Diff     int    `json:"diff"`
}

type Overlap struct {
	Atom      gitdiff.Atom `json:"atom"`
	CoveredBy []Assignment `json:"covered_by"`
}

type Orphan struct {
	Assignment Assignment         `json:"assignment"`
	Reference  saga.DiffReference `json:"reference"`
	Reason     string             `json:"reason"`
}

type TargetSummary struct {
	Target  string `json:"target"`
	Covered int    `json:"covered"`
}

// Report is a machine-consumed contract. Every collection field is always
// present and never null, so an agent can index into it without first testing
// for a missing key: a complete saga reports "uncovered": [] rather than
// omitting the field or emitting null.
type Report struct {
	Complete      bool                    `json:"complete"`
	CoverageScope string                  `json:"coverage_scope"`
	Summary       Summary                 `json:"summary"`
	Uncovered     []gitdiff.Atom          `json:"uncovered"`
	Overlaps      []Overlap               `json:"overlaps"`
	Orphans       []Orphan                `json:"orphans"`
	Targets       []TargetSummary         `json:"targets"`
	SagaChanges   []gitdiff.Atom          `json:"saga_changes"`
	Repository    string                  `json:"repository"`
	Base          string                  `json:"base"`
	Head          string                  `json:"head"`
	BaseOID       string                  `json:"base_oid"`
	HeadOID       string                  `json:"head_oid"`
	SchemaValid   bool                    `json:"schema_valid"`
	SchemaIssues  []saga.Issue            `json:"schema_issues"`
	Ownership     map[string][]Assignment `json:"-"`
}

type indexedAtom struct {
	index     int
	reference diffuri.Reference
}

type summaryAssignment struct {
	owners      int
	firstTarget string
}

type atomIndex struct {
	lines  map[string][]indexedAtom
	events []indexedAtom
	sorted map[string]bool
}

func Evaluate(document *saga.Saga, validation saga.Validation, changes gitdiff.ChangeSet) Report {
	report := newReport(validation, changes)
	assignments := make([][]Assignment, len(changes.Atoms))
	index := buildIndex(changes)
	walkDocumentDiffs(document, func(target string, files []saga.DiffFile) {
		visitDiffs(target, files, index, assignments, &report)
	})

	targetCounts := map[string]int{}
	for i, atom := range changes.Atoms {
		owners := assignments[i]
		if len(owners) > 0 {
			// The per-atom slice is complete and never mutated again, so transfer
			// its backing array into the report instead of copying every owner.
			report.Ownership[atom.Key] = owners
		}
		switch len(owners) {
		case 0:
			report.Uncovered = append(report.Uncovered, atom)
		case 1:
			targetCounts[owners[0].Target]++
		default:
			report.Overlaps = append(report.Overlaps, Overlap{Atom: atom, CoveredBy: owners})
			seen := map[string]bool{}
			for _, owner := range owners {
				if !seen[owner.Target] {
					targetCounts[owner.Target]++
					seen[owner.Target] = true
				}
			}
		}
	}
	finishReport(&report, targetCounts, len(changes.Atoms), len(report.Uncovered), len(report.Overlaps), len(changes.SagaChanges))
	return report
}

// SelectTarget returns the changed atoms matched by one narrative target's
// evidence. It reuses the coverage evaluator's indexed selector semantics but
// does not construct ownership for any sibling target or retain a whole-report
// graph. Lazy linked-code endpoints use it after reading only the source files
// named by that target.
func SelectTarget(files []saga.DiffFile, changes gitdiff.ChangeSet) []gitdiff.Atom {
	assignments := make([][]Assignment, len(changes.Atoms))
	report := Report{Orphans: []Orphan{}}
	visitDiffs("", files, buildIndex(changes), assignments, &report)
	matched := make([]gitdiff.Atom, 0)
	for index, owners := range assignments {
		if len(owners) > 0 {
			matched = append(matched, changes.Atoms[index])
		}
	}
	return matched
}

// EvaluateSummary computes coverage verdicts and per-target rollups without
// retaining atom-level ownership, uncovered, or overlap details. Overview-style
// queries use it because their bounded response needs counts, not the complete
// reverse indexes that gap, fragment, and diff-owner queries traverse.
func EvaluateSummary(document *saga.Saga, validation saga.Validation, changes gitdiff.ChangeSet) Report {
	report := newReport(validation, changes)
	assignments := make([]summaryAssignment, len(changes.Atoms))
	otherTargets := map[int][]string{}
	index := buildIndex(changes)
	walkDocumentDiffs(document, func(target string, files []saga.DiffFile) {
		visitDiffsSummary(target, files, index, assignments, otherTargets, &report)
	})

	targetCounts := map[string]int{}
	uncovered, overlapping := 0, 0
	for atomIndex, assignment := range assignments {
		if assignment.owners == 0 {
			uncovered++
			continue
		}
		if assignment.owners > 1 {
			overlapping++
		}
		targetCounts[assignment.firstTarget]++
		for _, target := range otherTargets[atomIndex] {
			targetCounts[target]++
		}
	}
	finishReport(&report, targetCounts, len(changes.Atoms), uncovered, overlapping, len(changes.SagaChanges))
	return report
}

func newReport(validation saga.Validation, changes gitdiff.ChangeSet) Report {
	return Report{
		CoverageScope: "mapping_only",
		Repository:    changes.Repository, Base: changes.Base, Head: changes.Head, BaseOID: changes.BaseOID, HeadOID: changes.HeadOID,
		SchemaValid: validation.Valid, SchemaIssues: nonNil(validation.Issues), SagaChanges: nonNil(changes.SagaChanges),
		Uncovered: []gitdiff.Atom{}, Overlaps: []Overlap{}, Orphans: []Orphan{}, Targets: []TargetSummary{},
		Ownership: make(map[string][]Assignment),
	}
}

func finishReport(report *Report, targetCounts map[string]int, total, uncovered, overlapping, sagaChanges int) {
	for target, count := range targetCounts {
		report.Targets = append(report.Targets, TargetSummary{Target: target, Covered: count})
	}
	sort.Slice(report.Targets, func(i, j int) bool { return report.Targets[i].Target < report.Targets[j].Target })
	report.Summary = Summary{
		Total: total, Covered: total - uncovered, Uncovered: uncovered,
		Overlapping: overlapping, Orphaned: len(report.Orphans), SagaChanges: sagaChanges,
	}
	report.Complete = report.SchemaValid && uncovered == 0 && len(report.Orphans) == 0
}

// nonNil keeps an empty collection encodable as [] instead of null. A nil Go
// slice and an empty one are indistinguishable in code but not in JSON, and
// consumers of the report branch on the JSON.
func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func visitDiffs(target string, files []saga.DiffFile, index atomIndex, assignments [][]Assignment, report *Report) {
	for _, file := range files {
		for i, reference := range file.Diffs {
			assignment := Assignment{Target: target, DiffFile: file.Path, Diff: i + 1}
			selector, err := diffuri.Parse(reference.URI)
			if err != nil {
				report.Orphans = append(report.Orphans, Orphan{Assignment: assignment, Reference: reference, Reason: err.Error()})
				continue
			}
			matched := 0
			candidates := index.events
			if selector.Kind == "line" {
				candidates = index.lineCandidates(selector)
			}
			for _, candidate := range candidates {
				if diffuri.Matches(selector, candidate.reference) {
					assignments[candidate.index] = append(assignments[candidate.index], assignment)
					matched++
				}
			}
			if matched == 0 {
				report.Orphans = append(report.Orphans, Orphan{Assignment: assignment, Reference: reference, Reason: "diff URI does not match the current source comparison"})
			}
		}
	}
}

func visitDiffsSummary(target string, files []saga.DiffFile, index atomIndex, assignments []summaryAssignment, otherTargets map[int][]string, report *Report) {
	for _, file := range files {
		for i, reference := range file.Diffs {
			assignment := Assignment{Target: target, DiffFile: file.Path, Diff: i + 1}
			selector, err := diffuri.Parse(reference.URI)
			if err != nil {
				report.Orphans = append(report.Orphans, Orphan{Assignment: assignment, Reference: reference, Reason: err.Error()})
				continue
			}
			matched := 0
			candidates := index.events
			if selector.Kind == "line" {
				candidates = index.lineCandidates(selector)
			}
			for _, candidate := range candidates {
				if diffuri.Matches(selector, candidate.reference) {
					addSummaryAssignment(assignments, otherTargets, candidate.index, target)
					matched++
				}
			}
			if matched == 0 {
				report.Orphans = append(report.Orphans, Orphan{Assignment: assignment, Reference: reference, Reason: "diff URI does not match the current source comparison"})
			}
		}
	}
}

func addSummaryAssignment(assignments []summaryAssignment, otherTargets map[int][]string, atomIndex int, target string) {
	assignment := &assignments[atomIndex]
	assignment.owners++
	if assignment.firstTarget == "" {
		assignment.firstTarget = target
		return
	}
	if assignment.firstTarget == target {
		return
	}
	for _, existing := range otherTargets[atomIndex] {
		if existing == target {
			return
		}
	}
	otherTargets[atomIndex] = append(otherTargets[atomIndex], target)
}

func buildIndex(changes gitdiff.ChangeSet) atomIndex {
	index := atomIndex{lines: map[string][]indexedAtom{}, sorted: map[string]bool{}}
	for atomIndex := range changes.Atoms {
		atom := &changes.Atoms[atomIndex]
		reference := diffuri.Reference{
			Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID,
			Kind: atom.Kind, Path: atom.Path, Side: atom.Side, Start: atom.Line, End: atom.Line,
			Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath,
		}
		if atom.Kind == "event" && atom.Event == "rename" {
			reference.Path = ""
		}
		// Hand-built ChangeSets used by package clients may omit the shared
		// comparison identity. Production gitdiff.Read results always carry it,
		// avoiding one long-URI parse and its allocations per atom.
		if reference.Repository == "" || reference.Base == "" || reference.Head == "" {
			var err error
			reference, err = diffuri.Parse(atom.URI)
			if err != nil {
				continue
			}
		}
		value := indexedAtom{index: atomIndex, reference: reference}
		if reference.Kind == "line" {
			key := lineIndexKey(reference)
			index.lines[key] = append(index.lines[key], value)
		} else {
			index.events = append(index.events, value)
		}
	}
	return index
}

func walkDocumentDiffs(document *saga.Saga, visit func(string, []saga.DiffFile)) {
	visit(document.Section.Target, document.Section.Diffs)
	walkSections(document.Section, func(section *saga.Section) {
		if section != document.Section {
			visit(section.Target, section.Diffs)
		}
		for _, fragment := range section.Fragments {
			visit(fragment.Target, fragment.Diffs)
			for landmarkIndex := range fragment.Landmarks {
				landmark := &fragment.Landmarks[landmarkIndex]
				visit(landmark.Target, landmark.Diffs)
			}
		}
	})
}

func (index atomIndex) lineCandidates(selector diffuri.Reference) []indexedAtom {
	key := lineIndexKey(selector)
	values := index.lines[key]
	if !index.sorted[key] {
		sort.SliceStable(values, func(i, j int) bool {
			return values[i].reference.Start < values[j].reference.Start
		})
		index.sorted[key] = true
	}
	start := sort.Search(len(values), func(i int) bool {
		return values[i].reference.Start >= selector.Start
	})
	end := sort.Search(len(values), func(i int) bool {
		return values[i].reference.Start > selector.End
	})
	return values[start:end]
}

func lineIndexKey(reference diffuri.Reference) string {
	return reference.Repository + "\x00" + reference.Base + "\x00" + reference.Head + "\x00" + reference.Path + "\x00" + reference.Side
}

func walkSections(section *saga.Section, fn func(*saga.Section)) {
	fn(section)
	for _, child := range section.Children {
		walkSections(child, fn)
	}
}

func DescribeAtom(atom gitdiff.Atom) string {
	if atom.Kind == "event" {
		if atom.Event == "rename" {
			return fmt.Sprintf("rename %s -> %s", atom.OldPath, atom.NewPath)
		}
		return fmt.Sprintf("%s %s", atom.Event, atom.Path)
	}
	return fmt.Sprintf("%s:%d (%s)", atom.Path, atom.Line, atom.Side)
}
