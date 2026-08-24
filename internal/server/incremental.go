package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"

	"github.com/twentyideas/changesaga/internal/gitdiff"
)

const (
	defaultSurfacePageLimit = 50
	maxSurfacePageLimit     = 200
	defaultDiffPageLimit    = 50
	maxDiffPageLimit        = 200
)

type pageCursor struct {
	Version  int    `json:"v"`
	Key      string `json:"key"`
	Offset   int    `json:"offset"`
	Checksum string `json:"checksum"`
}

type pageWindow struct {
	start, end int
	total      int
	next       string
}

type fileDiffPageView struct {
	File       *FileDiffView
	NextCursor string
	HasMore    bool
	Returned   int
}

func makeFileDiffPage(current *reviewSnapshot, filePath, owner, linkedTarget string, manifest bool, window pageWindow) *FileDiffView {
	file := fileSummary(current, filePath)
	lineIndexes := current.fileLines[filePath]
	lines := make([]gitdiff.DisplayLine, 0, window.end-window.start)
	if len(lineIndexes) == 0 {
		for _, index := range current.fileAtoms[filePath][window.start:window.end] {
			atom := current.changes.Atoms[index]
			line := gitdiff.DisplayLine{Kind: atom.Side, Path: filePath, Content: atom.Content, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath, AtomKey: atom.Key}
			if atom.Kind == "event" {
				line.Kind = "event"
			} else if atom.Side == "old" {
				line.OldLine = atom.Line
			} else {
				line.NewLine = atom.Line
			}
			lines = append(lines, line)
		}
	} else {
		for _, index := range lineIndexes[window.start:window.end] {
			lines = append(lines, current.changes.DisplayLines[index])
		}
	}
	needed := map[string]bool{}
	for _, line := range lines {
		if index, ok := current.atomByKey[line.AtomKey]; ok {
			needed[current.changes.Atoms[index].URI] = true
		}
	}
	threads := map[string][]*threadView{}
	if !manifest {
		for _, thread := range current.document.Threads {
			if thread.State == "withdrawn" || thread.Anchor.Type != "diff" || thread.Anchor.Diff == nil || !needed[thread.Anchor.Diff.URI] {
				continue
			}
			threads[thread.Anchor.Diff.URI] = append(threads[thread.Anchor.Diff.URI], makeThreadView(thread))
		}
	}
	for _, line := range lines {
		view := &DiffLineView{Kind: line.Kind, Path: filePath, OldLine: line.OldLine, NewLine: line.NewLine, Content: line.Content, Event: line.Event, OldPath: line.OldPath, NewPath: line.NewPath}
		if index, ok := current.atomByKey[line.AtomKey]; ok {
			atom := &current.changes.Atoms[index]
			view.Atom = &diffAtomView{Atom: *atom, Threads: threads[atom.URI], Target: owner}
			if linkedTarget != "" {
				for _, assignment := range current.report.Ownership[atom.Key] {
					if assignment.Target == linkedTarget {
						view.Linked = true
						break
					}
				}
			}
		}
		file.Lines = append(file.Lines, view)
	}
	return file
}

func (p pageWindow) hasMore() bool { return p.end < p.total }

func pageRequest(r *http.Request, key string, total, defaultLimit, maxLimit int) (pageWindow, error) {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return pageWindow{}, fmt.Errorf("limit must be between 1 and %d", maxLimit)
		}
		if value > maxLimit {
			value = maxLimit
		}
		limit = value
	}
	start := 0
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		data, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return pageWindow{}, fmt.Errorf("cursor is invalid")
		}
		var cursor pageCursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Key != key || cursor.Offset < 0 ||
			subtle.ConstantTimeCompare([]byte(cursor.Checksum), []byte(pageCursorChecksum(cursor))) != 1 {
			return pageWindow{}, fmt.Errorf("cursor does not apply to this request")
		}
		start = cursor.Offset
	}
	if start > total {
		return pageWindow{}, fmt.Errorf("cursor exceeds the available results")
	}
	end := start + limit
	if end > total {
		end = total
	}
	window := pageWindow{start: start, end: end, total: total}
	if end < total {
		cursor := pageCursor{Version: 1, Key: key, Offset: end}
		cursor.Checksum = pageCursorChecksum(cursor)
		encoded, _ := json.Marshal(cursor)
		window.next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return window, nil
}

func pageCursorChecksum(cursor pageCursor) string {
	cursor.Checksum = ""
	data, _ := json.Marshal(cursor)
	digest := sha256.Sum256(append([]byte("change-saga-http-page-v1\x00"), data...))
	return hex.EncodeToString(digest[:8])
}

func writePageHeaders(w http.ResponseWriter, window pageWindow) {
	w.Header().Set("X-Change-Saga-Total", strconv.Itoa(window.total))
	w.Header().Set("X-Change-Saga-Returned", strconv.Itoa(window.end-window.start))
	w.Header().Set("X-Change-Saga-Has-More", strconv.FormatBool(window.hasMore()))
	if window.next != "" {
		w.Header().Set("X-Change-Saga-Next-Cursor", window.next)
	}
}

