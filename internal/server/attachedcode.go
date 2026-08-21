package server

import (
	"sort"
	"strings"

	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/saga"
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
	Summary        string
	MissingSummary bool
	Added          int
	Deleted        int
	Events         int
	Changes        []*diffAtomView
}

func makeAttachedCodeView(title, target string, atoms []gitdiff.Atom, evidence []saga.DiffFile, threads map[string][]*threadView) *attachedCodeView {
	if len(atoms) == 0 {
		return nil
	}
	view := &attachedCodeView{Title: title, ChangeCount: len(atoms)}
	byPath := map[string]*attachedCodeFileView{}
	for _, atom := range atoms {
		path := effectiveAtomPath(atom)
		file := byPath[path]
		if file == nil {
			file = &attachedCodeFileView{Path: path, Href: CodeDiffURL(path, "")}
			byPath[path] = file
			view.Files = append(view.Files, file)
		}
		file.Changes = append(file.Changes, &diffAtomView{Atom: atom, Threads: threads[atom.URI], Target: target})
		switch {
		case atom.Kind == "event":
			file.Events++
		case atom.Side == "old":
			file.Deleted++
		default:
			file.Added++
		}
	}

	notes := map[string][]string{}
	type noteCandidate struct {
		atom      gitdiff.Atom
		reference diffuri.Reference
	}
	var candidates []noteCandidate
	candidatesReady := false
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
			// Large targets commonly contain one exact reference per changed
			// atom. Parse candidates once, but keep the index lazy so evidence
			// without authored notes retains its zero-parse fast path.
			if !candidatesReady {
				candidates = make([]noteCandidate, 0, len(atoms))
				for _, atom := range atoms {
					candidate, err := diffuri.Parse(atom.URI)
					if err == nil {
						candidates = append(candidates, noteCandidate{atom: atom, reference: candidate})
					}
				}
				candidatesReady = true
			}
			for _, candidate := range candidates {
				if !diffuri.Matches(selector, candidate.reference) {
					continue
				}
				path := effectiveAtomPath(candidate.atom)
				if !contains(notes[path], note) {
					notes[path] = append(notes[path], note)
				}
			}
		}
	}
	for _, file := range view.Files {
		file.Summary = strings.Join(notes[file.Path], " ")
		if file.Summary == "" {
			file.Summary = "No change summary was authored for this file."
			file.MissingSummary = true
		}
	}
	sort.SliceStable(view.Files, func(i, j int) bool { return view.Files[i].Path < view.Files[j].Path })
	return view
}
