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
	Title       string
	ChangeCount int
	Files       []*attachedCodeFileView
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
}

func makeAttachedCodeView(title, target string, atoms []gitdiff.Atom, evidence []saga.DiffFile) *attachedCodeView {
	if len(atoms) == 0 {
		return nil
	}
	view := &attachedCodeView{Title: title, ChangeCount: len(atoms)}
	byPath := map[string]*attachedCodeFileView{}
	for _, atom := range atoms {
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

	for path, notes := range attachedFileNotes(atoms, evidence) {
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
	notes := map[string][]string{}
	type candidate struct {
		path      string
		reference diffuri.Reference
	}
	var buckets map[string][]candidate
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
				buckets = make(map[string][]candidate, len(atoms))
				for _, atom := range atoms {
					parsed, err := diffuri.Parse(atom.URI)
					if err != nil {
						continue
					}
					key := selectorBucket(parsed)
					buckets[key] = append(buckets[key], candidate{path: effectiveAtomPath(atom), reference: parsed})
				}
			}
			for _, entry := range buckets[selectorBucket(selector)] {
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
	if reference.Kind == "event" && reference.Event == "rename" {
		return "event\x00rename\x00" + reference.OldPath + "\x00" + reference.NewPath
	}
	return reference.Kind + "\x00" + reference.Path
}
