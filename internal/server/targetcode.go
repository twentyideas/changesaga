package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

type targetCodeView struct {
	DOMID       string
	Target      string
	Title       string
	ChangeCount int
	Attached    *attachedCodeView
}

type targetSelection struct {
	catalog  gitdiff.Catalog
	evidence []saga.DiffFile
	changes  gitdiff.ChangeSet
	matched  []gitdiff.Atom
}

// evidenceOwnerCache is a compact reverse index over authored diff-reference
// metadata. It stores target IDs by named path, never source atoms, rendered
// diff rows, or the coverage graph.
type evidenceOwnerCache struct {
	mutex  sync.Mutex
	source string
	byPath map[string]map[string]bool
	ready  bool
	builds int
}

// targetCode resolves one narrative target into a linked-code summary. The
// request reads only that target's evidence and the source files named by it;
// it never constructs the whole coverage graph.
func (a *app) targetCode(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	document := a.narrativeDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	title, ok := narrativeTargetTitle(document.Section, target)
	if !ok {
		http.Error(w, "unknown narrative target", http.StatusBadRequest)
		return
	}
	selection, err := a.selectTargetCode(r.Context(), document, target, "")
	if err != nil {
		http.Error(w, "Linked code could not be loaded.", http.StatusInternalServerError)
		return
	}
	attached := makeAttachedCodeView(title, target, selection.matched, selection.evidence)
	view := targetCodeView{DOMID: domID(target), Target: target, Title: title, ChangeCount: len(selection.matched), Attached: attached}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "target-code", view); err != nil {
		http.Error(w, "Linked code could not be rendered.", http.StatusInternalServerError)
	}
}

// selectTargetCode is the shared bounded mapping path for the summary and one
// expanded linked file. filePath limits source reading to a single catalog
// entry; an empty path reads each changed file named by this target.
func (a *app) selectTargetCode(ctx context.Context, document *saga.Saga, target, filePath string) (targetSelection, error) {
	index := saga.MutationIndexFromDocument(document)
	evidence, validation, err := saga.LoadTargetDiffs(index, target)
	if err != nil {
		return targetSelection{}, err
	}
	if !validation.Valid {
		return targetSelection{}, fmt.Errorf("target evidence is invalid")
	}
	catalog, err := a.sourceCatalog(ctx, document.Manifest)
	if err != nil {
		return targetSelection{}, err
	}
	selected := catalogFilesForEvidence(catalog, evidence)
	if filePath != "" {
		file, ok := catalogFile(catalog, filePath)
		if !ok {
			return targetSelection{}, fmt.Errorf("changed file not found")
		}
		selected = nil
		for _, candidate := range catalogFilesForEvidence(catalog, evidence) {
			if candidate.Path == file.Path {
				selected = append(selected, candidate)
				break
			}
		}
	}
	changes := gitdiff.ChangeSet{
		Repository: catalog.Repository, Base: catalog.Base, Head: catalog.Head,
		BaseOID: catalog.BaseOID, HeadOID: catalog.HeadOID,
	}
	for _, file := range selected {
		part, readErr := gitdiff.ReadFile(ctx, a.sourceDir, catalog, file)
		if readErr != nil {
			return targetSelection{}, readErr
		}
		changes.Atoms = append(changes.Atoms, part.Atoms...)
		changes.SagaChanges = append(changes.SagaChanges, part.SagaChanges...)
		changes.DisplayLines = append(changes.DisplayLines, part.DisplayLines...)
	}
	return targetSelection{
		catalog: catalog, evidence: evidence, changes: changes,
		matched: coverage.SelectTarget(evidence, changes),
	}, nil
}

