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
			for _, atom := range atoms {
				candidate, err := diffuri.Parse(atom.URI)
				if err != nil || !diffuri.Matches(selector, candidate) {
					continue
				}
				path := effectiveAtomPath(atom)
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
