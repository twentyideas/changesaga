// Package equiv compares two coverage evaluations for semantic equality.
//
// The comparison is deliberately about meaning rather than about bytes. Two
// encodings of the same evidence do not have to produce the same evidence file
// paths or the same number of selectors; they do have to produce the same
// owners for every atom, the same note on every atom, the same uncovered set,
// the same overlaps, and the same stale references. Anything weaker would let a
// compaction quietly change what a reviewer is told.
package equiv

import (
	"fmt"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

// Fact is the meaning of one atom: who explains it and with what note.
type Fact struct {
	Atom    string
	Owners  []string
	Notes   []string
	Overlap bool
}

// Facts projects a coverage report onto per-atom meaning. Evidence file paths
// and selector spellings are deliberately not part of the projection.
func Facts(document *saga.Saga, report coverage.Report) map[string]Fact {
	notes := noteIndex(document)
	facts := make(map[string]Fact, len(report.Ownership))
	for key, assignments := range report.Ownership {
		fact := Fact{Atom: key, Overlap: len(assignments) > 1}
		seenOwner := map[string]bool{}
		seenNote := map[string]bool{}
		for _, assignment := range assignments {
			if !seenOwner[assignment.Target] {
				seenOwner[assignment.Target] = true
				fact.Owners = append(fact.Owners, assignment.Target)
			}
			note := notes[noteKey(assignment)]
			if note != "" && !seenNote[assignment.Target+"\x00"+note] {
				seenNote[assignment.Target+"\x00"+note] = true
				fact.Notes = append(fact.Notes, assignment.Target+" :: "+note)
			}
		}
		sort.Strings(fact.Owners)
		sort.Strings(fact.Notes)
		facts[key] = fact
	}
	return facts
}

func noteKey(assignment coverage.Assignment) string {
	return fmt.Sprintf("%s#%d", assignment.DiffFile, assignment.Diff)
}

func noteIndex(document *saga.Saga) map[string]string {
	notes := map[string]string{}
	record := func(files []saga.DiffFile) {
		for _, file := range files {
			for i, reference := range file.Diffs {
				notes[fmt.Sprintf("%s#%d", file.Path, i+1)] = reference.Note
			}
		}
	}
	var visit func(*saga.Section)
	visit = func(section *saga.Section) {
		record(section.Diffs)
		for _, fragment := range section.Fragments {
			record(fragment.Diffs)
			for i := range fragment.Landmarks {
				record(fragment.Landmarks[i].Diffs)
			}
		}
		for _, child := range section.Children {
			visit(child)
		}
	}
	visit(document.Section)
	return notes
}

// Difference describes one way two evaluations disagree.
type Difference struct {
	Kind  string
	Atom  string
	Left  string
	Right string
}

func (d Difference) String() string {
	if d.Atom == "" {
		return fmt.Sprintf("%s: %s != %s", d.Kind, d.Left, d.Right)
	}
	return fmt.Sprintf("%s at %s: %s != %s", d.Kind, d.Atom, d.Left, d.Right)
}

// Compare reports every semantic difference between two evaluations. An empty
// result means the two encodings say the same thing about every atom.
func Compare(leftDocument *saga.Saga, left coverage.Report, rightDocument *saga.Saga, right coverage.Report) []Difference {
	var differences []Difference
	add := func(kind, atom, l, r string) {
		if l != r {
			differences = append(differences, Difference{Kind: kind, Atom: atom, Left: l, Right: r})
		}
	}
	add("summary.total", "", itoa(left.Summary.Total), itoa(right.Summary.Total))
	add("summary.covered", "", itoa(left.Summary.Covered), itoa(right.Summary.Covered))
	add("summary.uncovered", "", itoa(left.Summary.Uncovered), itoa(right.Summary.Uncovered))
	add("summary.overlapping", "", itoa(left.Summary.Overlapping), itoa(right.Summary.Overlapping))
	add("summary.orphaned", "", itoa(left.Summary.Orphaned), itoa(right.Summary.Orphaned))
	add("summary.saga_changes", "", itoa(left.Summary.SagaChanges), itoa(right.Summary.SagaChanges))
	add("complete", "", fmt.Sprint(left.Complete), fmt.Sprint(right.Complete))
	add("base_oid", "", left.BaseOID, right.BaseOID)
	add("head_oid", "", left.HeadOID, right.HeadOID)
	add("repository", "", left.Repository, right.Repository)

	add("uncovered", "", atomKeys(left.Uncovered), atomKeys(right.Uncovered))
	add("saga_changes", "", atomKeys(left.SagaChanges), atomKeys(right.SagaChanges))
	add("targets", "", targetKeys(left), targetKeys(right))

	leftFacts := Facts(leftDocument, left)
	rightFacts := Facts(rightDocument, right)
	keys := map[string]bool{}
	for key := range leftFacts {
		keys[key] = true
	}
	for key := range rightFacts {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		l, okLeft := leftFacts[key]
		r, okRight := rightFacts[key]
		if okLeft != okRight {
			differences = append(differences, Difference{Kind: "atom presence", Atom: key,
				Left: fmt.Sprint(okLeft), Right: fmt.Sprint(okRight)})
			continue
		}
		add("owners", key, strings.Join(l.Owners, ","), strings.Join(r.Owners, ","))
		add("notes", key, strings.Join(l.Notes, " | "), strings.Join(r.Notes, " | "))
		add("overlap", key, fmt.Sprint(l.Overlap), fmt.Sprint(r.Overlap))
	}
	return differences
}

func itoa(value int) string { return fmt.Sprint(value) }

func atomKeys(atoms []gitdiff.Atom) string {
	keys := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		keys = append(keys, atom.Key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func targetKeys(report coverage.Report) string {
	entries := make([]string, 0, len(report.Targets))
	for _, target := range report.Targets {
		entries = append(entries, fmt.Sprintf("%s=%d", target.Target, target.Covered))
	}
	sort.Strings(entries)
	return strings.Join(entries, ",")
}