func catalogFilesForEvidence(catalog gitdiff.Catalog, evidence []saga.DiffFile) []gitdiff.FileSummary {
	paths := map[string]bool{}
	for _, file := range evidence {
		for _, value := range file.Diffs {
			reference, err := diffuri.Parse(value.URI)
			if err != nil || reference.Repository != catalog.Repository || reference.Base != catalog.BaseOID || reference.Head != catalog.HeadOID {
				continue
			}
			for _, candidate := range []string{reference.Path, reference.OldPath, reference.NewPath} {
				if candidate != "" {
					paths[candidate] = true
				}
			}
		}
	}
	result := make([]gitdiff.FileSummary, 0)
	for _, file := range catalog.Files {
		if paths[file.Path] || paths[file.OldPath] || paths[file.NewPath] {
			result = append(result, file)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

// catalogFileNarrativeOwners is the reverse half of target-scoped linked code:
// it reads the authored evidence for each explanation, but only retains the
// explanations that name the selected catalog file. It never reads source
// bodies or constructs the global coverage graph.
func (a *app) catalogFileNarrativeOwners(document *saga.Saga, catalog gitdiff.Catalog, filePath string) ([]*ManifestOwnerView, error) {
	if filePath == "" {
		return nil, nil
	}
	file, ok := catalogFile(catalog, filePath)
	if !ok {
		return nil, fmt.Errorf("changed file not found")
	}
	paths := map[string]bool{file.Path: true}
	if file.OldPath != "" {
		paths[file.OldPath] = true
	}
	if file.NewPath != "" {
		paths[file.NewPath] = true
	}
	locations := indexNarrativeFragments(document)
	byPath, err := a.evidenceOwnersByPath(document, catalog, locations)
	if err != nil {
		return nil, err
	}
	targets := map[string]bool{}
	for path := range paths {
		for target := range byPath[path] {
			targets[target] = true
		}
	}
	var owners []*ManifestOwnerView
	for _, location := range locations {
		if !targets[location.target] {
			continue
		}
		title := location.title
		if title == "" {
			title = location.itemID
		}
		kind := "Fragment"
		if location.target != location.fragment.Target {
			kind = "Landmark"
		}
		owners = append(owners, &ManifestOwnerView{
			Target: location.target, Title: title, Kind: kind,
			Chapter: location.chapterTitle, Href: location.fragmentHref,
		})
	}
	return owners, nil
}

type targetEvidenceResult struct {
	target   string
	evidence []saga.DiffFile
	err      error
}

func (a *app) evidenceOwnersByPath(document *saga.Saga, catalog gitdiff.Catalog, locations []narrativeLocation) (map[string]map[string]bool, error) {
	source := sourceCatalogIdentity(catalog)
	a.evidence.mutex.Lock()
	defer a.evidence.mutex.Unlock()
	if a.evidence.ready && a.evidence.source == source {
		return a.evidence.byPath, nil
	}

	index := saga.MutationIndexFromDocument(document)
	jobs := make(chan narrativeLocation, len(locations))
	results := make(chan targetEvidenceResult, len(locations))
	jobCount := 0
	for _, location := range locations {
		if !location.hasDiffs {
			continue
		}
		jobs <- location
		jobCount++
	}
	close(jobs)
	workers := jobCount
	if workers > 8 {
		workers = 8
	}
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for location := range jobs {
				evidence, validation, err := saga.LoadTargetDiffs(index, location.target)
				if err == nil && !validation.Valid {
					err = fmt.Errorf("target evidence is invalid")
				}
				results <- targetEvidenceResult{target: location.target, evidence: evidence, err: err}
			}
		}()
	}
	wait.Wait()
	close(results)

	byPath := map[string]map[string]bool{}
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		for _, path := range evidenceCatalogPaths(result.evidence, catalog) {
			if byPath[path] == nil {
				byPath[path] = map[string]bool{}
			}
			byPath[path][result.target] = true
		}
	}
	a.evidence.source, a.evidence.byPath, a.evidence.ready = source, byPath, true
	a.evidence.builds++
	return byPath, nil
}

func evidenceCatalogPaths(evidence []saga.DiffFile, catalog gitdiff.Catalog) []string {
	paths := map[string]bool{}
	for _, file := range evidence {
		for _, value := range file.Diffs {
			reference, err := diffuri.Parse(value.URI)
			if err != nil || reference.Repository != catalog.Repository || reference.Base != catalog.BaseOID || reference.Head != catalog.HeadOID {
				continue
			}
			for _, path := range []string{reference.Path, reference.OldPath, reference.NewPath} {
				if path != "" {
					paths[path] = true
				}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	return result
}

func narrativeTargetTitle(section *saga.Section, target string) (string, bool) {
	if section.Target == target {
		return section.Title, true
	}
	for _, fragment := range section.Fragments {
		if fragment.Target == target {
			if fragment.Title != "" {
				return fragment.Title, true
			}
			return fragment.ID, true
		}
		for _, landmark := range fragment.Landmarks {
			if landmark.Target == target {
				return landmark.Label, true
			}
		}
	}
	for _, child := range section.Children {
		if title, ok := narrativeTargetTitle(child, target); ok {
			return title, true
		}
	}
	return "", false
}
