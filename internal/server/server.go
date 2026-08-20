package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
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
	"time"

	"github.com/review-saga/review-saga/internal/coverage"
	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitattribution"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/reviewstore"
	"github.com/review-saga/review-saga/internal/saga"
	"github.com/review-saga/review-saga/internal/store"
)

type app struct {
	root      string
	sourceDir string
	template  *template.Template
}

type pageData struct {
	Saga          *saga.Saga
	Root          *sectionView
	Nav           []*navNodeView
	Chapters      []*chapterIndexView
	Overview      bool
	Chapter       bool
	Diagnostic    string
	Code          *CodeReviewView
	Manifest      *CoverageManifestView
	Error         string
	Files         []*fileDiffView
	ReviewedFiles int
	ReviewDecided int
	ReviewTotal   int
	ReviewItems   []*reviewProgressItem
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

type chapterIndexView struct {
	ID         string
	Title      string
	URL        string
	Status     string
	StateClass string
	StateIcon  string
	Active     bool
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
	DOMID         string
	Changes       []*diffAtomView
	Attached      *attachedCodeView
	Threads       []*threadView
	FragmentViews []*fragmentView
	ChildViews    []*sectionView
	ReviewState   string
	ReviewAuthor  string
	ReviewDetail  string
	ReviewBody    string
}

type fragmentView struct {
	*saga.Fragment
	DOMID         string
	URL           string
	Markdown      template.HTML
	Plain         string
	Interactive   bool
	Image         bool
	AspectRatio   string
	LandmarkViews []*landmarkView
	Changes       []*diffAtomView
	Attached      *attachedCodeView
	Threads       []*threadView
	ReviewState   string
	ReviewAuthor  string
	ReviewDetail  string
	ReviewBody    string
}

type landmarkView struct {
	saga.Landmark
	DOMID    string
	Title    string
	Changes  []*diffAtomView
	Attached *attachedCodeView
	Threads  []*threadView
	Region   *saga.LandmarkRegion
}

type diffAtomView struct {
	gitdiff.Atom
	Threads  []*threadView
	Target   string
	Href     string
	Selected bool
}

type fileDiffView = FileDiffView

type threadView struct {
	*saga.Thread
	MessageViews [][]*fragmentView
	StateAuthor  string
	StateDetail  string
}

func Listen(ctx context.Context, root, sourceDir, addr string, openBrowser bool, out io.Writer) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if _, validation, err := saga.Load(abs); err != nil {
		return err
	} else if !validation.Valid {
		return fmt.Errorf("saga is structurally invalid; run saga validate")
	}
	if sourceDir == "" {
		sourceDir = abs
	}
	tmpl, err := newPageTemplate()
	if err != nil {
		return err
	}
	application := &app{root: abs, sourceDir: sourceDir, template: tmpl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chapters/{chapter}", application.page)
	mux.HandleFunc("GET /", application.page)
	mux.HandleFunc("GET /app.js", application.javascript)
	mux.HandleFunc("GET /f/{id}/{path...}", application.fragmentFile)
	mux.HandleFunc("POST /api/thread", application.createThread)
	mux.HandleFunc("POST /api/reply", application.reply)
	mux.HandleFunc("POST /api/thread-state", application.threadState)
	mux.HandleFunc("POST /api/thread-anchor", application.threadAnchor)
	mux.HandleFunc("POST /api/review", application.review)
	mux.HandleFunc("POST /api/diff-review", application.diffReview)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	server := &http.Server{Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second}
	serverURL := "http://" + listener.Addr().String()
	if host, port, err := net.SplitHostPort(listener.Addr().String()); err == nil && (host == "127.0.0.1" || host == "::1") {
		serverURL = "http://127.0.0.1:" + port
	}
	fmt.Fprintf(out, "Review Saga is available at %s\nPress Ctrl-C to stop.\n", serverURL)
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
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
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
	document, validation, err := saga.Load(a.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Review identity belongs to the repository containing the saga, which can
	// be different from the source checkout used to evaluate product diffs.
	applyGitAttribution(r.Context(), gitattribution.New(r.Context(), a.root), document)
	changes, diffErr := gitdiff.Read(r.Context(), a.sourceDir, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
	var report coverage.Report
	if diffErr == nil {
		report = coverage.Evaluate(document, validation, changes)
	}
	changesByTarget := map[string][]gitdiff.Atom{}
	for _, atom := range changes.Atoms {
		seen := map[string]bool{}
		for _, owner := range report.Ownership[atom.Key] {
			if !seen[owner.Target] {
				changesByTarget[owner.Target] = append(changesByTarget[owner.Target], atom)
				seen[owner.Target] = true
			}
		}
	}
	threadsByTarget := map[string][]*threadView{}
	threadsByDiff := map[string][]*threadView{}
	for _, thread := range document.Threads {
		if thread.State == "withdrawn" {
			continue
		}
		view := makeThreadView(thread)
		if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil {
			threadsByDiff[thread.Anchor.Diff.URI] = append(threadsByDiff[thread.Anchor.Diff.URI], view)
		} else {
			threadsByTarget[thread.Target] = append(threadsByTarget[thread.Target], view)
		}
	}
	code, selectionErr := makeCodeReviewView(document, changes, report, threadsByDiff, codeSelectionFromRequest(r))
	if selectionErr != nil && diffErr == nil {
		http.Error(w, selectionErr.Error(), selectionErr.status)
		return
	}
	if code != nil {
		rebaseCodeReviewURLs(code, r.URL.Path)
	}
	rootView := makeSectionView(document.Section, changesByTarget, threadsByTarget, threadsByDiff)
	chapterID, chapterRoute := requestedChapter(r)
	if r.URL.Path != "/" && !chapterRoute {
		http.NotFound(w, r)
		return
	}
	selected := rootView
	if chapterRoute {
		selected = nil
		for _, child := range rootView.ChildViews {
			if child.Kind == "chapter" && child.ID == chapterID {
				selected = child
				break
			}
		}
		if selected == nil {
			http.NotFound(w, r)
			return
		}
	} else {
		// The overview owns only root-level content. Chapter bodies are rendered
		// exclusively by their stable chapter routes.
		selected = cloneSectionWithoutChildren(rootView)
	}
	data := pageData{
		Saga: document,
		Root: selected, Overview: !chapterRoute, Chapter: chapterRoute,
		Chapters: makeChapterIndex(rootView, chapterID),
		Code:     code,
	}
	data.ReviewItems = makeReviewProgressItems(rootView)
	data.ReviewDecided, data.ReviewTotal = reviewProgressSummary(data.ReviewItems)
	data.Nav = makeNavTree(document.Manifest.Title, rootView, selected, chapterRoute, chapterID)
	if diffErr == nil {
		data.Manifest = makeCoverageManifestView(document, changes, report)
	}
	if code != nil {
		data.Files, data.ReviewedFiles = code.Files, code.ReviewedFiles
	}
	if diffErr != nil {
		data.Error = "The source comparison could not be loaded. Run saga validate for diagnostic details."
	} else if !report.Complete {
		data.Diagnostic = "Review is blocked because this saga does not account for every source change. Run saga validate for diagnostic details."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.template.ExecuteTemplate(w, "page", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func cloneSectionWithoutChildren(view *sectionView) *sectionView {
	clone := *view
	clone.ChildViews = nil
	return &clone
}

func makeChapterIndex(root *sectionView, activeID string) []*chapterIndexView {
	chapters := make([]*chapterIndexView, 0, len(root.ChildViews))
	for _, child := range root.ChildViews {
		if child.Kind != "chapter" {
			continue
		}
		status, class, icon := reviewProgress(child)
		chapters = append(chapters, &chapterIndexView{
			ID: child.ID, Title: child.Title, URL: "/chapters/" + url.PathEscape(child.ID),
			Status: status, StateClass: class, StateIcon: icon, Active: child.ID == activeID,
		})
	}
	return chapters
}

// reviewProgress reports resume state for one chapter. It is deliberately
// coarse: reviewers need to know where to continue, not a completion score.
func reviewProgress(view *sectionView) (status, class, icon string) {
	if view.ReviewState == "approved" {
		return "Approved", "approved", "check"
	}
	if sectionHasActivity(view) {
		return "In progress", "progress", "half"
	}
	return "Unreviewed", "", "circle"
}

// makeNavTree builds the collapsed documentation tree in the sidebar: the
// overview, then every chapter. Only the open page expands, and it expands into
// its own headings so a long chapter stays navigable.
func makeNavTree(title string, root, selected *sectionView, chapterRoute bool, activeID string) []*navNodeView {
	overview := &navNodeView{Title: "Overview", Href: "/", NodeID: "nav-overview", Active: !chapterRoute}
	if !chapterRoute && selected != nil {
		overview.Children = withoutRedundantLead(documentOutline(selected), overview.Title)
		overview.Expanded = len(overview.Children) > 0
	}
	nodes := []*navNodeView{overview}
	for _, child := range root.ChildViews {
		if child.Kind != "chapter" {
			continue
		}
		status, class, icon := reviewProgress(child)
		node := &navNodeView{
			Title: child.Title, Href: "/chapters/" + url.PathEscape(child.ID),
			NodeID: "nav-" + domID(child.Target), Active: chapterRoute && child.ID == activeID,
			StateLabel: status, StateClass: class, StateIcon: icon,
		}
		if node.Active && selected != nil {
			node.Children = withoutRedundantLead(documentOutline(selected), node.Title)
			node.Expanded = len(node.Children) > 0
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// documentOutline turns the open page into headings a reader recognises. Titled
// content becomes an entry; untitled content is skipped rather than exposed
// under an internal identifier.
func documentOutline(view *sectionView) []*navNodeView {
	var nodes []*navNodeView
	for _, fragment := range view.FragmentViews {
		// A lead-in that repeats the page title is not a separate destination.
		if fragment.Title == "" || strings.EqualFold(fragment.Title, view.Title) {
			continue
		}
		nodes = append(nodes, &navNodeView{Title: fragment.Title, Href: "#" + fragment.DOMID, NodeID: "nav-" + fragment.DOMID})
	}
	for _, child := range view.ChildViews {
		if child.Title == "" {
			continue
		}
		node := &navNodeView{Title: child.Title, Href: "#" + child.DOMID, NodeID: "nav-" + child.DOMID}
		node.Children = documentOutline(child)
		node.Expanded = len(node.Children) > 0
		nodes = append(nodes, node)
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

func sectionHasActivity(view *sectionView) bool {
	if view.ReviewState != "" || len(view.Threads) > 0 {
		return true
	}
	for _, fragment := range view.FragmentViews {
		if fragment.ReviewState != "" || len(fragment.Threads) > 0 {
			return true
		}
	}
	for _, child := range view.ChildViews {
		if sectionHasActivity(child) {
			return true
		}
	}
	return false
}

func makeReviewProgressItems(root *sectionView) []*reviewProgressItem {
	if root == nil {
		return nil
	}
	var result []*reviewProgressItem
	var walk func(*sectionView, string)
	walk = func(view *sectionView, base string) {
		if view == nil {
			return
		}
		title := view.Title
		if title == "" {
			title = view.ID
		}
		result = append(result, makeReviewProgressItem(view.Target, title, base+"#"+view.DOMID, view.ReviewState, view.ReviewBody))
		for _, fragment := range view.FragmentViews {
			fragmentTitle := fragment.Title
			if fragmentTitle == "" {
				fragmentTitle = fragment.ID
			}
			result = append(result, makeReviewProgressItem(fragment.Target, fragmentTitle, base+"#"+fragment.DOMID, fragment.ReviewState, fragment.ReviewBody))
		}
		for _, child := range view.ChildViews {
			childBase := base
			if child.Kind == "chapter" {
				childBase = "/chapters/" + url.PathEscape(child.ID)
			}
			walk(child, childBase)
		}
	}
	walk(root, "/")
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

func makeSectionView(section *saga.Section, changes map[string][]gitdiff.Atom, threads map[string][]*threadView, diffThreads map[string][]*threadView) *sectionView {
	view := &sectionView{
		Section: section, DOMID: domID(section.Target), Changes: makeAtomViews(changes[section.Target], section.Target, diffThreads),
		Attached: makeAttachedCodeView(section.Title, section.Target, changes[section.Target], section.Diffs, diffThreads), Threads: threads[section.Target],
	}
	view.ReviewState, view.ReviewAuthor, view.ReviewDetail, view.ReviewBody = latestReview(section.Reviews)
	for _, fragment := range section.Fragments {
		view.FragmentViews = append(view.FragmentViews, makeFragmentView(fragment, changes[fragment.Target], threads[fragment.Target], diffThreads, changes, threads))
	}
	for _, child := range section.Children {
		view.ChildViews = append(view.ChildViews, makeSectionView(child, changes, threads, diffThreads))
	}
	return view
}

func makeFragmentView(fragment *saga.Fragment, changes []gitdiff.Atom, threads []*threadView, diffThreads map[string][]*threadView, changesByTarget map[string][]gitdiff.Atom, threadsByTarget map[string][]*threadView) *fragmentView {
	title := fragment.Title
	if title == "" {
		title = fragment.ID
	}
	view := &fragmentView{
		Fragment: fragment, DOMID: domID(fragment.Target), Changes: makeAtomViews(changes, fragment.Target, diffThreads),
		Attached: makeAttachedCodeView(title, fragment.Target, changes, fragment.Diffs, diffThreads), Threads: threads,
	}
	for _, landmark := range fragment.Landmarks {
		region := landmark.Hotspot
		if region == nil && landmark.Selector.Type == "region" {
			region = &saga.LandmarkRegion{X: landmark.Selector.X, Y: landmark.Selector.Y, Width: landmark.Selector.Width, Height: landmark.Selector.Height}
		}
		landmarkChanges := changesByTarget[landmark.Target]
		view.LandmarkViews = append(view.LandmarkViews, &landmarkView{
			Landmark: landmark, DOMID: view.DOMID + "--" + landmark.ID, Title: landmark.Label,
			Changes:  makeAtomViews(landmarkChanges, landmark.Target, diffThreads),
			Attached: makeAttachedCodeView(landmark.Label, landmark.Target, landmarkChanges, landmark.Diffs, diffThreads),
			Threads:  threadsByTarget[landmark.Target], Region: region,
		})
	}
	view.ReviewState, view.ReviewAuthor, view.ReviewDetail, view.ReviewBody = latestReview(fragment.Reviews)
	view.URL = "/f/" + url.PathEscape(fragment.ID) + "/" + strings.Join(pathEscapeParts(filepath.ToSlash(fragment.Entrypoint)), "/")
	switch fragment.MediaType {
	case "text/markdown":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil {
			view.Markdown = markdownWithAnchors(string(data), view.DOMID)
		}
	case "text/plain":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil {
			view.Plain = string(data)
		}
	case "text/html", "image/svg+xml":
		view.Interactive = true
		if fragment.MediaType == "image/svg+xml" {
			if data, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil {
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
			fragments = append(fragments, makeFragmentView(fragment, nil, nil, nil, nil, nil))
		}
		view.MessageViews = append(view.MessageViews, fragments)
	}
	return view
}

func makeAtomViews(atoms []gitdiff.Atom, target string, threads map[string][]*threadView) []*diffAtomView {
	views := make([]*diffAtomView, 0, len(atoms))
	for _, atom := range atoms {
		views = append(views, &diffAtomView{Atom: atom, Threads: threads[atom.URI], Target: target})
	}
	return views
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
		if previous, ok := latest[review.URI]; !ok || previous.CreatedAt.Before(review.CreatedAt) {
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
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
	last := values[len(values)-1]
	return last.State, last.Author, last.AttributionDetail, last.Body
}

func applyGitAttribution(ctx context.Context, resolver *gitattribution.Resolver, document *saga.Saga) {
	apply := func(path string, author *string, detail *string) {
		value := resolver.Resolve(ctx, path)
		switch value.State {
		case gitattribution.Committed:
			*author = value.Name
			commitID := value.CommitID
			if len(commitID) > 12 {
				commitID = commitID[:12]
			}
			*detail = fmt.Sprintf("%s · committed %s · %s", value.Email, value.CommittedAt.Format("2006-01-02 15:04 MST"), commitID)
		case gitattribution.Uncommitted:
			*author = "Local / uncommitted"
			*detail = "This review event has not been committed yet."
		case gitattribution.Rewritten:
			*author = "History rewritten"
			*detail = "Git history no longer contains the commit that introduced this review event. Stored legacy identity is not authoritative."
		default:
			*author = "Git history unavailable"
			*detail = "Git attribution is unavailable. Stored legacy identity is not authoritative."
		}
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		for index := range section.Reviews {
			review := &section.Reviews[index]
			apply(review.Path, &review.Author, &review.AttributionDetail)
		}
		for _, fragment := range section.Fragments {
			for index := range fragment.Reviews {
				review := &fragment.Reviews[index]
				apply(review.Path, &review.Author, &review.AttributionDetail)
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	for _, thread := range document.Threads {
		apply(filepath.Join(thread.Directory, "thread.json"), &thread.CreatedBy, &thread.AttributionDetail)
		for _, message := range thread.Messages {
			apply(message.Path, &message.Author, &message.AttributionDetail)
		}
		for index := range thread.Events {
			event := &thread.Events[index]
			apply(event.Path, &event.Author, &event.AttributionDetail)
		}
	}
	for index := range document.DiffReviews {
		review := &document.DiffReviews[index]
		apply(review.Path, &review.Author, &review.AttributionDetail)
	}
}

func (a *app) fragmentFile(w http.ResponseWriter, r *http.Request) {
	document, _, err := saga.Load(a.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fragment := findFragment(document, r.PathValue("id"))
	if fragment == nil {
		http.NotFound(w, r)
		return
	}
	rel := filepath.Clean(filepath.FromSlash(r.PathValue("path")))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || hasReservedPart(rel) {
		http.Error(w, "invalid fragment path", http.StatusBadRequest)
		return
	}
	path := filepath.Join(fragment.Directory, rel)
	realRoot, rootErr := filepath.EvalSymlinks(fragment.Directory)
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

func (a *app) createThread(w http.ResponseWriter, r *http.Request) {
	attachments, err := parseMultipart(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer removeTemporary(attachments)
	document, _, err := saga.Load(a.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target := r.FormValue("target")
	if !targetExists(document, target) {
		http.Error(w, "target does not exist", http.StatusBadRequest)
		return
	}
	var anchor saga.Anchor
	if err := json.Unmarshal([]byte(r.FormValue("anchor")), &anchor); err != nil {
		http.Error(w, "invalid annotation anchor", http.StatusBadRequest)
		return
	}
	if _, err := reviewstore.AddThread(a.root, target, r.FormValue("body"), anchor, r.FormValue("kind"), r.FormValue("replacement"), attachments); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectAfterReview(w, r, "/#"+domID(target))
}

func (a *app) reply(w http.ResponseWriter, r *http.Request) {
	attachments, err := parseMultipart(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer removeTemporary(attachments)
	if _, err := reviewstore.AddReply(a.root, r.FormValue("thread"), r.FormValue("body"), attachments); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) threadState(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := reviewstore.SetState(a.root, r.FormValue("thread"), r.FormValue("state")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) threadAnchor(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var anchor saga.Anchor
	if err := json.Unmarshal([]byte(r.FormValue("anchor")), &anchor); err != nil {
		http.Error(w, "invalid annotation anchor", http.StatusBadRequest)
		return
	}
	if err := reviewstore.SetAnchor(a.root, r.FormValue("thread"), anchor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) review(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	document, _, err := saga.Load(a.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dir := findTargetDirectory(document, r.FormValue("target"))
	if dir == "" {
		http.Error(w, "review target does not exist", http.StatusBadRequest)
		return
	}
	if err := reviewstore.AddReview(a.root, dir, r.FormValue("state"), r.FormValue("body")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Header.Get("X-Review-Saga-Async") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	redirectAfterReview(w, r, "/#"+domID(r.FormValue("target")))
}

func (a *app) diffReview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := reviewstore.AddDiffReview(a.root, r.FormValue("uri"), r.FormValue("state")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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

func parseMultipart(r *http.Request, w http.ResponseWriter) ([]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		return nil, err
	}
	files := r.MultipartForm.File["attachment"]
	var paths []string
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			removeTemporary(paths)
			return nil, err
		}
		ext := filepath.Ext(filepath.Base(header.Filename))
		temp, err := os.CreateTemp("", "review-saga-attachment-*"+ext)
		if err != nil {
			file.Close()
			removeTemporary(paths)
			return nil, err
		}
		_, copyErr := io.Copy(temp, io.LimitReader(file, 10<<20))
		file.Close()
		closeErr := temp.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(temp.Name())
			removeTemporary(paths)
			if copyErr != nil {
				return nil, copyErr
			}
			return nil, closeErr
		}
		paths = append(paths, temp.Name())
	}
	return paths, nil
}

func removeTemporary(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
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

const defaultAnnotationColor = "#d04832"

// Sticky notes default to the warm amber already used for landmark highlights so
// a placed note reads as paper rather than as a drawing stroke.
const defaultNoteColor = "#f2bd4b"

func validAnnotationColor(value string) bool { return saga.ValidAnnotationColor(value) }

func markdown(source string) template.HTML {
	return markdownWithAnchors(source, "heading")
}

func markdownWithAnchors(source, namespace string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var out strings.Builder
	inCode, inList := false, false
	anchors := map[string]int{}
	closeList := func() {
		if inList {
			out.WriteString("</ul>")
			inList = false
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			closeList()
			if inCode {
				out.WriteString("</code></pre>")
			} else {
				out.WriteString("<pre><code>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			out.WriteString(html.EscapeString(line))
			out.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			closeList()
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>")
			out.WriteString(html.EscapeString(strings.TrimSpace(trimmed[2:])))
			out.WriteString("</li>")
			continue
		}
		closeList()
		if heading, ok := saga.ParseMarkdownHeading(trimmed); ok {
			anchor := heading.Anchor
			if !heading.Explicit || !saga.ValidMarkdownAnchor(anchor) {
				anchor = store.Slug(heading.Text)
			}
			anchors[anchor]++
			if anchors[anchor] > 1 {
				anchor += "-" + strconv.Itoa(anchors[anchor])
			}
			id := namespace + "--" + anchor
			htmlLevel := min(heading.Level+2, 6)
			fmt.Fprintf(&out, `<h%d id="%s" class="fragment-heading"><span>%s</span><a class="permalink heading-permalink" href="#%s" data-copy-link="#%s" aria-label="Copy link to %s">#</a></h%d>`, htmlLevel, html.EscapeString(id), html.EscapeString(heading.Text), html.EscapeString(id), html.EscapeString(id), html.EscapeString(heading.Text), htmlLevel)
			continue
		}
		out.WriteString("<p>")
		out.WriteString(html.EscapeString(trimmed))
		out.WriteString("</p>")
	}
	closeList()
	if inCode {
		out.WriteString("</code></pre>")
	}
	return template.HTML(out.String()) // #nosec G203 -- all source text was escaped above.
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
