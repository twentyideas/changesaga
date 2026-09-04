package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitattribution"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/reviewstore"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/snapshotcache"
	"github.com/twentyideas/changesaga/internal/store"
)

type app struct {
	root          string
	sourceDir     string
	template      *template.Template
	mutationToken string
	shutdownToken string
	shutdown      func()
	cache         snapshotCache
	outline       outlineCache
	catalog       sourceCatalogCache
	evidence      evidenceOwnerCache
	// comparisonLoader is the injectable boundary around the expensive source
	// diff and coverage build. Root and narrative shell handlers must never call
	// it; focused comparison endpoints reach it through snapshot().
	comparisonLoader func(context.Context) (*reviewSnapshot, error)
	// catalogLoader is the bounded changed-file metadata seam. Code navigation
	// uses it instead of comparisonLoader so opening the tab cannot construct
	// every source atom or the coverage ownership graph.
	catalogLoader func(context.Context, saga.Manifest) (gitdiff.Catalog, error)
	generations   *snapshotcache.Store
	// reviewRefreshHook is a test seam for the post-commit failure boundary.
	reviewRefreshHook func() error
}

// ManagedOptions lets the CLI supervise a detached loopback server without
// weakening the ordinary foreground server. The shutdown token is random,
// stored only in the user's private runtime directory, and never rendered into
// saga content.
type ManagedOptions struct {
	ShutdownToken string
	OnReady       func(string) error
}

type lockedWriter struct {
	mutex sync.Mutex
	value io.Writer
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.value.Write(data)
}

// OpenBrowser opens a trusted loopback URL using the platform launcher. It is
// exported for the CLI's detached-server path, where readiness is observed in
// a different process from the server itself.
func OpenBrowser(rawURL string) error { return launchBrowser(rawURL) }

type pageData struct {
	Saga          *saga.Saga
	SlideNative   bool
	Root          *sectionView
	Nav           []*navNodeView
	ActivityCount int
	Diagnostic    string
	Code          *CodeReviewView
	Manifest      *CoverageManifestView
	Error         string
	Files         []*fileDiffView
	ReviewedFiles int
	ReviewDecided int
	ReviewTotal   int
	ReviewItems   []*reviewProgressItem
	MutationToken string
	// CoverageTotals is the audit reduced to the numbers the shell states
	// outright. The audit itself stays on the Coverage tab.
	CoverageTotals *coverageTotalsView
}

// coverageTotalsView is the coverage state a reviewer needs before deciding
// whether to open the audit: how much changed, how much of it the story
// explains, and whether anything is still unaccounted for.
type coverageTotalsView struct {
	Files       int
	Total       int
	Covered     int
	Uncovered   int
	Overlapping int
	Orphaned    int
	Mappings    int
	Complete    bool
}

func makeCoverageTotals(manifest *CoverageManifestView) *coverageTotalsView {
	if manifest == nil {
		return nil
	}
	return &coverageTotalsView{
		Files: len(manifest.Files), Total: manifest.Total, Covered: manifest.Covered,
		Uncovered: manifest.Uncovered, Overlapping: manifest.Overlapping,
		Orphaned: manifest.Orphaned, Mappings: manifest.MappingCount, Complete: manifest.Complete,
	}
}

type reviewProgressItem struct {
	Target     string
	Title      string
	Href       string
	State      string
	StateClass string
	Status     string
	Note       string
}

// chapterReviewItem is one approval-bearing destination inside a chapter. A
// chapter is only a container: its own historical approval records remain
// readable for compatibility but never become a row or contribute to current
// completion. ReviewState is the storage-compatible decision used by the
// shared controls; State is the deliberately smaller three-state UI contract.
type chapterReviewItem struct {
	Target       string
	Title        string
	Href         string
	KindLabel    string
	Depth        int
	State        string
	StateClass   string
	Status       string
	CommentCount int
	CommentLabel string
	ActivityHref string
	HasActivity  bool
	ReviewState  string
	ReviewAuthor string
	ReviewDetail string
	ReviewBody   string
}

// navNodeView is the sidebar documentation tree. It exposes titles, links and a
// quiet review state only: never counts, never the storage hierarchy.
type navNodeView struct {
	Title      string
	Href       string
	NodeID     string
	Active     bool
	Expanded   bool
	StateClass string
	StateLabel string
	StateIcon  string
	Children   []*navNodeView
}

type sectionView struct {
	*saga.Section
	// Deferred marks a chapter summary whose body has not been rendered. The
	// body arrives from /api/section the first time the chapter is opened.
	Deferred               bool
	DOMID                  string
	ChangeCount            int
	Attached               *attachedCodeView
	Threads                []*threadView
	FragmentViews          []*fragmentView
	ChildViews             []*sectionView
	ReviewDirectory        []*chapterReviewItem
	ReviewDirectoryDecided int
	DirectoryManaged       bool
	ReviewState            string
	ReviewAuthor           string
	ReviewDetail           string
	ReviewBody             string
}

type fragmentView struct {
	*saga.Fragment
	// Deferred marks a descriptor: the fragment is named, linked, and
	// reviewable, and its content arrives from /api/fragment.
	Deferred      bool
	DOMID         string
	URL           string
	Markdown      template.HTML
	Plain         string
	Interactive   bool
	Image         bool
	AspectRatio   string
	LandmarkViews []*landmarkView
	ChangeCount   int
	Attached      *attachedCodeView
	// Threads keeps its historical meaning: comments that belong to the
	// fragment as a whole, listed under the content. Comments drawn onto the
	// content move to AnnotationThreads and render as bubbles on the mark.
	Threads           []*threadView
	AnnotationThreads []*annotationThreadView
	DirectoryManaged  bool
	ReviewState       string
	ReviewAuthor      string
	ReviewDetail      string
	ReviewBody        string
}

type landmarkView struct {
	saga.Landmark
	DOMID        string
	Title        string
	ChangeCount  int
	Attached     *attachedCodeView
	Threads      []*threadView
	Region       *saga.LandmarkRegion
	ReviewState  string
	ReviewAuthor string
	ReviewDetail string
	ReviewBody   string
}

type diffAtomView struct {
	gitdiff.Atom
	Threads  []*threadView
	Target   string
	Selected bool
}

type fileDiffView = FileDiffView

type threadView struct {
	*saga.Thread
	MessageViews [][]*fragmentView
	StateAuthor  string
	StateDetail  string
}

// annotationThreadView pins a comment to the visual mark it was drawn on. X and
// Y are normalized stage coordinates for the bubble; Placed is false for a
// highlight, whose position only exists once the browser has marked the text,
// so the browser measures that one instead. Comments holds the single thread so
// the bubble can reuse the same comment rendering as the list below the content.
type annotationThreadView struct {
	*threadView
	Comments []*threadView
	Label    string
	PanelID  string
	X        float64
	Y        float64
	Placed   bool
}

func Listen(ctx context.Context, root, sourceDir, addr string, openBrowser bool, out io.Writer) error {
	return ListenManaged(ctx, root, sourceDir, addr, openBrowser, out, ManagedOptions{})
}

