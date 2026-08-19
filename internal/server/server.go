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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/review-saga/review-saga/internal/coverage"
	"github.com/review-saga/review-saga/internal/diffuri"
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
	Chapters      []*chapterIndexView
	Overview      bool
	Chapter       bool
	Diagnostic    string
	Code          *CodeReviewView
	Error         string
	Files         []*fileDiffView
	ReviewedFiles int
}

type chapterIndexView struct {
	ID      string
	Title   string
	URL     string
	Summary string
	Status  string
	Action  string
	Active  bool
}

type sectionView struct {
	*saga.Section
	DOMID             string
	Changes           []*diffAtomView
	Threads           []*threadView
	FragmentViews     []*fragmentView
	ChildViews        []*sectionView
	ReviewState       string
	ReviewAttribution saga.Attribution
}

type fragmentView struct {
	*saga.Fragment
	DOMID             string
	URL               string
	Markdown          template.HTML
	Plain             string
	Interactive       bool
	Image             bool
	Changes           []*diffAtomView
	Threads           []*threadView
	ReviewState       string
	ReviewAttribution saga.Attribution
}

type diffAtomView struct {
	gitdiff.Atom
	Threads []*threadView
	Target  string
	Href    string
}

type fileDiffView = FileDiffView

type threadView struct {
	*saga.Thread
	MessageViews [][]*fragmentView
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
	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"markdown":    markdown,
		"attribution": attributionLabel,
		"coord":       func(value float64) string { return strconv.FormatFloat(value*1000, 'f', 2, 64) },
		"points": func(values []saga.Point) string {
			parts := make([]string, 0, len(values))
			for _, point := range values {
				parts = append(parts, fmt.Sprintf("%.2f,%.2f", point.X*1000, point.Y*1000))
			}
			return strings.Join(parts, " ")
		},
	}).Parse(pageTemplate)
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

func (a *app) page(w http.ResponseWriter, r *http.Request) {
	document, validation, err := saga.Load(a.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
		sections, fragments := sectionSize(child)
		status := "Unreviewed"
		action := "Open"
		if child.ReviewState == "approved" {
			status = "Approved"
		} else if sectionHasActivity(child) {
			status = "In progress"
			action = "Resume"
		}
		parts := make([]string, 0, 2)
		if sections > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", sections, plural(sections, "section", "sections")))
		}
		parts = append(parts, fmt.Sprintf("%d %s", fragments, plural(fragments, "fragment", "fragments")))
		chapters = append(chapters, &chapterIndexView{
			ID: child.ID, Title: child.Title, URL: "/chapters/" + url.PathEscape(child.ID),
			Summary: strings.Join(parts, " · "), Status: status, Action: action, Active: child.ID == activeID,
		})
	}
	return chapters
}

func sectionSize(view *sectionView) (int, int) {
	sections := len(view.ChildViews)
	fragments := len(view.FragmentViews)
	for _, child := range view.ChildViews {
		childSections, childFragments := sectionSize(child)
		sections += childSections
		fragments += childFragments
	}
	return sections, fragments
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

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func makeSectionView(section *saga.Section, changes map[string][]gitdiff.Atom, threads map[string][]*threadView, diffThreads map[string][]*threadView) *sectionView {
	view := &sectionView{Section: section, DOMID: domID(section.Target), Changes: makeAtomViews(changes[section.Target], section.Target, diffThreads), Threads: threads[section.Target]}
	if len(section.Reviews) > 0 {
		reviews := append([]saga.Review(nil), section.Reviews...)
		sort.Slice(reviews, func(i, j int) bool { return reviews[i].CreatedAt.Before(reviews[j].CreatedAt) })
		last := reviews[len(reviews)-1]
		view.ReviewState, view.ReviewAttribution = last.State, last.Attribution
	}
	for _, fragment := range section.Fragments {
		view.FragmentViews = append(view.FragmentViews, makeFragmentView(fragment, changes[fragment.Target], threads[fragment.Target], diffThreads))
	}
	for _, child := range section.Children {
		view.ChildViews = append(view.ChildViews, makeSectionView(child, changes, threads, diffThreads))
	}
	return view
}

func makeFragmentView(fragment *saga.Fragment, changes []gitdiff.Atom, threads []*threadView, diffThreads map[string][]*threadView) *fragmentView {
	view := &fragmentView{Fragment: fragment, DOMID: domID(fragment.Target), Changes: makeAtomViews(changes, fragment.Target, diffThreads), Threads: threads}
	view.ReviewState, view.ReviewAttribution = latestReview(fragment.Reviews)
	view.URL = "/f/" + url.PathEscape(fragment.ID) + "/" + strings.Join(pathEscapeParts(filepath.ToSlash(fragment.Entrypoint)), "/")
	switch fragment.MediaType {
	case "text/markdown":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil {
			view.Markdown = markdown(string(data))
		}
	case "text/plain":
		if data, err := os.ReadFile(filepath.Join(fragment.Directory, fragment.Entrypoint)); err == nil {
			view.Plain = string(data)
		}
	case "text/html", "image/svg+xml":
		view.Interactive = true
	default:
		view.Image = strings.HasPrefix(fragment.MediaType, "image/")
	}
	return view
}

func makeThreadView(thread *saga.Thread) *threadView {
	view := &threadView{Thread: thread}
	for _, message := range thread.Messages {
		var fragments []*fragmentView
		for _, fragment := range message.Fragments {
			fragments = append(fragments, makeFragmentView(fragment, nil, nil, nil))
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
				file.Reviewed, file.ReviewAttribution = review.State == "reviewed", review.Attribution
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

func latestReview(reviews []saga.Review) (string, saga.Attribution) {
	if len(reviews) == 0 {
		return "", saga.Attribution{}
	}
	values := append([]saga.Review(nil), reviews...)
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
	last := values[len(values)-1]
	return last.State, last.Attribution
}

func attributionLabel(value saga.Attribution) string {
	switch value.Status {
	case saga.AttributionCommitted:
		if value.Committer == nil || value.CommittedAt == nil {
			return "Git history unavailable"
		}
		commit := value.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		return fmt.Sprintf("%s <%s> · %s · commit %s", value.Committer.Name, value.Committer.Email, value.CommittedAt.Format(time.RFC3339), commit)
	case saga.AttributionUncommitted:
		return "Local, uncommitted"
	default:
		return "Git history unavailable"
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
	destination := r.FormValue("return_to")
	parsed, err := url.Parse(destination)
	if err != nil || destination == "" || !strings.HasPrefix(destination, "/") || strings.HasPrefix(destination, "//") || parsed.IsAbs() || parsed.Host != "" {
		destination = fallback
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
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

func domID(value string) string { return "target-" + store.Slug(value) }

func markdown(source string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var out strings.Builder
	inCode, inList := false, false
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
		level := 0
		for level < len(trimmed) && level < 4 && trimmed[level] == '#' {
			level++
		}
		if level > 0 && len(trimmed) > level && trimmed[level] == ' ' {
			fmt.Fprintf(&out, "<h%d>%s</h%d>", level+2, html.EscapeString(strings.TrimSpace(trimmed[level:])), level+2)
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