type codePageView struct {
	Tree          ChangedFileTreeView
	Selected      *FileDiffView
	Owners        []*ManifestOwnerView
	RelatedEmpty  string
	TotalFiles    int
	ReviewedFiles int
	NextCursor    string
	HasMore       bool
	Returned      int
}

func fileSummary(current *reviewSnapshot, filePath string) *FileDiffView {
	file := current.fileSummaries[filePath]
	file.Name, file.Href = path.Base(filePath), CodeDiffURL(filePath, "")
	return &file
}

func (a *app) codePage(w http.ResponseWriter, r *http.Request) {
	document := a.sourceReviewDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	catalog, err := a.sourceCatalog(r.Context(), document.Manifest)
	if err != nil {
		http.Error(w, "The source comparison could not be loaded.", http.StatusInternalServerError)
		return
	}
	selectedPath, selectionErr := selectedCatalogPath(catalog, r)
	if selectionErr != nil {
		http.Error(w, selectionErr.Error(), http.StatusBadRequest)
		return
	}
	total := len(catalog.Files)
	identity := sourceCatalogIdentity(catalog)
	window, err := pageRequest(r, "code\x00"+identity+"\x00"+r.URL.Query().Get("file")+"\x00"+r.URL.Query().Get("diff"), total, defaultSurfacePageLimit, maxSurfacePageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reviews, reviewedFiles := latestCatalogReviews(document, catalog)
	fileStart, fileEnd := boundedSlice(window.start, window.end, len(catalog.Files))
	files := make([]*FileDiffView, 0, fileEnd-fileStart)
	for _, summary := range catalog.Files[fileStart:fileEnd] {
		file := catalogFileView(catalog, summary, reviews[summary.Path])
		file.Selected = summary.Path == selectedPath
		files = append(files, file)
	}
	var selected *FileDiffView
	if selectedPath != "" {
		index := sort.Search(len(catalog.Files), func(index int) bool { return catalog.Files[index].Path >= selectedPath })
		if index < len(catalog.Files) && catalog.Files[index].Path == selectedPath {
			selected = catalogFileView(catalog, catalog.Files[index], reviews[selectedPath])
			selected.Selected = true
		}
	}
	result := codePageView{
		Tree: makeChangedFileTree(files), Selected: selected,
		RelatedEmpty: "Loading explanations…",
		TotalFiles:   len(catalog.Files), ReviewedFiles: reviewedFiles, NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start,
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	if err := a.template.ExecuteTemplate(w, "code-page", result); err != nil {
		http.Error(w, "The code page could not be rendered.", http.StatusInternalServerError)
	}
}

type fileOwnersView struct {
	Owners       []*ManifestOwnerView
	RelatedEmpty string
}

func (a *app) fileOwners(w http.ResponseWriter, r *http.Request) {
	document := a.narrativeDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	catalog, err := a.sourceCatalog(r.Context(), document.Manifest)
	if err != nil {
		http.Error(w, "The source comparison could not be loaded.", http.StatusInternalServerError)
		return
	}
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	owners, err := a.catalogFileNarrativeOwners(document, catalog, filePath)
	if err != nil {
		http.Error(w, "The explanations for this file could not be loaded.", http.StatusInternalServerError)
		return
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "file-owners", fileOwnersView{Owners: owners, RelatedEmpty: "Nothing in the story explains this file yet."}); err != nil {
		http.Error(w, "The explanations for this file could not be rendered.", http.StatusInternalServerError)
	}
}

type coveragePageView struct {
	Mode       string
	Summary    coverageTotalsView
	Code       []coverageCodeItem
	Saga       []coverageSagaItem
	Orphans    []*ManifestOrphanView
	NextCursor string
	HasMore    bool
	Returned   int
}

type coverageCodeItem struct {
	File *ManifestFileView
}

type coverageSagaItem struct {
	Target    *ManifestOwnerView
	AtomCount int
	FileCount int
}

type coverageFilePageView struct {
	File       string
	Chunks     []*ManifestChunkView
	NextCursor string
	HasMore    bool
	Returned   int
}

type coverageTargetPageView struct {
	Target     string
	Files      []*ManifestTargetFileView
	NextCursor string
	HasMore    bool
	Returned   int
}

func (a *app) coveragePage(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "code"
	}
	if mode != "code" && mode != "saga" {
		http.Error(w, "mode must be code or saga", http.StatusBadRequest)
		return
	}
	current := a.requestSnapshot(w, r)
	if current == nil {
		return
	}
	if current.diffErr != nil {
		http.Error(w, "The source comparison could not be loaded.", http.StatusInternalServerError)
		return
	}
	total := len(current.fileOrder)
	if mode == "saga" {
		total = len(current.targetOrder) + len(current.report.Orphans)
	}
	window, err := pageRequest(r, "coverage\x00"+current.identity+"\x00"+mode, total, defaultSurfacePageLimit, maxSurfacePageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := coveragePageView{Mode: mode, Summary: coverageTotalsView{
		Files: len(current.fileOrder), Total: current.report.Summary.Total, Covered: current.report.Summary.Covered,
		Uncovered: current.report.Summary.Uncovered, Overlapping: current.report.Summary.Overlapping,
		Orphaned: current.report.Summary.Orphaned, Complete: current.report.Complete,
	}, NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start}
	locations := current.locations
	if mode == "code" {
		start, end := boundedSlice(window.start, window.end, len(current.fileOrder))
		for _, path := range current.fileOrder[start:end] {
			fileValue := current.fileCoverage[path]
			file := &fileValue
			result.Code = append(result.Code, coverageCodeItem{File: file})
		}
	} else {
		targetTotal := len(current.targetOrder)
		targetStart, targetEnd := boundedSlice(window.start, window.end, targetTotal)
		for _, target := range current.targetOrder[targetStart:targetEnd] {
			result.Saga = append(result.Saga, coverageSagaItem{
				Target: manifestOwner(target, locations), AtomCount: len(current.targetAtoms[target]), FileCount: len(current.targetFiles[target]),
			})
		}
		orphanStart := window.start - targetTotal
		if orphanStart < 0 {
			orphanStart = 0
		}
		orphanEnd := window.end - targetTotal
		if orphanEnd > len(current.report.Orphans) {
			orphanEnd = len(current.report.Orphans)
		}
		for index := orphanStart; index < orphanEnd; index++ {
			orphan := current.report.Orphans[index]
			result.Orphans = append(result.Orphans, &ManifestOrphanView{Owner: manifestOwner(orphan.Assignment.Target, locations), URI: orphan.Reference.URI, Reason: orphan.Reason})
		}
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	if err := a.template.ExecuteTemplate(w, "coverage-page", result); err != nil {
		http.Error(w, "The coverage page could not be rendered.", http.StatusInternalServerError)
	}
}

func (a *app) coverageFilePage(w http.ResponseWriter, r *http.Request) {
	current := a.requestSnapshot(w, r)
	if current == nil {
		return
	}
	filePath := r.URL.Query().Get("file")
	indexes, ok := current.fileAtoms[filePath]
	if !ok {
		http.Error(w, "changed file not found", http.StatusNotFound)
		return
	}
	window, err := pageRequest(r, "coverage-file\x00"+current.identity+"\x00"+filePath, len(indexes), maxSurfacePageLimit, maxSurfacePageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := coverageFilePageView{
		File: filePath, Chunks: makeManifestChunks(atomsForIndexes(current, indexes[window.start:window.end]), current.report.Ownership, current.locations, true),
		NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start,
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	if err := a.template.ExecuteTemplate(w, "coverage-file-page", result); err != nil {
		http.Error(w, "The file coverage could not be rendered.", http.StatusInternalServerError)
	}
}

func (a *app) coverageTargetPage(w http.ResponseWriter, r *http.Request) {
	current := a.requestSnapshot(w, r)
	if current == nil {
		return
	}
	target := r.URL.Query().Get("target")
	paths, ok := current.targetFiles[target]
	if !ok {
		http.Error(w, "coverage target not found", http.StatusNotFound)
		return
	}
	window, err := pageRequest(r, "coverage-target\x00"+current.identity+"\x00"+target, len(paths), defaultSurfacePageLimit, maxSurfacePageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	start, end := boundedSlice(window.start, window.end, len(paths))
	result := coverageTargetPageView{Target: target, NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start}
	for _, filePath := range paths[start:end] {
		atoms := atomsForIndexes(current, current.targetFileAtoms[target][filePath])
		file := &ManifestTargetFileView{Path: filePath, Href: CodeDiffURL(filePath, ""), HasDiff: true, Chunks: makeManifestChunks(atoms, nil, current.locations, false)}
		for _, atom := range atoms {
			file.AtomCount++
			switch {
			case atom.Kind == "event":
				file.Events++
			case atom.Side == "old":
				file.Deleted++
			default:
				file.Added++
			}
		}
		result.Files = append(result.Files, file)
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	if err := a.template.ExecuteTemplate(w, "coverage-target-page", result); err != nil {
		http.Error(w, "The target coverage could not be rendered.", http.StatusInternalServerError)
	}
}

func atomsForIndexes(current *reviewSnapshot, indexes []int) []gitdiff.Atom {
	atoms := make([]gitdiff.Atom, 0, len(indexes))
	for _, index := range indexes {
		atoms = append(atoms, current.changes.Atoms[index])
	}
	return atoms
}

func boundedSlice(start, end, length int) (int, int) {
	if start > length {
		start = length
	}
	if end > length {
		end = length
	}
	return start, end
}