func ListenManaged(ctx context.Context, root, sourceDir, addr string, openBrowser bool, out io.Writer, options ManagedOptions) error {
	out = &lockedWriter{value: out}
	if !loopbackListenAddress(addr) {
		return fmt.Errorf("refusing non-loopback listen address %q; remote serving is disabled", addr)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if info, err := os.Stat(abs); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a saga directory", root)
	}
	if sourceDir == "" {
		sourceDir = abs
	}
	if _, validation, err := saga.LoadMutationIndex(abs); err != nil {
		return err
	} else if !validation.Valid {
		return fmt.Errorf("saga is structurally invalid; run change-saga validate")
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		return err
	}
	mutationToken, err := newMutationToken()
	if err != nil {
		return fmt.Errorf("create mutation token: %w", err)
	}
	generations, err := snapshotcache.Default()
	if err != nil {
		return fmt.Errorf("open review cache: %w", err)
	}
	stopCh := make(chan struct{}, 1)
	application := &app{root: abs, sourceDir: sourceDir, template: tmpl, mutationToken: mutationToken, shutdownToken: options.ShutdownToken, generations: generations}
	application.shutdown = func() {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}
	mux := newMux(application)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	server := newHTTPServer(secureHandler(mux, listener.Addr().String()))
	serverURL := "http://" + listener.Addr().String()
	if host, port, err := net.SplitHostPort(listener.Addr().String()); err == nil && host == "127.0.0.1" {
		serverURL = "http://127.0.0.1:" + port
	}
	if options.OnReady != nil {
		if err := options.OnReady(serverURL); err != nil {
			_ = listener.Close()
			return fmt.Errorf("publish managed server state: %w", err)
		}
	}
	fmt.Fprintf(out, "Change Saga is available at %s\nPress Ctrl-C to stop.\n", serverURL)
	if openBrowser {
		if err := launchBrowser(serverURL); err != nil {
			fmt.Fprintf(out, "Could not open a browser automatically: %v\n", err)
		}
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case <-stopCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func newMux(application *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chapters/{chapter}", application.page)
	mux.HandleFunc("GET /", application.page)
	mux.HandleFunc("GET /app.js", application.javascript)
	mux.HandleFunc("GET /theme.js", application.themeScript)
	mux.HandleFunc("GET /api/code", application.codePage)
	mux.HandleFunc("GET /api/activity", application.reviewActivity)
	mux.HandleFunc("GET /api/coverage", application.coveragePage)
	mux.HandleFunc("GET /api/coverage-file", application.coverageFilePage)
	mux.HandleFunc("GET /api/coverage-target", application.coverageTargetPage)
	mux.HandleFunc("GET /api/file-diff", application.fileDiffFragment)
	mux.HandleFunc("GET /api/target-code", application.targetCode)
	mux.HandleFunc("GET /api/file-owners", application.fileOwners)
	mux.HandleFunc("GET /api/section", application.sectionBody)
	mux.HandleFunc("GET /api/fragment", application.fragmentContent)
	mux.HandleFunc("GET /api/locate", application.locateAnchor)
	mux.HandleFunc("GET /api/runtime", application.runtimeStatus)
	mux.HandleFunc("POST /api/runtime-stop", application.runtimeStop)
	mux.HandleFunc("GET /f/{id}/{path...}", application.fragmentFile)
	mux.HandleFunc("POST /api/thread", application.createThread)
	mux.HandleFunc("POST /api/reply", application.reply)
	mux.HandleFunc("POST /api/thread-state", application.threadState)
	mux.HandleFunc("POST /api/thread-anchor", application.threadAnchor)
	mux.HandleFunc("POST /api/review", application.review)
	mux.HandleFunc("POST /api/diff-review", application.diffReview)
	return mux
}

func (a *app) runtimeStatus(w http.ResponseWriter, _ *http.Request) {
	state, _ := a.snapshotState()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": state != "error", "cache": state})
}

// fileDiffFragment renders one complete changed file on demand. It is the only
// place a diff body is produced: the page ships file summaries, and the linked
// code drawer and the coverage audit both ask for a body when a reviewer opens
// one file. Inlining every body instead made the document grow with the whole
// comparison, twice over, for markup no reviewer had asked to see.
//
// `target` scopes the body to one narrative owner, so the rows that target
// explains are marked as its evidence and any comment written from the drawer
// is attributed to it. `view=manifest` returns the read-only rows the coverage
// audit shows, which carry no per-line review actions.
func (a *app) fileDiffFragment(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "missing changed file", http.StatusBadRequest)
		return
	}
	// Target-scoped drawers still need the mapping generation to mark the exact
	// rows owned by that explanation. Ordinary Code and Coverage file bodies do
	// not: read only the requested catalog entry and leave mapping independent.
	if r.URL.Query().Get("target") != "" {
		a.mappedFileDiffFragment(w, r)
		return
	}
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
	file, ok := catalogFile(catalog, filePath)
	if !ok {
		http.Error(w, "changed file not found", http.StatusNotFound)
		return
	}
	changes, err := gitdiff.ReadFile(r.Context(), a.sourceDir, catalog, file)
	if err != nil {
		http.Error(w, "The file diff could not be loaded.", http.StatusInternalServerError)
		return
	}
	manifestView := r.URL.Query().Get("view") == "manifest"
	var threads map[string][]*threadView
	if !manifestView {
		_, threads = threadViews(document)
	}
	files := makeFileViews(changes, saga.SagaTarget(document.Manifest.ID), document.DiffReviews, threads)
	var selected *FileDiffView
	for _, candidate := range files {
		if candidate.Path == filePath {
			selected = candidate
			break
		}
	}
	if selected == nil {
		// Binary and mode-only entries can have catalog metadata without text
		// rows. They still render a stable, reviewable file shell.
		selected = catalogFileView(catalog, file, latestReviewForCatalogFile(document, catalog, filePath))
	}
	total := len(selected.Lines)
	window, err := pageRequest(r, "file-diff\x00"+sourceCatalogIdentity(catalog)+"\x00"+filePath+"\x00\x00"+r.URL.Query().Get("view"), total, defaultDiffPageLimit, maxDiffPageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	selected.Lines = selected.Lines[window.start:window.end]
	name := "file-diff-page"
	if manifestView {
		name = "manifest-file-diff-page"
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	page := fileDiffPageView{File: selected, NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start}
	if err := a.template.ExecuteTemplate(w, name, page); err != nil {
		http.Error(w, "The file diff could not be rendered.", http.StatusInternalServerError)
	}
}

func (a *app) mappedFileDiffFragment(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	document := a.sourceReviewDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	manifestView := r.URL.Query().Get("view") == "manifest"
	target := r.URL.Query().Get("target")
	if target != "" && !targetExists(document, target) {
		http.Error(w, "unknown narrative target", http.StatusBadRequest)
		return
	}
	selection, err := a.selectTargetCode(r.Context(), document, target, filePath)
	if err != nil {
		http.Error(w, "The linked file diff could not be loaded.", http.StatusInternalServerError)
		return
	}
	_, diffThreads := threadViews(document)
	files := makeFileViews(selection.changes, target, document.DiffReviews, diffThreads)
	var selected *FileDiffView
	for _, candidate := range files {
		if candidate.Path == filePath {
			selected = candidate
			break
		}
	}
	if selected == nil {
		file, ok := catalogFile(selection.catalog, filePath)
		if !ok {
			http.Error(w, "changed file not found", http.StatusNotFound)
			return
		}
		selected = catalogFileView(selection.catalog, file, latestReviewForCatalogFile(document, selection.catalog, filePath))
	}
	linked := make(map[string]bool, len(selection.matched))
	for _, atom := range selection.matched {
		linked[atom.URI] = true
	}
	for _, line := range selected.Lines {
		line.Linked = line.Atom != nil && linked[line.Atom.URI]
	}
	total := len(selected.Lines)
	window, err := pageRequest(r, "file-diff\x00"+sourceCatalogIdentity(selection.catalog)+"\x00"+filePath+"\x00"+target+"\x00"+r.URL.Query().Get("view"), total, defaultDiffPageLimit, maxDiffPageLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	selected.Lines = selected.Lines[window.start:window.end]
	name := "file-diff-page"
	if manifestView {
		name = "manifest-file-diff-page"
	}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	writePageHeaders(w, window)
	page := fileDiffPageView{File: selected, NextCursor: window.next, HasMore: window.hasMore(), Returned: window.end - window.start}
	if err := a.template.ExecuteTemplate(w, name, page); err != nil {
		http.Error(w, "The file diff could not be rendered.", http.StatusInternalServerError)
	}
}

// threadViews indexes the live comments the way the renderer consumes them: by
// the narrative target they belong to, and by the diff line they were written
// on. The page and the incremental endpoints share it so a comment reads the
// same whether it arrives on first load or with the chapter it lives in.
func threadViews(document *saga.Saga) (byTarget, byDiff map[string][]*threadView) {
	byTarget, byDiff = map[string][]*threadView{}, map[string][]*threadView{}
	for _, thread := range document.Threads {
		if thread.State == "withdrawn" {
			continue
		}
		view := makeThreadView(thread)
		if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil {
			byDiff[thread.Anchor.Diff.URI] = append(byDiff[thread.Anchor.Diff.URI], view)
		} else {
			byTarget[thread.Target] = append(byTarget[thread.Target], view)
		}
	}
	return byTarget, byDiff
}

// sectionBody renders one chapter's body on demand: its comments, its
// explanations as descriptors, and the sections nested inside it. It is bounded
// by that one chapter, and it renders at the same scope the page renders its
// root at, so an opened chapter reads exactly as the shell around it.
func (a *app) sectionBody(w http.ResponseWriter, r *http.Request) {
	document := a.narrativeDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	section := findSection(document, r.URL.Query().Get("target"))
	if section == nil {
		http.Error(w, "unknown section", http.StatusNotFound)
		return
	}
	threadsByTarget, _ := threadViews(document)
	scope := viewScope{threads: threadsByTarget}.shell()
	// The chapter response owns the one set of decision controls for everything
	// inside it. Fragment bodies can then stay focused on the authored material.
	scope.directoryManaged = section.Kind == "chapter"
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "section-body", makeSectionView(section, scope)); err != nil {
		http.Error(w, "The chapter could not be rendered.", http.StatusInternalServerError)
	}
}

// fragmentContent renders one explanation's narrative content, marked places,
// annotations, and review records. Linked source summaries are deliberately a
// separate lazy surface: reading prose must never start or wait for a source
// comparison or coverage build.
func (a *app) fragmentContent(w http.ResponseWriter, r *http.Request) {
	document := a.narrativeDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	fragment := findFragmentByTarget(document, r.URL.Query().Get("target"))
	if fragment == nil {
		http.Error(w, "unknown fragment", http.StatusNotFound)
		return
	}
	threadsByTarget, threadsByDiff := threadViews(document)
	scope := viewScope{threads: threadsByTarget, diffThreads: threadsByDiff, directoryManaged: targetBelongsToChapter(document.Section, fragment.Target)}
	writeIncrementalHeaders(w, "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "fragment", makeFragmentView(fragment, scope)); err != nil {
		http.Error(w, "The explanation could not be rendered.", http.StatusInternalServerError)
	}
}

// locateAnchor answers where a page anchor lives. A permalink can name a
// heading, a marked place, or a comment inside a chapter nobody has opened yet,
// and the browser has to know which chapter to fetch before it can scroll to it.
// Answering here costs one small request on a deep link; shipping the same
// answer as an index would cost every reviewer the whole document on every load.
func (a *app) locateAnchor(w http.ResponseWriter, r *http.Request) {
	anchor := r.URL.Query().Get("anchor")
	if anchor == "" {
		http.Error(w, "missing anchor", http.StatusBadRequest)
		return
	}
	document := a.narrativeDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded.", http.StatusInternalServerError)
		return
	}
	place, ok := locateAnchorIn(document, anchor)
	if !ok {
		http.Error(w, "unknown anchor", http.StatusNotFound)
		return
	}
	response := map[string]string{}
	if place.chapter != "" {
		response["chapter"] = domID(place.chapter)
	}
	if place.fragment != "" {
		response["fragment"] = domID(place.fragment)
	}
	writeIncrementalHeaders(w, "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "The anchor could not be resolved.", http.StatusInternalServerError)
	}
}

// writeIncrementalHeaders answers a request for part of the page. What comes
// back carries live review state — decisions, comments, and the identity behind
// them — so it is never reused from a cache: a reviewer would otherwise open a
// chapter and read it as it was before their own last comment.
func writeIncrementalHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
}

