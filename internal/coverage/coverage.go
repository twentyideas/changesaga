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
	Complete     bool                    `json:"complete"`
	Summary      Summary                 `json:"summary"`
	Uncovered    []gitdiff.Atom          `json:"uncovered"`
	Overlaps     []Overlap               `json:"overlaps"`
	Orphans      []Orphan                `json:"orphans"`
	Targets      []TargetSummary         `json:"targets"`
	SagaChanges  []gitdiff.Atom          `json:"saga_changes"`
	Repository   string                  `json:"repository"`
	Base         string                  `json:"base"`
	Head         string                  `json:"head"`
	BaseOID      string                  `json:"base_oid"`
	HeadOID      string                  `json:"head_oid"`
	SchemaValid  bool                    `json:"schema_valid"`
	SchemaIssues []saga.Issue            `json:"schema_issues"`
	Ownership    map[string][]Assignment `json:"-"`
}

type indexedAtom struct {
	atom      gitdiff.Atom
	reference diffuri.Reference
}

type atomIndex struct {
	lines  map[string][]indexedAtom
	events []indexedAtom
	sorted map[string]bool
}

func Evaluate(document *saga.Saga, validation saga.Validation, changes gitdiff.ChangeSet) Report {
	report := Report{
		Repository: changes.Repository, Base: changes.Base, Head: changes.Head, BaseOID: changes.BaseOID, HeadOID: changes.HeadOID,
		SchemaValid: validation.Valid, SchemaIssues: nonNil(validation.Issues), SagaChanges: nonNil(changes.SagaChanges),
		Uncovered: []gitdiff.Atom{}, Overlaps: []Overlap{}, Orphans: []Orphan{}, Targets: []TargetSummary{},
		Ownership: make(map[string][]Assignment),
	}
	assignments := make(map[string][]Assignment, len(changes.Atoms))
	index := buildIndex(changes.Atoms)

	visitDiffs(document.Section.Target, document.Section.Diffs, index, assignments, &report)
	walkSections(document.Section, func(section *saga.Section) {
		if section != document.Section {
			visitDiffs(section.Target, section.Diffs, index, assignments, &report)
		}
		for _, fragment := range section.Fragments {
			visitDiffs(fragment.Target, fragment.Diffs, index, assignments, &report)
			for landmarkIndex := range fragment.Landmarks {
				landmark := &fragment.Landmarks[landmarkIndex]
				visitDiffs(landmark.Target, landmark.Diffs, index, assignments, &report)
			}
		}
	})

	targetCounts := map[string]int{}
	for _, atom := range changes.Atoms {
		owners := assignments[atom.Key]
		if len(owners) > 0 {
			report.Ownership[atom.Key] = append([]Assignment(nil), owners...)
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
	for target, count := range targetCounts {
		report.Targets = append(report.Targets, TargetSummary{Target: target, Covered: count})
	}
	sort.Slice(report.Targets, func(i, j int) bool { return report.Targets[i].Target < report.Targets[j].Target })
	report.Summary = Summary{
		Total: len(changes.Atoms), Covered: len(changes.Atoms) - len(report.Uncovered), Uncovered: len(report.Uncovered),
		Overlapping: len(report.Overlaps), Orphaned: len(report.Orphans), SagaChanges: len(changes.SagaChanges),
	}
	report.Complete = validation.Valid && len(report.Uncovered) == 0 && len(report.Orphans) == 0
	return report
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

func visitDiffs(target string, files []saga.DiffFile, index atomIndex, assignments map[string][]Assignment, report *Report) {
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
					assignments[candidate.atom.Key] = append(assignments[candidate.atom.Key], assignment)
					matched++
				}
			}
			if matched == 0 {
				report.Orphans = append(report.Orphans, Orphan{Assignment: assignment, Reference: reference, Reason: "diff URI does not match the current source comparison"})
			}
		}
	}
}

func buildIndex(atoms []gitdiff.Atom) atomIndex {
	index := atomIndex{lines: map[string][]indexedAtom{}, sorted: map[string]bool{}}
	for _, atom := range atoms {
		reference, err := diffuri.Parse(atom.URI)
		if err != nil {
			continue
		}
		value := indexedAtom{atom: atom, reference: reference}
		if reference.Kind == "line" {
			key := lineIndexKey(reference)
			index.lines[key] = append(index.lines[key], value)
		} else {
			index.events = append(index.events, value)
		}
	}
	return index
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
