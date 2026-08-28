package server

import (
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

// attachedCodeView is the narrative-first projection of a target's evidence.
// It preserves exact diff atoms while presenting the file as the first unit a
// reviewer chooses to expand.
type attachedCodeView struct {
	Title           string
	ChangeCount     int
	LineCount       int
	EventCount      int
	LinkedLineCount int
	LinkedEvents    int
	Files           []*attachedCodeFileView
}

type attachedCodeFileView struct {
	Path           string
	Href           string
	Target         string
	Summary        string
	MissingSummary bool
	Added          int
	Deleted        int
	Events         int
	Changes        int
	LinkedLines    int
	LinkedEvents   int
}

func makeAttachedCodeView(title, target string, linked, full []gitdiff.Atom, evidence []saga.DiffFile) *attachedCodeView {
	return makeAttachedCodeViewFromAtoms(
		title, target,
		len(linked), func(index int) gitdiff.Atom { return linked[index] },
		len(full), func(index int) gitdiff.Atom { return full[index] },
		evidence,
	)
}

func makeAttachedCodeViewIndexed(title, target string, snapshot *reviewSnapshot, indexes []int, evidence []saga.DiffFile) *attachedCodeView {
	fullIndexes := make([]int, 0)
	for _, path := range snapshot.targetFiles[target] {
		fullIndexes = append(fullIndexes, snapshot.fileAtoms[path]...)
	}
	return makeAttachedCodeViewFromAtoms(
		title, target,
		len(indexes), func(index int) gitdiff.Atom { return snapshot.changes.Atoms[indexes[index]] },
		len(fullIndexes), func(index int) gitdiff.Atom { return snapshot.changes.Atoms[fullIndexes[index]] },
		evidence,
	)
}

func makeAttachedCodeViewFromAtoms(title, target string, linkedCount int, linkedAt func(int) gitdiff.Atom, fullCount int, fullAt func(int) gitdiff.Atom, evidence []saga.DiffFile) *attachedCodeView {
	if linkedCount == 0 {
		return nil
	}
	view := &attachedCodeView{Title: title}
	byPath := map[string]*attachedCodeFileView{}
	for index := 0; index < fullCount; index++ {
		atom := fullAt(index)
		path := effectiveAtomPath(atom)
		file := byPath[path]
		if file == nil {
			file = &attachedCodeFileView{Path: path, Href: CodeDiffURL(path, ""), Target: target}
			byPath[path] = file
			view.Files = append(view.Files, file)
		}
		file.Changes++
		switch {
		case atom.Kind == "event":
			file.Events++
		case atom.Side == "old":
			file.Deleted++
		default:
			file.Added++
		}
	}
	for index := 0; index < linkedCount; index++ {
		atom := linkedAt(index)
		file := byPath[effectiveAtomPath(atom)]
		if file == nil {
			continue
		}
		if atom.Kind == "event" {
			file.LinkedEvents++
			view.LinkedEvents++
		} else {
			file.LinkedLines++
			view.LinkedLineCount++
		}
	}
	for _, file := range view.Files {
		view.LineCount += file.Added + file.Deleted
		view.EventCount += file.Events
	}
	view.ChangeCount = view.LineCount
	if view.ChangeCount == 0 {
		view.ChangeCount = view.EventCount
	}

	for path, notes := range attachedFileNotesFromAtoms(linkedCount, linkedAt, evidence) {
		if file := byPath[path]; file != nil {
			file.Summary = strings.Join(notes, " ")
		}
	}
	for _, file := range view.Files {
		if file.Summary == "" {
			file.Summary = "No change summary was authored for this file."
			file.MissingSummary = true
		}
	}
	sort.SliceStable(view.Files, func(i, j int) bool { return view.Files[i].Path < view.Files[j].Path })
	return view
}

// attachedFileNotes collects the authored notes that apply to each changed
// file. A reference selects atoms only when it agrees on kind and path, so
// candidates are bucketed by that pair and a selector is compared against its
// own bucket. Comparing every reference against every atom instead made the
// page quadratic in the size of a well-covered target: the codebase saga has
// one exact reference per changed line, so the two loops grew together.
func attachedFileNotes(atoms []gitdiff.Atom, evidence []saga.DiffFile) map[string][]string {
	return attachedFileNotesFromAtoms(len(atoms), func(index int) gitdiff.Atom { return atoms[index] }, evidence)
}

func attachedFileNotesFromAtoms(atomCount int, atomAt func(int) gitdiff.Atom, evidence []saga.DiffFile) map[string][]string {
	notes := map[string][]string{}
	type candidate struct {
		path      string
		reference diffuri.Reference
	}
	var buckets map[string][]candidate
	prepared := false
	for _, diffFile := range evidence {
		for _, reference := range diffFile.Diffs {
			note := strings.TrimSpace(reference.Note)
			if note == "" {
				continue
			}
			selector, err := diffuri.Parse(reference.URI)
			if err != nil {
				continue
			}
			// Evidence without authored notes keeps its zero-parse fast path:
			// the index is only built once a note actually needs matching.
			if buckets == nil {
				buckets = make(map[string][]candidate, atomCount)
				for index := 0; index < atomCount; index++ {
					atom := atomAt(index)
					parsed, err := diffuri.Parse(atom.URI)
					if err != nil {
						continue
					}
					key := selectorBucket(parsed)
					buckets[key] = append(buckets[key], candidate{path: effectiveAtomPath(atom), reference: parsed})
				}
			}
			if !prepared {
				for key := range buckets {
					if strings.HasPrefix(key, "line\x00") {
						sort.SliceStable(buckets[key], func(left, right int) bool {
							return buckets[key][left].reference.Start < buckets[key][right].reference.Start
						})
					}
				}
				prepared = true
			}
			candidates := buckets[selectorBucket(selector)]
			if selector.Kind == "line" {
				start := sort.Search(len(candidates), func(index int) bool {
					return candidates[index].reference.Start >= selector.Start
				})
				end := sort.Search(len(candidates), func(index int) bool {
					return candidates[index].reference.Start > selector.End
				})
				candidates = candidates[start:end]
			}
			for _, entry := range candidates {
				if !diffuri.Matches(selector, entry.reference) {
					continue
				}
				if !contains(notes[entry.path], note) {
					notes[entry.path] = append(notes[entry.path], note)
				}
			}
		}
	}
	return notes
}

// selectorBucket is the coarsest key on which diffuri.Matches can still
// succeed. Two references that disagree on it can never match, and every
// reference that agrees on it is still compared exactly.
func selectorBucket(reference diffuri.Reference) string {
	if reference.Kind == "line" {
		return "line\x00" + reference.Path + "\x00" + reference.Side
	}
	if reference.Kind == "event" && reference.Event == "rename" {
		return "event\x00rename\x00" + reference.OldPath + "\x00" + reference.NewPath
	}
	return reference.Kind + "\x00" + reference.Path
}