// anchorPlace names the two things a deferred anchor needs before it can be
// scrolled to: the chapter whose body must be fetched, and the fragment whose
// content must be rendered. Either can be empty — an anchor in the overview has
// no chapter, and a chapter's own anchor has no fragment.
type anchorPlace struct {
	chapter  string
	fragment string
}

// locateAnchorIn resolves an anchor exactly when the document names it, and
// otherwise by the "owner--detail" shape every derived anchor uses: a heading,
// a footnote, a marked place, and a comment bubble are all suffixes of the DOM
// id of the thing that owns them.
func locateAnchorIn(document *saga.Saga, anchor string) (anchorPlace, bool) {
	places := anchorPlaces(document)
	if place, ok := places[anchor]; ok {
		return place, true
	}
	for cut := strings.LastIndex(anchor, "--"); cut > 0; cut = strings.LastIndex(anchor[:cut], "--") {
		if place, ok := places[anchor[:cut]]; ok {
			return place, true
		}
	}
	return anchorPlace{}, false
}

func anchorPlaces(document *saga.Saga) map[string]anchorPlace {
	places, byTarget := map[string]anchorPlace{}, map[string]anchorPlace{}
	var walk func(*saga.Section, string)
	walk = func(section *saga.Section, chapter string) {
		if section.Kind == "chapter" {
			chapter = section.Target
		}
		place := anchorPlace{chapter: chapter}
		byTarget[section.Target], places[domID(section.Target)] = place, place
		for _, fragment := range section.Fragments {
			within := anchorPlace{chapter: chapter, fragment: fragment.Target}
			byTarget[fragment.Target], places[domID(fragment.Target)] = within, within
			for index := range fragment.Landmarks {
				byTarget[fragment.Landmarks[index].Target] = within
			}
		}
		for _, child := range section.Children {
			walk(child, chapter)
		}
	}
	walk(document.Section, "")
	for _, thread := range document.Threads {
		place, ok := byTarget[thread.Target]
		if !ok {
			continue
		}
		places[domID("thread:"+thread.ID)] = place
		for _, message := range thread.Messages {
			places[domID("message:"+message.ID)] = place
		}
	}
	return places
}

func findSection(document *saga.Saga, target string) *saga.Section {
	if target == "" {
		return nil
	}
	var found *saga.Section
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Target == target {
			found = section
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return found
}

func findFragmentByTarget(document *saga.Saga, target string) *saga.Fragment {
	if target == "" {
		return nil
	}
	var found *saga.Fragment
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for _, fragment := range section.Fragments {
			if fragment.Target == target {
				found = fragment
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return found
}

func targetBelongsToChapter(root *saga.Section, target string) bool {
	var walk func(*saga.Section, bool) bool
	walk = func(section *saga.Section, inChapter bool) bool {
		if section == nil {
			return false
		}
		inChapter = inChapter || section.Kind == "chapter"
		if inChapter && section.Target == target {
			return true
		}
		for _, fragment := range section.Fragments {
			if inChapter && fragment.Target == target {
				return true
			}
		}
		for _, child := range section.Children {
			if walk(child, inChapter) {
				return true
			}
		}
		return false
	}
	return walk(root, false)
}

// markLinkedEvidence flags the rows of a whole-file diff that a single
// narrative target actually explains, so the drawer keeps showing the reviewer
// which lines its explanation is answerable for once the surrounding file
// arrives. The page used to carry those rows twice — once as the target's
// evidence and once inside the file — purely so the browser could compare them.
func markLinkedEvidence(file *FileDiffView, linked []gitdiff.Atom) {
	if len(linked) == 0 {
		return
	}
	keys := make(map[string]bool, len(linked))
	for _, atom := range linked {
		keys[atom.Key] = true
	}
	for _, line := range file.Lines {
		if line.Atom != nil && keys[line.Atom.Key] {
			line.Linked = true
		}
	}
}

func (a *app) runtimeStop(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Change-Saga-Shutdown")
	if a.shutdownToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(a.shutdownToken)) != 1 {
		http.Error(w, "Missing or invalid shutdown token.", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`+"\n")
	if a.shutdown != nil {
		go a.shutdown()
	}
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           securityHeaders(handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func secureHandler(next http.Handler, listenerAddress string) http.Handler {
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Cross-origin request rejected.", http.StatusForbidden)
	}))
	return validateHost(listenerAddress, crossOrigin.Handler(next))
}

func validateHost(listenerAddress string, next http.Handler) http.Handler {
	allowed := allowedListenerHosts(listenerAddress)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[strings.ToLower(r.Host)] {
			http.Error(w, "Invalid request host.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedListenerHosts(listenerAddress string) map[string]bool {
	host, port, err := net.SplitHostPort(listenerAddress)
	if err != nil {
		return map[string]bool{}
	}
	allowed := map[string]bool{strings.ToLower(net.JoinHostPort(host, port)): true}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		allowed[net.JoinHostPort("localhost", port)] = true
	}
	return allowed
}

func loopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func newMutationToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token[:]), nil
}

// newPageTemplate is the single definition of the renderer's template funcs so
// tests exercise exactly the helpers the served page uses.
func newPageTemplate() (*template.Template, error) {
	return template.New("page").Funcs(templateFuncs()).Parse(pageTemplate)
}

// templateFuncs is shared by the server and its rendering tests so a new
// presentation helper cannot be wired into one and forgotten in the other.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"markdown":    markdown,
		"domID":       domID,
		"fileIcon":    fileIcon,
		"anchorLabel": anchorLabel,
		"lower":       strings.ToLower,
		"reviewDiffSurface": func(path, codeHref string) reviewDiffSurfaceView {
			return reviewDiffSurfaceView{Path: path, CodeHref: codeHref}
		},
		"annotationColor": func(value string) string {
			if validAnnotationColor(value) {
				return value
			}
			return defaultAnnotationColor
		},
		"noteColor": func(value string) string {
			if validAnnotationColor(value) {
				return value
			}
			return defaultNoteColor
		},
		"percent": func(value float64) string { return strconv.FormatFloat(value*100, 'f', 4, 64) },
		"coord":   func(value float64) string { return strconv.FormatFloat(value*1000, 'f', 2, 64) },
		"points": func(values []saga.Point) string {
			parts := make([]string, 0, len(values))
			for _, point := range values {
				parts = append(parts, fmt.Sprintf("%.2f,%.2f", point.X*1000, point.Y*1000))
			}
			return strings.Join(parts, " ")
		},
	}
}

func (a *app) page(w http.ResponseWriter, r *http.Request) {
	document := a.outlineDocument(r.Context())
	if document == nil {
		http.Error(w, "The saga could not be loaded. Run change-saga validate for details.", http.StatusInternalServerError)
		return
	}
	if document.Manifest.Version == saga.SlideSagaVersion {
		document = a.narrativeDocument(r.Context())
		if document == nil {
			http.Error(w, "The slide deck could not be loaded. Run change-saga validate for details.", http.StatusInternalServerError)
			return
		}
	}
	chapterID, chapterRoute := requestedChapter(r)
	if r.URL.Path != "/" {
		if !chapterRoute {
			http.NotFound(w, r)
			return
		}
		for _, child := range document.Section.Children {
			if child.Kind == "chapter" && child.ID == chapterID {
				http.Redirect(w, r, "/#"+domID(child.Target), http.StatusFound)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	threadsByTarget, _ := threadViews(document)
	// The saga view is a shell: identity, coverage totals, the overview's
	// fragments as descriptors, one summary per chapter, and the navigation
	// outline. Everything below that arrives from /api/section and
	// /api/fragment as a reviewer opens it.
	scope := viewScope{threads: threadsByTarget}
	if document.Manifest.Version != saga.SlideSagaVersion {
		scope = scope.shell()
	}
	rootView := makeSectionView(document.Section, scope)
	data := pageData{
		Saga:           document,
		SlideNative:    document.Manifest.Version == saga.SlideSagaVersion,
		Root:           rootView,
		MutationToken:  a.mutationToken,
		CoverageTotals: a.cachedCoverageTotals(),
	}
	if data.SlideNative {
		data.ReviewItems = makeSlideReviewProgressItems(document.Section)
	} else {
		data.ReviewItems = makeReviewProgressItems(document.Section)
	}
	data.ReviewDecided, data.ReviewTotal = reviewProgressSummary(data.ReviewItems)
	data.ActivityCount = reviewActivityCount(document)
	data.Nav = makeNavTree(document.Section, threadsByTarget)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "page", data); err != nil {
		http.Error(w, "The review page could not be rendered.", http.StatusInternalServerError)
	}
}

func (a *app) narrativeDocument(ctx context.Context) *saga.Saga {
	document, validation, err := saga.LoadNarrative(a.root)
	if err != nil || !validation.Valid {
		return nil
	}
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	return document
}

// sourceReviewDocument adds the small, mutable file-review overlay to the
// narrative generation without opening authored coverage mappings. Code and
// ordinary file responses need this overlay, while prose-only requests keep
// using narrativeDocument and never touch diff-review records.
func (a *app) sourceReviewDocument(ctx context.Context) *saga.Saga {
	document, validation, err := saga.LoadNarrative(a.root)
	if err != nil || !validation.Valid {
		return nil
	}
	state, reviewValidation, err := saga.LoadReviewState(saga.MutationIndexFromDocument(document))
	if err != nil || !reviewValidation.Valid {
		return nil
	}
	document.DiffReviews = state.DiffReviews
	applyGitAttribution(ctx, gitattribution.New(ctx, a.root), document)
	return document
}

func requestedChapter(r *http.Request) (string, bool) {
	if value := r.PathValue("chapter"); value != "" {
		return value, true
	}
	const prefix = "/chapters/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(r.URL.Path, prefix)
	return value, value != "" && !strings.Contains(value, "/")
}

// reviewProgress reduces a chapter's directory to a quiet resume signal. The
// chapter's own legacy approval is intentionally absent: completion belongs to
// the individual approval-bearing things inside it.
func reviewProgress(section *saga.Section, threads map[string][]*threadView) (status, class, icon string) {
	items := makeChapterReviewDirectory(section, threads)
	allApproved := len(items) > 0
	for _, item := range items {
		if item.ReviewState == "rejected" {
			return "Needs changes", "rejected", "reject"
		}
		if item.ReviewState != "approved" {
			allApproved = false
		}
	}
	if allApproved {
		return "Approved", "approved", "check"
	}
	for _, item := range items {
		if item.HasActivity {
			return "In progress", "progress", "half"
		}
	}
	return "Unreviewed", "", "circle"
}

// makeNavTree builds a documentation outline for the one-page saga. It reads the
// document rather than the rendered views: the page ships chapter summaries, and
// the outline still has to name every destination beneath them so a reviewer can
// navigate into a chapter that has not been fetched yet. Titles and targets come
// from the saga's own manifests, so building the whole outline reads no content.
func makeNavTree(root *saga.Section, threads map[string][]*threadView) []*navNodeView {
	overview := &navNodeView{Title: "Overview", Href: sagaHref(root.Target), NodeID: "nav-overview", Active: true}
	overview.Children = withoutRedundantLead(fragmentOutline(root), overview.Title)
	overview.Expanded = len(overview.Children) > 0
	nodes := []*navNodeView{overview}
	for _, child := range root.Children {
		if child.Kind != "chapter" {
			continue
		}
		status, class, icon := reviewProgress(child, threads)
		node := &navNodeView{
			Title: child.Title, Href: sagaHref(child.Target),
			NodeID:     "nav-" + domID(child.Target),
			StateLabel: status, StateClass: class, StateIcon: icon,
		}
		node.Children = withoutRedundantLead(documentOutline(child), node.Title)
		nodes = append(nodes, node)
	}
	return nodes
}

// documentOutline turns the open page into headings a reader recognises. Titled
// content becomes an entry; untitled content is skipped rather than exposed
// under an internal identifier.
func documentOutline(section *saga.Section) []*navNodeView {
	nodes := fragmentOutline(section)
	for _, child := range section.Children {
		if child.Title == "" {
			continue
		}
		id := domID(child.Target)
		node := &navNodeView{Title: child.Title, Href: "#" + id, NodeID: "nav-" + id}
		node.Children = documentOutline(child)
		node.Expanded = len(node.Children) > 0
		nodes = append(nodes, node)
	}
	return nodes
}

// fragmentOutline lists one section's own explanations, which is the whole of
// the overview's outline: the overview is the root, and its chapters are
// separate top-level entries rather than children of it.
func fragmentOutline(section *saga.Section) []*navNodeView {
	var nodes []*navNodeView
	for _, fragment := range section.Fragments {
		// A lead-in that repeats the page title is not a separate destination.
		if fragment.Title == "" || strings.EqualFold(fragment.Title, section.Title) {
			continue
		}
		id := domID(fragment.Target)
		nodes = append(nodes, &navNodeView{Title: fragment.Title, Href: "#" + id, NodeID: "nav-" + id})
	}
	return nodes
}

// withoutRedundantLead drops a leading entry that only repeats the label of the
// page it sits under. A tree that reads "Overview > Overview" tells the reader
// nothing, and the parent row already links to that content.
func withoutRedundantLead(nodes []*navNodeView, label string) []*navNodeView {
	if len(nodes) == 0 || len(nodes[0].Children) > 0 || !strings.EqualFold(nodes[0].Title, label) {
		return nodes
	}
	return nodes[1:]
}

// makeChapterReviewDirectory walks only authored metadata and the compact
// thread index. It is therefore safe to build in /api/section without opening
// fragment bodies, coverage mappings, or the source comparison.
func makeChapterReviewDirectory(chapter *saga.Section, threads map[string][]*threadView) []*chapterReviewItem {
	if chapter == nil {
		return nil
	}
	var result []*chapterReviewItem
	var walk func(*saga.Section, int, bool)
	walk = func(section *saga.Section, depth int, includeSection bool) {
		if section == nil {
			return
		}
		if includeSection && section.Kind != "chapter" {
			result = append(result, makeChapterReviewItem(section.Target, section.Title, "Section", depth, section.Reviews, len(threads[section.Target])))
		}
		for _, fragment := range section.Fragments {
			title := fragment.Title
			if title == "" {
				title = fragment.ID
			}
			comments := len(threads[fragment.Target])
			for _, landmark := range fragment.Landmarks {
				comments += len(threads[landmark.Target])
			}
			result = append(result, makeChapterReviewItem(fragment.Target, title, "Explanation", depth, fragment.Reviews, comments))
		}
		for _, child := range section.Children {
			walk(child, depth+1, true)
		}
	}
	walk(chapter, 0, false)
	return result
}

func makeChapterReviewItem(target, title, kind string, depth int, reviews []saga.Review, comments int) *chapterReviewItem {
	rawState, author, detail, body := latestReview(reviews)
	item := &chapterReviewItem{
		Target: target, Title: title, Href: "#" + domID(target), KindLabel: kind, Depth: depth,
		State: "unreviewed", StateClass: "unreviewed", Status: "Unreviewed",
		CommentCount: comments, HasActivity: rawState != "" || comments > 0,
		ReviewAuthor: author, ReviewDetail: detail, ReviewBody: body,
	}
	if comments > 0 {
		item.ActivityHref = "/?activity=1&target=" + url.QueryEscape(target)
	}
	if comments == 1 {
		item.CommentLabel = "1 comment or annotation"
	} else {
		item.CommentLabel = fmt.Sprintf("%d comments and annotations", comments)
	}
	switch rawState {
	case "approved":
		item.State, item.StateClass, item.Status, item.ReviewState = "approved", "approved", "Approved", "approved"
	case "rejected":
		item.State, item.StateClass, item.Status, item.ReviewState = "changes-requested", "changes-requested", "Changes requested", "rejected"
	}
	return item
}

// makeReviewProgressItems counts decisions over the whole document, not over the
// part of it the page happens to have rendered. It reads review records and
// titles only, so the progress map stays complete while chapter bodies are still
// deferred.
func makeReviewProgressItems(root *saga.Section) []*reviewProgressItem {
	if root == nil {
		return nil
	}
	var result []*reviewProgressItem
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section == nil {
			return
		}
		if section.Kind != "chapter" {
			title := section.Title
			if title == "" {
				title = section.ID
			}
			state, _, _, body := latestReview(section.Reviews)
			result = append(result, makeReviewProgressItem(section.Target, title, "#"+domID(section.Target), state, body))
		}
		for _, fragment := range section.Fragments {
			fragmentTitle := fragment.Title
			if fragmentTitle == "" {
				fragmentTitle = fragment.ID
			}
			fragmentState, _, _, fragmentBody := latestReview(fragment.Reviews)
			result = append(result, makeReviewProgressItem(fragment.Target, fragmentTitle, "#"+domID(fragment.Target), fragmentState, fragmentBody))
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(root)
	return result
}

// makeSlideReviewProgressItems reflects the v4 decision boundary: reviewers
// approve complete visual arguments (slides), while Items remain precise
// evidence and comment targets rather than becoming a checklist of approvals.
func makeSlideReviewProgressItems(root *saga.Section) []*reviewProgressItem {
	if root == nil {
		return nil
	}
	var result []*reviewProgressItem
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for _, fragment := range section.Fragments {
			if fragment.SlideMeta == nil {
				continue
			}
			title := fragment.Title
			if title == "" {
				title = fragment.ID
			}
			state, _, _, body := latestReview(fragment.Reviews)
			result = append(result, makeReviewProgressItem(fragment.Target, title, "#"+domID(fragment.Target), state, body))
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(root)
	return result
}

func makeReviewProgressItem(target, title, href, state, note string) *reviewProgressItem {
	item := &reviewProgressItem{Target: target, Title: title, Href: href, State: state, StateClass: "pending", Status: "Not reviewed", Note: note}
	switch state {
	case "approved":
		item.StateClass, item.Status = "approved", "Approved"
	case "rejected":
		item.StateClass, item.Status = "rejected", "Changes requested"
	}
	return item
}

func reviewProgressSummary(items []*reviewProgressItem) (decided, total int) {
	for _, item := range items {
		total++
		if item.State == "approved" || item.State == "rejected" {
			decided++
		}
	}
	return decided, total
}

// anchorLabel keeps thread metadata in the reviewer's vocabulary instead of
// exposing the stored anchor discriminator.
func anchorLabel(kind string) string {
	switch kind {
	case "region":
		return "rectangle"
	case "drawing":
		return "freehand"
	case "text":
		return "highlight"
	case "diff":
		return "code"
	case "target":
		return "comment"
	}
	return "note"
}

// annotationAnchor reports whether a comment was drawn onto the content: a
// rectangle, a freehand drawing, a highlight, or a sticky note. Those comments
// render as bubbles pinned to the mark. Every other anchor — a whole fragment,
// a section, a chapter, a diff line — keeps its place in the list below.
func annotationAnchor(kind string) bool {
	switch kind {
	case "region", "drawing", "text", "note":
		return true
	}
	return false
}

// annotationBubbleLabel names the mark a bubble belongs to, in the same
// vocabulary the annotation toolbox uses. anchorLabel answers "note" for every
// anchor it does not know, which is too vague to say out loud on a bubble.
func annotationBubbleLabel(kind string) string {
	if kind == "note" {
		return "sticky note"
	}
	return anchorLabel(kind)
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// annotationShapeBounds is the normalized box a drawn shape occupies. It mirrors
// shapeBounds in appjs.go, because the server places a bubble from the stored
// anchor and the browser then refines it from the rendered mark; the two must
// agree on where the shape is.
func annotationShapeBounds(shape saga.Shape) (left, top, right, bottom float64, ok bool) {
	switch shape.Type {
	case "path":
		if len(shape.Points) == 0 {
			return 0, 0, 0, 0, false
		}
		left, right = shape.Points[0].X, shape.Points[0].X
		top, bottom = shape.Points[0].Y, shape.Points[0].Y
		for _, point := range shape.Points[1:] {
			left, right = math.Min(left, point.X), math.Max(right, point.X)
			top, bottom = math.Min(top, point.Y), math.Max(bottom, point.Y)
		}
		return left, top, right, bottom, true
	case "line":
		return math.Min(shape.X, shape.Width), math.Min(shape.Y, shape.Height),
			math.Max(shape.X, shape.Width), math.Max(shape.Y, shape.Height), true
	case "ellipse":
		return shape.X - shape.Width, shape.Y - shape.Height, shape.X + shape.Width, shape.Y + shape.Height, true
	case "rect":
		return shape.X, shape.Y, shape.X + shape.Width, shape.Y + shape.Height, true
	}
	return 0, 0, 0, 0, false
}

// annotationBubblePoint is where a bubble sits before the browser has measured
// anything: the top-right corner of the mark. A highlight reports no point
// because its position is a property of the rendered text, not of the record.
func annotationBubblePoint(anchor saga.Anchor) (x, y float64, ok bool) {
	if anchor.Type == "note" {
		if anchor.Note == nil {
			return 0, 0, false
		}
		return clampUnit(anchor.Note.X), clampUnit(anchor.Note.Y), true
	}
	for _, shape := range anchor.Shapes {
		_, shapeTop, shapeRight, _, valid := annotationShapeBounds(shape)
		if !valid {
			continue
		}
		if !ok {
			x, y, ok = shapeRight, shapeTop, true
			continue
		}
		x, y = math.Max(x, shapeRight), math.Min(y, shapeTop)
	}
	if !ok {
		return 0, 0, false
	}
	return clampUnit(x), clampUnit(y), true
}

func makeAnnotationThreadView(thread *threadView) *annotationThreadView {
	view := &annotationThreadView{
		threadView: thread,
		Comments:   []*threadView{thread},
		Label:      annotationBubbleLabel(thread.Anchor.Type),
		PanelID:    domID("thread:"+thread.ID) + "--bubble",
	}
	view.X, view.Y, view.Placed = annotationBubblePoint(thread.Anchor)
	return view
}

// viewScope carries everything a narrative view needs from the snapshot, and how
// much of the tree this render is allowed to materialise. The page renders a
// shell — the overview, its fragments as descriptors, and one summary per
// chapter — because rendering the whole document eagerly made first load grow
// with the size of the story rather than with what a reviewer can see. The
// bounded /api/section and /api/fragment endpoints render one node each, and
// /api/section reuses the page's own scope so a chapter body is built by
// exactly the code that built the page around it.
type viewScope struct {
	changes     map[string][]gitdiff.Atom
	snapshot    *reviewSnapshot
	threads     map[string][]*threadView
	diffThreads map[string][]*threadView
	// summary stops the render at this section's own head: its body arrives
	// from /api/section when a reviewer opens it.
	summary bool
	// summarizeChapters turns this section's direct chapter children into
	// summaries. It applies to one level only, so a chapter body still renders
	// the sections nested inside it as the page always did.
	summarizeChapters bool
	// deferContent renders every fragment as a descriptor whose content arrives
	// from /api/fragment.
	deferContent bool
	// directoryManaged removes duplicate inline decision controls for targets
	// whose chapter directory owns those controls.
	directoryManaged bool
}

// shell is the scope both the page and /api/section render at: this node in
// full, its fragments as descriptors, and any chapter beneath it as a summary.
func (scope viewScope) shell() viewScope {
	scope.summary, scope.summarizeChapters, scope.deferContent = false, true, true
	return scope
}

func makeSectionView(section *saga.Section, scope viewScope) *sectionView {
	changeCount, attached := scopedAttachedCode(scope, section.Title, section.Target, section.Diffs)
	changeCount = lazyChangeCount(section.HasDiffs, changeCount)
	view := &sectionView{
		Section: section, DOMID: domID(section.Target), ChangeCount: changeCount,
		Attached: attached, Threads: scope.threads[section.Target], DirectoryManaged: scope.directoryManaged,
	}
	view.ReviewState, view.ReviewAuthor, view.ReviewDetail, view.ReviewBody = latestReview(section.Reviews)
	if scope.summary {
		view.Deferred = true
		return view
	}
	if section.Kind == "chapter" && scope.directoryManaged {
		view.ReviewDirectory = makeChapterReviewDirectory(section, scope.threads)
		for _, item := range view.ReviewDirectory {
			if item.ReviewState == "approved" || item.ReviewState == "rejected" {
				view.ReviewDirectoryDecided++
			}
		}
	}
	for _, fragment := range section.Fragments {
		view.FragmentViews = append(view.FragmentViews, makeFragmentView(fragment, scope))
	}
	for _, child := range section.Children {
		childScope := scope
		childScope.summary, childScope.summarizeChapters = scope.summarizeChapters && child.Kind == "chapter", false
		view.ChildViews = append(view.ChildViews, makeSectionView(child, childScope))
	}
	return view
}

func makeFragmentView(fragment *saga.Fragment, scope viewScope) *fragmentView {
	title := fragment.Title
	if title == "" {
		title = fragment.ID
	}
	view := &fragmentView{Fragment: fragment, DOMID: domID(fragment.Target), DirectoryManaged: scope.directoryManaged}
	view.ReviewState, view.ReviewAuthor, view.ReviewDetail, view.ReviewBody = latestReview(fragment.Reviews)
	view.URL = "/f/" + url.PathEscape(fragment.ID) + "/" + strings.Join(pathEscapeParts(fragment.Entrypoint), "/")
	if scope.deferContent {
		// A descriptor names the explanation and carries its review controls.
		// The content, its landmarks, and its linked code arrive from
		// /api/fragment once the reviewer can actually see this fragment.
		view.Deferred = true
		return view
	}
	threads := scope.threads[fragment.Target]
	view.ChangeCount, view.Attached = scopedAttachedCode(scope, title, fragment.Target, fragment.Diffs)
	view.ChangeCount = lazyChangeCount(fragment.HasDiffs, view.ChangeCount)
	for _, thread := range threads {
		if annotationAnchor(thread.Anchor.Type) {
			view.AnnotationThreads = append(view.AnnotationThreads, makeAnnotationThreadView(thread))
			continue
		}
		view.Threads = append(view.Threads, thread)
	}
	for _, landmark := range fragment.Landmarks {
		region := landmark.Hotspot
		if region == nil && landmark.Selector.Type == "region" {
			region = &saga.LandmarkRegion{X: landmark.Selector.X, Y: landmark.Selector.Y, Width: landmark.Selector.Width, Height: landmark.Selector.Height}
		}
		changeCount, attached := scopedAttachedCode(scope, landmark.Label, landmark.Target, landmark.Diffs)
		changeCount = lazyChangeCount(landmark.HasDiffs, changeCount)
		landmarkView := &landmarkView{
			Landmark: landmark, DOMID: view.DOMID + "--" + landmark.ID, Title: landmark.Label,
			ChangeCount: changeCount,
			Attached:    attached,
			Threads:     scope.threads[landmark.Target], Region: region,
		}
		landmarkView.ReviewState, landmarkView.ReviewAuthor, landmarkView.ReviewDetail, landmarkView.ReviewBody = latestReview(landmark.Reviews)
		view.LandmarkViews = append(view.LandmarkViews, landmarkView)
	}
	switch fragment.MediaType {
	case "text/markdown":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); err == nil {
			view.Markdown = markdownWithAnchors(string(data), view.DOMID)
		}
	case "text/plain":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); err == nil {
			view.Plain = string(data)
		}
	case "text/html", "image/svg+xml":
		view.Interactive = true
		if fragment.MediaType == "image/svg+xml" {
			if data, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))); err == nil {
				view.AspectRatio = svgAspectRatio(string(data))
				if view.AspectRatio != "" {
					view.URL += "?saga_aspect=" + url.QueryEscape(view.AspectRatio)
				}
			}
		}
	default:
		view.Image = strings.HasPrefix(fragment.MediaType, "image/")
	}
	return view
}

// A negative count is an internal render state: authored evidence exists, but
// its exact current-source match count belongs to the lazy target-code request.
func lazyChangeCount(hasDiffs bool, count int) int {
	if count == 0 && hasDiffs {
		return -1
	}
	return count
}

func scopedAttachedCode(scope viewScope, title, target string, evidence []saga.DiffFile) (int, *attachedCodeView) {
	if scope.snapshot != nil {
		indexes := scope.snapshot.targetAtoms[target]
		attached := makeAttachedCodeViewIndexed(title, target, scope.snapshot, indexes, evidence)
		if attached == nil {
			return 0, nil
		}
		return attached.ChangeCount, attached
	}
	atoms := scope.changes[target]
	attached := makeAttachedCodeView(title, target, atoms, atoms, evidence)
	if attached == nil {
		return 0, nil
	}
	return attached.ChangeCount, attached
}

var svgViewBoxPattern = regexp.MustCompile(`(?i)\bviewBox\s*=\s*["']([^"']+)["']`)

func svgAspectRatio(source string) string {
	match := svgViewBoxPattern.FindStringSubmatch(source)
	if len(match) != 2 {
		return ""
	}
	parts := strings.Fields(match[1])
	if len(parts) != 4 {
		return ""
	}
	width, widthErr := strconv.ParseFloat(parts[2], 64)
	height, heightErr := strconv.ParseFloat(parts[3], 64)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return ""
	}
	return strconv.FormatFloat(width/height, 'f', 8, 64)
}

func makeThreadView(thread *saga.Thread) *threadView {
	view := &threadView{Thread: thread}
	for index := len(thread.Events) - 1; index >= 0; index-- {
		if thread.Events[index].State != "" {
			view.StateAuthor, view.StateDetail = thread.Events[index].Author, thread.Events[index].AttributionDetail
			break
		}
	}
	for _, message := range thread.Messages {
		var fragments []*fragmentView
		for _, fragment := range message.Fragments {
			fragments = append(fragments, makeFragmentView(fragment, viewScope{}))
		}
		view.MessageViews = append(view.MessageViews, fragments)
	}
	return view
}

func makeFileViews(changes gitdiff.ChangeSet, target string, reviews []saga.DiffReview, threads map[string][]*threadView) []*fileDiffView {
	byPath := map[string]*fileDiffView{}
	renameTo := map[string]string{}
	for _, atom := range changes.Atoms {
		if atom.Kind == "event" && atom.Event == "rename" && atom.OldPath != "" && atom.NewPath != "" {
			renameTo[atom.OldPath] = atom.NewPath
		}
	}
	latest := map[string]saga.DiffReview{}
	for _, review := range reviews {
		// Ties on created_at resolve by id so the current reviewed state of a
		// file does not depend on directory listing order.
		if previous, ok := latest[review.URI]; !ok || previous.CreatedAt.Before(review.CreatedAt) ||
			previous.CreatedAt.Equal(review.CreatedAt) && previous.ID < review.ID {
			latest[review.URI] = review
		}
	}
	for _, atom := range changes.Atoms {
		path := atom.Path
		if path == "" {
			path = atom.NewPath
		}
		if renamed, ok := renameTo[path]; ok {
			path = renamed
		}
		file := byPath[path]
		if file == nil {
			uri, _ := diffuri.Build(diffuri.Reference{Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "file", Path: path})
			digest := sha256.Sum256([]byte(path))
			file = &fileDiffView{ID: fmt.Sprintf("diff-%x", digest[:8]), Path: path, URI: uri}
			if review, ok := latest[uri]; ok {
				file.Reviewed, file.Reviewer, file.ReviewerDetail = review.State == "reviewed", review.Author, review.AttributionDetail
			}
			byPath[path] = file
		}
		file.Atoms = append(file.Atoms, &diffAtomView{Atom: atom, Threads: threads[atom.URI], Target: target})
		if atom.Side == "new" {
			file.Added++
		} else if atom.Side == "old" {
			file.Deleted++
		}
	}
	// Keep a stable key lookup so renderer-only context lines can point back to
	// the exact changed atom used by comments, suggestions, and coverage.
	atomsByKey := map[string]*diffAtomView{}
	for _, file := range byPath {
		for _, atom := range file.Atoms {
			atomsByKey[atom.Key] = atom
		}
	}
	for _, line := range changes.DisplayLines {
		linePath := line.Path
		if renamed, ok := renameTo[linePath]; ok {
			linePath = renamed
		}
		file := byPath[linePath]
		if file == nil {
			continue
		}
		file.Lines = append(file.Lines, &DiffLineView{
			Kind: line.Kind, Path: linePath, OldLine: line.OldLine, NewLine: line.NewLine,
			Content: line.Content, Event: line.Event, OldPath: line.OldPath, NewPath: line.NewPath,
			Atom: atomsByKey[line.AtomKey],
		})
	}
	// Manually constructed ChangeSets (and older callers) have no display
	// context. Fall back to the changed atoms without weakening their actions.
	for _, file := range byPath {
		if len(file.Lines) != 0 {
			continue
		}
		for _, atom := range file.Atoms {
			line := &DiffLineView{Kind: atom.Side, Path: file.Path, Content: atom.Content, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath, Atom: atom}
			if atom.Kind == "event" {
				line.Kind = "event"
			} else if atom.Side == "old" {
				line.OldLine = atom.Line
			} else {
				line.NewLine = atom.Line
			}
			file.Lines = append(file.Lines, line)
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]*fileDiffView, 0, len(paths))
	for _, path := range paths {
		result = append(result, byPath[path])
	}
	return result
}

func latestReview(reviews []saga.Review) (string, string, string, string) {
	if len(reviews) == 0 {
		return "", "", "", ""
	}
	values := append([]saga.Review(nil), reviews...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	last := values[len(values)-1]
	return last.State, last.Author, last.AttributionDetail, last.Body
}

func applyGitAttribution(ctx context.Context, resolver *gitattribution.Resolver, document *saga.Saga) {
	type target struct {
		path   string
		author *string
		detail *string
	}
	var targets []target
	collect := func(path string, author *string, detail *string) {
		targets = append(targets, target{path: path, author: author, detail: detail})
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for index := range section.Reviews {
			review := &section.Reviews[index]
			collect(review.Path, &review.Author, &review.AttributionDetail)
		}
		for _, fragment := range section.Fragments {
			for index := range fragment.Reviews {
				review := &fragment.Reviews[index]
				collect(review.Path, &review.Author, &review.AttributionDetail)
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	for _, thread := range document.Threads {
		collect(filepath.Join(thread.Directory, "thread.json"), &thread.CreatedBy, &thread.AttributionDetail)
		for _, message := range thread.Messages {
			collect(message.Path, &message.Author, &message.AttributionDetail)
		}
		for index := range thread.Events {
			event := &thread.Events[index]
			collect(event.Path, &event.Author, &event.AttributionDetail)
		}
	}
	for index := range document.DiffReviews {
		review := &document.DiffReviews[index]
		collect(review.Path, &review.Author, &review.AttributionDetail)
	}
	paths := make([]string, len(targets))
	for index, target := range targets {
		paths[index] = target.path
	}
	for index, value := range resolver.ResolveAll(ctx, paths) {
		target := targets[index]
		switch value.State {
		case gitattribution.Committed:
			*target.author = value.Name
			commitID := value.CommitID
			if len(commitID) > 12 {
				commitID = commitID[:12]
			}
			*target.detail = fmt.Sprintf("%s · committed %s · %s", value.Email, value.CommittedAt.Format("2006-01-02 15:04 MST"), commitID)
		case gitattribution.Uncommitted:
			*target.author = "Local / uncommitted"
			*target.detail = "This review event has not been committed yet."
		case gitattribution.Rewritten:
			*target.author = "History rewritten"
			*target.detail = "Git history no longer contains the commit that introduced this review event. Stored legacy identity is not authoritative."
		default:
			*target.author = "Git history unavailable"
			*target.detail = "Git attribution is unavailable. Stored legacy identity is not authoritative."
		}
	}
}

func (a *app) fragmentFile(w http.ResponseWriter, r *http.Request) {
	index, validation, err := saga.LoadMutationIndex(a.root)
	if err != nil || !validation.Valid {
		http.Error(w, "The saga could not be loaded. Run change-saga validate for details.", http.StatusInternalServerError)
		return
	}
	assetTarget := saga.FragmentTarget(index.Manifest.ID, r.PathValue("id"))
	if index.Manifest.Version == saga.SlideSagaVersion {
		assetTarget = saga.SlideTarget(index.Manifest.ID, r.PathValue("id"))
	}
	fragmentDir, ok := index.Targets[assetTarget]
	if !ok {
		http.NotFound(w, r)
		return
	}
	rel := filepath.Clean(filepath.FromSlash(r.PathValue("path")))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || hasReservedPart(rel) {
		http.Error(w, "invalid fragment path", http.StatusBadRequest)
		return
	}
	path := filepath.Join(fragmentDir, rel)
	realRoot, rootErr := filepath.EvalSymlinks(fragmentDir)
	realPath, pathErr := filepath.EvalSymlinks(path)
	if rootErr != nil || pathErr != nil {
		http.NotFound(w, r)
		return
	}
	realRel, err := filepath.Rel(realRoot, realPath)
	if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		http.Error(w, "fragment file escapes its package", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' blob:; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	file, err := os.Open(realPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(realPath))); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, filepath.Base(realPath), info.ModTime(), file)
}

func (a *app) javascript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, appJavaScript)
}

// themeScript is served as its own file rather than inlined in the head
// because the page's Content-Security-Policy allows script-src 'self' only.
// It stays out of app.js, and stays render-blocking, so the theme is settled
// before the first paint instead of one deferred script later.
func (a *app) themeScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, themeBoot)
}

func (a *app) createThread(w http.ResponseWriter, r *http.Request) {
	attachments, cleanup, err := parseMultipart(r, w)
	if err != nil {
		writeMultipartError(w, err)
		return
	}
	defer cleanup()
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	index, validation, err := saga.LoadMutationIndex(a.root)
	if err != nil || !validation.Valid {
		http.Error(w, "The saga could not be loaded. Run change-saga validate for details.", http.StatusConflict)
		return
	}
	target := r.FormValue("target")
	if _, ok := index.Targets[target]; !ok {
		http.Error(w, "target does not exist", http.StatusBadRequest)
		return
	}
	var anchor saga.Anchor
	if err := json.Unmarshal([]byte(r.FormValue("anchor")), &anchor); err != nil {
		http.Error(w, "invalid annotation anchor", http.StatusBadRequest)
		return
	}
	if _, err := reviewstore.AddThread(a.root, target, r.FormValue("body"), anchor, r.FormValue("kind"), r.FormValue("replacement"), attachments); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	redirectAfterReview(w, r, "/#"+domID(target))
}

func (a *app) reply(w http.ResponseWriter, r *http.Request) {
	attachments, cleanup, err := parseMultipart(r, w)
	if err != nil {
		writeMultipartError(w, err)
		return
	}
	defer cleanup()
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	if _, err := reviewstore.AddReply(a.root, r.FormValue("thread"), r.FormValue("body"), attachments); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) threadState(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, 64<<10); err != nil {
		http.Error(w, "Invalid request body.", http.StatusBadRequest)
		return
	}
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	if err := reviewstore.SetState(a.root, r.FormValue("thread"), r.FormValue("state")); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) threadAnchor(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, 2<<20); err != nil {
		http.Error(w, "Invalid request body.", http.StatusBadRequest)
		return
	}
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	var anchor saga.Anchor
	if err := json.Unmarshal([]byte(r.FormValue("anchor")), &anchor); err != nil {
		http.Error(w, "invalid annotation anchor", http.StatusBadRequest)
		return
	}
	if err := reviewstore.SetAnchor(a.root, r.FormValue("thread"), anchor); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) review(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, 64<<10); err != nil {
		http.Error(w, "Invalid request body.", http.StatusBadRequest)
		return
	}
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	index, validation, err := saga.LoadMutationIndex(a.root)
	if err != nil || !validation.Valid {
		http.Error(w, "The saga could not be loaded. Run change-saga validate for details.", http.StatusConflict)
		return
	}
	target := r.FormValue("target")
	dir, ok := index.ReviewTargets[target]
	if !ok {
		http.Error(w, "review target does not exist", http.StatusBadRequest)
		return
	}
	reviewTarget := dir
	if index.Manifest.Version == saga.SlideSagaVersion {
		reviewTarget = target
	}
	if err := reviewstore.AddReview(a.root, reviewTarget, r.FormValue("state"), r.FormValue("body")); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	if r.Header.Get("X-Change-Saga-Async") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) diffReview(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r, 64<<10); err != nil {
		http.Error(w, "Invalid request body.", http.StatusBadRequest)
		return
	}
	if !a.validMutationToken(r) {
		http.Error(w, "Missing or invalid mutation token.", http.StatusForbidden)
		return
	}
	if err := reviewstore.AddDiffReview(a.root, r.FormValue("uri"), r.FormValue("state")); err != nil {
		writeMutationError(w)
		return
	}
	if !a.publishReviewsAfterMutation(r.Context()) {
		w.Header().Set("X-Change-Saga-Review-State", "reload-pending")
	}
	fallback := "/?view=code#" + url.PathEscape(r.FormValue("file"))
	if reference, err := diffuri.Parse(r.FormValue("uri")); err == nil && reference.Kind == "file" {
		fallback = CodeDiffURL(reference.Path, "")
	}
	redirectAfterReview(w, r, fallback)
}

func redirectAfterReview(w http.ResponseWriter, r *http.Request, fallback string) {
	http.Redirect(w, r, reviewRedirectDestination(r.FormValue("return_to"), fallback), http.StatusSeeOther)
}

func reviewRedirectDestination(destination, fallback string) string {
	parsed, err := url.Parse(destination)
	if err != nil || destination == "" || !strings.HasPrefix(destination, "/") || strings.HasPrefix(destination, "//") || parsed.IsAbs() || parsed.Host != "" {
		return fallback
	}
	return destination
}

const (
	maxMultipartBytes  = 32 << 20
	maxAttachmentBytes = 10 << 20
	maxAttachments     = 8
)

var (
	errUploadTooLarge = errors.New("upload too large")
	errInvalidUpload  = errors.New("invalid upload")
	attachmentTempDir string
)

func parseMultipart(r *http.Request, w http.ResponseWriter) ([]string, func(), error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)
	var paths []string
	cleanup := func() {
		removeTemporary(paths)
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		cleanup()
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return nil, func() {}, errUploadTooLarge
		}
		return nil, func() {}, errInvalidUpload
	}
	files := r.MultipartForm.File["attachment"]
	if len(files) > maxAttachments {
		cleanup()
		return nil, func() {}, errUploadTooLarge
	}
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			cleanup()
			return nil, func() {}, errInvalidUpload
		}
		ext := filepath.Ext(filepath.Base(header.Filename))
		temp, err := os.CreateTemp(attachmentTempDir, "change-saga-attachment-*"+ext)
		if err != nil {
			_ = file.Close()
			cleanup()
			return nil, func() {}, errInvalidUpload
		}
		path := temp.Name()
		paths = append(paths, path)
		written, copyErr := io.Copy(temp, io.LimitReader(file, maxAttachmentBytes+1))
		fileErr := file.Close()
		closeErr := temp.Close()
		if written > maxAttachmentBytes {
			cleanup()
			return nil, func() {}, errUploadTooLarge
		}
		if copyErr != nil || fileErr != nil || closeErr != nil || !validUploadedContent(path, header.Filename) {
			cleanup()
			return nil, func() {}, errInvalidUpload
		}
	}
	return paths, cleanup, nil
}

func validUploadedContent(path, filename string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var sample [512]byte
	n, err := file.Read(sample[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	detected := http.DetectContentType(sample[:n])
	if parsed, _, err := mime.ParseMediaType(detected); err == nil {
		detected = parsed
	}
	declared := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if parsed, _, err := mime.ParseMediaType(declared); err == nil {
		declared = parsed
	}
	if declared == "image/svg+xml" {
		lower := strings.ToLower(string(sample[:n]))
		return (detected == "text/plain" || detected == "text/xml") && strings.Contains(lower, "<svg")
	}
	if strings.HasPrefix(declared, "image/") {
		return detected == declared
	}
	if declared == "text/html" {
		return detected == "text/html"
	}
	if declared == "text/plain" || declared == "text/markdown" {
		return detected == "text/plain"
	}
	return false
}

func removeTemporary(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func parseForm(w http.ResponseWriter, r *http.Request, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return r.ParseForm()
}

func (a *app) validMutationToken(r *http.Request) bool {
	if a.mutationToken == "" {
		return true
	}
	provided := r.Header.Get("X-Change-Saga-Mutation-Token")
	if provided == "" {
		provided = r.FormValue("mutation_token")
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(a.mutationToken)) == 1
}

func writeMutationError(w http.ResponseWriter) {
	http.Error(w, "The review request was invalid or the saga could not be updated. Run change-saga validate and try again.", http.StatusBadRequest)
}

func writeMultipartError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUploadTooLarge) {
		http.Error(w, "Upload exceeds the allowed size or file count.", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "Upload must be a supported image, HTML, Markdown, or plain-text file.", http.StatusBadRequest)
}

func findFragment(document *saga.Saga, id string) *saga.Fragment {
	var found *saga.Fragment
	matches := 0
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for _, fragment := range section.Fragments {
			if fragment.ID == id {
				found = fragment
				matches++
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	for _, thread := range document.Threads {
		for _, message := range thread.Messages {
			for _, fragment := range message.Fragments {
				if fragment.ID == id {
					found = fragment
					matches++
				}
			}
		}
	}
	if matches != 1 {
		return nil
	}
	return found
}

func targetExists(document *saga.Saga, target string) bool {
	if target == saga.SagaTarget(document.Manifest.ID) {
		return true
	}
	found := false
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Target == target {
			found = true
		}
		for _, fragment := range section.Fragments {
			if fragment.Target == target {
				found = true
			}
			for index := range fragment.Landmarks {
				if fragment.Landmarks[index].Target == target {
					found = true
				}
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	for _, thread := range document.Threads {
		for _, message := range thread.Messages {
			for _, fragment := range message.Fragments {
				if fragment.Target == target {
					found = true
				}
			}
		}
	}
	return found
}

func findTargetDirectory(document *saga.Saga, target string) string {
	if target == saga.SagaTarget(document.Manifest.ID) {
		return document.Root
	}
	result := ""
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Target == target {
			result = filepath.Join(document.Root, filepath.FromSlash(section.Path))
		}
		for _, fragment := range section.Fragments {
			if fragment.Target == target {
				result = fragment.Directory
			}
			for index := range fragment.Landmarks {
				landmark := &fragment.Landmarks[index]
				if landmark.Target == target {
					result = landmark.Directory
				}
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return result
}

func pathEscapeParts(path string) []string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return parts
}

func hasReservedPart(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, "___") || part == "fragment.json" {
			return true
		}
	}
	return false
}

func domID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("target-%s-%x", store.Slug(value), digest[:6])
}

func sagaHref(target string) string { return "#" + domID(target) }

const defaultAnnotationColor = "#d04832"

// Sticky notes default to the warm amber already used for landmark highlights so
// a placed note reads as paper rather than as a drawing stroke.
const defaultNoteColor = "#f2bd4b"

func validAnnotationColor(value string) bool { return saga.ValidAnnotationColor(value) }

func markdown(source string) template.HTML {
	return markdownWithAnchors(source, "heading")
}

func launchBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		command, args = "xdg-open", []string{target}
	}
	return exec.Command(command, args...).Start()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(r.URL.Path, "/f/") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		}
		next.ServeHTTP(w, r)
	})
}
