package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/review-saga/review-saga/internal/coverage"
	"github.com/review-saga/review-saga/internal/diffuri"
	"github.com/review-saga/review-saga/internal/gitdiff"
	"github.com/review-saga/review-saga/internal/reviewstore"
	"github.com/review-saga/review-saga/internal/saga"
	reviewserver "github.com/review-saga/review-saga/internal/server"
	"github.com/review-saga/review-saga/internal/store"
)

// Version, Commit, and BuildDate describe the running binary. Release builds
// overwrite them with -ldflags -X; local builds keep the -dev default.
var (
	Version   = "0.2.0-dev"
	Commit    = ""
	BuildDate = ""
)

// VersionString renders the version plus whatever build metadata was injected.
func VersionString() string {
	out := Version
	if Commit != "" {
		out += " (" + Commit + ")"
	}
	if BuildDate != "" {
		out += " built " + BuildDate
	}
	return out
}

type StatusError struct{ Code int }

func (e *StatusError) Error() string { return "command reported a non-success status" }

func PrintHelp(out io.Writer) {
	fmt.Fprint(out, `Review Saga — make every part of a large change reviewable

Usage:
  saga init [flags] <name.saga>
  saga add-chapter [flags] <saga> <name>
  saga add-section [flags] <saga> <section/path>
  saga add-fragment [flags] <saga>
  saga cover [flags] <saga>
  saga thread [flags] <saga>
  saga reply [flags] <saga>
  saga review [flags] <saga>
  saga validate [--json] <saga>
  saga status [--json] [--repo PATH] <saga>
  saga serve [--addr ADDR] [--repo PATH] [--open] <saga>
  saga open [--addr ADDR] [--repo PATH] <saga>
  saga spec [--json]

Run "saga <command> -h" for command-specific options.
`)
}

func Init(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(out)
	base := flags.String("base", "main", "base Git revision")
	head := flags.String("head", "HEAD", "head Git revision, or WORKTREE")
	title := flags.String("title", "", "saga title")
	id := flags.String("id", "", "stable saga identifier")
	repoDir := flags.String("repo", ".", "source repository checkout")
	repository := flags.String("repository", "", "absolute source repository URI; defaults to origin")
	prNumber := flags.Int("pr", 0, "pull request number")
	prURL := flags.String("pr-url", "", "pull request URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga init [flags] <name.saga>")
	}
	root := flags.Arg(0)
	if !strings.HasSuffix(filepath.Base(root), ".saga") {
		return fmt.Errorf("saga directory must end in .saga")
	}
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("%s already exists", root)
	} else if !os.IsNotExist(err) {
		return err
	}
	if *title == "" {
		*title = strings.TrimSuffix(filepath.Base(root), ".saga")
	}
	if *id == "" {
		*id = store.Slug(strings.TrimSuffix(filepath.Base(root), ".saga"))
	}
	if !saga.ValidID(*id) {
		return fmt.Errorf("--id must be a stable 1-128 character identifier")
	}
	if *prNumber < 0 {
		return fmt.Errorf("--pr cannot be negative")
	}
	repositoryURI, _, err := discoverRepository(ctx, *repoDir, *repository)
	if err != nil {
		return err
	}
	for _, dir := range []string{"___diffs", "___approvals", filepath.Join("___review", "threads"), filepath.Join("___review", "diffs")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return err
		}
	}
	manifest := saga.Manifest{Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: *id, Title: *title, Source: saga.Source{Repository: repositoryURI, Base: *base, Head: *head}}
	if *prNumber != 0 || *prURL != "" {
		manifest.PR = &saga.PR{Number: *prNumber, URL: *prURL}
	}
	if err := store.WriteJSON(filepath.Join(root, "saga.json"), manifest, true); err != nil {
		return err
	}
	if err := createFragment(filepath.Join(root, "overview.fragment"), saga.FragmentManifest{Version: saga.CurrentVersion, ID: *id + "-overview", Title: "Overview", MediaType: "text/markdown", Entrypoint: "content.md"}, "", []byte("Explain the change as a whole. Lead with the context that makes the rest of the saga easier to navigate.\n")); err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %s\nNext: saga add-chapter --title \"Architecture\" %s architecture\n", root, root)
	return nil
}

func AddChapter(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("add-chapter", flag.ContinueOnError)
	flags.SetOutput(out)
	title := flags.String("title", "", "chapter title")
	id := flags.String("id", "", "stable chapter identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: saga add-chapter [flags] <saga> <name>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(flags.Arg(1), ".chapter")
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Base(name) != name || strings.HasPrefix(name, "___") {
		return fmt.Errorf("chapter name must be a single non-reserved path component")
	}
	if *title == "" {
		*title = strings.ReplaceAll(name, "-", " ")
	}
	if *id == "" {
		*id = store.Slug(name)
	}
	overviewID := *id + "-overview"
	if !saga.ValidID(*id) || !saga.ValidID(overviewID) || targetIDExists(document, *id) || targetIDExists(document, overviewID) {
		return fmt.Errorf("chapter id %q or its overview id is invalid or already used", *id)
	}
	dir := filepath.Join(document.Root, store.Slug(name)+".chapter")
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("chapter %s already exists", filepath.Base(dir))
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, reserved := range []string{"___diffs", "___approvals"} {
		if err := os.MkdirAll(filepath.Join(dir, reserved), 0o755); err != nil {
			return err
		}
	}
	manifest := saga.ChapterManifest{Version: saga.CurrentVersion, ID: *id, Title: *title, Order: *order}
	if err := store.WriteJSON(filepath.Join(dir, "chapter.json"), manifest, true); err != nil {
		return err
	}
	overview := saga.FragmentManifest{Version: saga.CurrentVersion, ID: overviewID, Title: "Chapter overview", MediaType: "text/markdown", Entrypoint: "content.md"}
	content := []byte("Explain this chapter as an independently reviewable change. Describe its boundary, behavior, and risks.\n")
	if err := createFragment(filepath.Join(dir, "overview.fragment"), overview, "", content); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added chapter %s\n", filepath.Base(dir))
	return nil
}

func AddSection(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("add-section", flag.ContinueOnError)
	flags.SetOutput(out)
	title := flags.String("title", "", "section title")
	id := flags.String("id", "", "stable section identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: saga add-section [flags] <saga> <section/path>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	sectionPath := filepath.Clean(flags.Arg(1))
	parentPath, name := filepath.Dir(sectionPath), filepath.Base(sectionPath)
	if parentPath == "." || name == "." || strings.HasPrefix(name, "___") || strings.HasSuffix(name, ".chapter") || strings.HasSuffix(name, ".fragment") {
		return fmt.Errorf("sections must be created inside an existing chapter or section")
	}
	parentDir, _, err := resolveTarget(document, parentPath, false)
	if err != nil {
		return fmt.Errorf("resolve parent: %w", err)
	}
	dir := filepath.Join(parentDir, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("section %s already exists", flags.Arg(1))
	} else if !os.IsNotExist(err) {
		return err
	}
	if *title == "" {
		*title = strings.ReplaceAll(filepath.Base(dir), "-", " ")
	}
	if *id == "" {
		*id = store.Slug(strings.ReplaceAll(filepath.ToSlash(flags.Arg(1)), "/", "-"))
	}
	if !saga.ValidID(*id) || targetIDExists(document, *id) {
		return fmt.Errorf("section id %q is invalid or already used", *id)
	}
	for _, reserved := range []string{"___diffs", "___approvals"} {
		if err := os.MkdirAll(filepath.Join(dir, reserved), 0o755); err != nil {
			return err
		}
	}
	manifest := saga.SectionManifest{Version: saga.CurrentVersion, ID: *id, Title: *title, Order: *order}
	if err := store.WriteJSON(filepath.Join(dir, "section.json"), manifest, true); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added section %s\n", filepath.ToSlash(flags.Arg(1)))
	return nil
}

func AddFragment(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("add-fragment", flag.ContinueOnError)
	flags.SetOutput(out)
	section := flags.String("section", ".", "containing section")
	id := flags.String("id", "", "stable fragment identifier")
	title := flags.String("title", "", "fragment title")
	kind := flags.String("type", "markdown", "markdown, html, svg, image, or text")
	mediaType := flags.String("media-type", "", "explicit media type")
	source := flags.String("source", "", "source file to copy into the fragment")
	entrypointFlag := flags.String("entrypoint", "", "entrypoint within a source directory")
	name := flags.String("name", "", "fragment directory name without .fragment")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga add-fragment [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	sectionDir, _, err := resolveTarget(document, *section, false)
	if err != nil {
		return err
	}
	if *name == "" {
		*name = store.Slug(firstNonEmpty(*title, *id, *kind))
	}
	if *id == "" {
		*id = store.Slug(document.Manifest.ID + "-" + strings.ReplaceAll(filepath.ToSlash(*section), "/", "-") + "-" + *name)
	}
	if !saga.ValidID(*id) || targetIDExists(document, *id) {
		return fmt.Errorf("fragment id %q is invalid or already used", *id)
	}
	fragmentDir := filepath.Join(sectionDir, store.Slug(*name)+".fragment")
	if _, err := os.Stat(fragmentDir); err == nil {
		return fmt.Errorf("fragment %s already exists", fragmentDir)
	}
	entrypoint, content, resolvedType, err := fragmentContent(*kind, *mediaType, *source)
	if err != nil {
		return err
	}
	if *entrypointFlag != "" {
		if filepath.Clean(*entrypointFlag) != *entrypointFlag {
			return fmt.Errorf("entrypoint must be a normalized fragment-relative path")
		}
		entrypoint = *entrypointFlag
	}
	if filepath.IsAbs(entrypoint) || entrypoint == ".." || strings.HasPrefix(entrypoint, ".."+string(filepath.Separator)) || filepath.Clean(entrypoint) != entrypoint {
		return fmt.Errorf("entrypoint must be a normalized fragment-relative path")
	}
	if !supportedMediaType(resolvedType) {
		return fmt.Errorf("unsupported fragment media type %q", resolvedType)
	}
	manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: *id, Title: *title, MediaType: resolvedType, Entrypoint: entrypoint, Order: *order}
	if err := createFragment(fragmentDir, manifest, *source, content); err != nil {
		return err
	}
	rel, _ := filepath.Rel(document.Root, fragmentDir)
	fmt.Fprintf(out, "Added fragment %s\n", filepath.ToSlash(rel))
	return nil
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func Cover(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("cover", flag.ContinueOnError)
	flags.SetOutput(out)
	target := flags.String("target", ".", "section or .fragment directory receiving evidence")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	path := flags.String("path", "", "changed repository path")
	side := flags.String("side", "", "line side: old or new")
	lines := flags.String("lines", "", "line ranges, for example 4-9,12")
	event := flags.String("event", "", "file event: rename, mode, or binary")
	oldPath := flags.String("old-path", "", "old path for a rename event")
	newPath := flags.String("new-path", "", "new path for a rename event")
	note := flags.String("note", "", "optional explanation for report authors")
	name := flags.String("name", "", "coverage filename without .json")
	var uris stringList
	flags.Var(&uris, "uri", "absolute saga-diff URI; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga cover [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	targetDir, _, err := resolveTarget(document, *target, true)
	if err != nil {
		return err
	}
	for _, value := range uris {
		if _, err := diffuri.Parse(value); err != nil {
			return fmt.Errorf("invalid --uri: %w", err)
		}
	}
	if *path != "" || *event != "" {
		checkout := firstNonEmpty(*repoDir, document.Root)
		changes, err := gitdiff.Read(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
		if err != nil {
			return fmt.Errorf("read source diff (use --repo for a separate saga repository): %w", err)
		}
		if *event == "" {
			if *side != "old" && *side != "new" {
				return fmt.Errorf("line coverage requires --side old or new")
			}
			ranges, err := parseRanges(*lines)
			if err != nil {
				return err
			}
			for _, lineRange := range ranges {
				value, err := diffuri.Build(diffuri.Reference{Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "line", Path: filepath.ToSlash(*path), Side: *side, Start: lineRange.Start, End: lineRange.End})
				if err != nil {
					return err
				}
				uris = append(uris, value)
			}
		} else {
			value, err := diffuri.Build(diffuri.Reference{Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "event", Event: *event, Path: filepath.ToSlash(*path), OldPath: filepath.ToSlash(*oldPath), NewPath: filepath.ToSlash(*newPath)})
			if err != nil {
				return err
			}
			uris = append(uris, value)
		}
	}
	if len(uris) == 0 {
		return fmt.Errorf("provide --uri or --path/--lines (or --event)")
	}
	file := saga.DiffFile{Version: saga.CurrentVersion}
	for _, value := range uris {
		file.Diffs = append(file.Diffs, saga.DiffReference{URI: value, Note: *note})
	}
	if *name == "" {
		*name = store.Slug(firstNonEmpty(*path, *event, "diff") + "-" + store.EventID(time.Now()))
	}
	diffDir, err := store.EnsureDirWithin(document.Root, filepath.Join(targetDir, "___diffs"))
	if err != nil {
		return err
	}
	targetPath := filepath.Join(diffDir, store.Slug(*name)+".json")
	if err := store.WriteJSON(targetPath, file, true); err != nil {
		return err
	}
	rel, _ := filepath.Rel(document.Root, targetPath)
	fmt.Fprintf(out, "Added %s\n", filepath.ToSlash(rel))
	return nil
}

func Thread(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("thread", flag.ContinueOnError)
	flags.SetOutput(out)
	target := flags.String("target", ".", "section/fragment path or target URN")
	body := flags.String("body", "", "initial Markdown comment")
	anchorJSON := flags.String("anchor", `{"type":"target"}`, "anchor JSON")
	kind := flags.String("kind", "comment", "comment or suggestion")
	replacement := flags.String("replacement", "", "replacement code for a suggestion")
	var attachments stringList
	flags.Var(&attachments, "attachment", "image, SVG, HTML, or text attachment; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga thread [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	_, targetURI, err := resolveTarget(document, *target, true)
	if err != nil {
		return err
	}
	var anchor saga.Anchor
	if err := json.Unmarshal([]byte(*anchorJSON), &anchor); err != nil {
		return fmt.Errorf("parse --anchor: %w", err)
	}
	id, err := reviewstore.AddThread(document.Root, targetURI, *body, anchor, *kind, *replacement, attachments)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Created thread %s\n", id)
	return nil
}

func Reply(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("reply", flag.ContinueOnError)
	flags.SetOutput(out)
	threadID := flags.String("thread", "", "thread identifier")
	body := flags.String("body", "", "Markdown reply")
	state := flags.String("state", "", "optionally set thread to open, resolved, or withdrawn")
	var attachments stringList
	flags.Var(&attachments, "attachment", "attachment; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga reply [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	if *body != "" || len(attachments) > 0 {
		if _, err := reviewstore.AddReply(document.Root, *threadID, *body, attachments); err != nil {
			return err
		}
	}
	if *state != "" {
		if err := reviewstore.SetState(document.Root, *threadID, *state); err != nil {
			return err
		}
	}
	if *body == "" && len(attachments) == 0 && *state == "" {
		return fmt.Errorf("provide --body, --attachment, or --state")
	}
	fmt.Fprintf(out, "Updated thread %s\n", *threadID)
	return nil
}

func Review(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(out)
	target := flags.String("target", ".", "saga, chapter, section, or fragment path")
	state := flags.String("state", "", "approved, rejected, closed, or open")
	body := flags.String("body", "", "optional review note")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga review [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	targetDir, _, err := resolveTarget(document, *target, true)
	if err != nil {
		return err
	}
	if err := reviewstore.AddReview(document.Root, targetDir, *state, *body); err != nil {
		return err
	}
	fmt.Fprintf(out, "Recorded %s review for %s\n", *state, *target)
	return nil
}

func Validate(_ context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga validate [--json] <saga>")
	}
	_, validation, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeJSON(out, validation); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(out, map[bool]string{true: "Valid saga", false: "Invalid saga"}[validation.Valid])
		for _, issue := range validation.Issues {
			fmt.Fprintf(out, "  %s: %s: %s\n", issue.Severity, issue.Path, issue.Message)
		}
	}
	if !validation.Valid {
		return &StatusError{Code: 1}
	}
	return nil
}

func Status(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON")
	maxItems := flags.Int("max", 100, "maximum uncovered items in text mode; 0 means all")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga status [--json] [--repo PATH] <saga>")
	}
	report, err := buildReport(ctx, flags.Arg(0), *repoDir)
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeJSON(out, report); err != nil {
			return err
		}
	} else {
		printReport(out, report, *maxItems)
	}
	if !report.Complete {
		return &StatusError{Code: 3}
	}
	return nil
}

func buildReport(ctx context.Context, root, repoDir string) (coverage.Report, error) {
	document, validation, err := saga.Load(root)
	if err != nil {
		return coverage.Report{}, err
	}
	checkout := firstNonEmpty(repoDir, document.Root)
	changes, err := gitdiff.Read(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
	if err != nil {
		return coverage.Report{}, fmt.Errorf("read source diff (use --repo for a separate saga repository): %w", err)
	}
	return coverage.Evaluate(document, validation, changes), nil
}

func printReport(out io.Writer, report coverage.Report, maxItems int) {
	state := "INCOMPLETE"
	if report.Complete {
		state = "COMPLETE"
	}
	fmt.Fprintf(out, "%s — %d/%d product changes accounted for\n", state, report.Summary.Covered, report.Summary.Total)
	fmt.Fprintf(out, "Uncovered: %d  Overlapping: %d  Stale URIs: %d  Saga-only changes: %d\n", report.Summary.Uncovered, report.Summary.Overlapping, report.Summary.Orphaned, report.Summary.SagaChanges)
	if len(report.SchemaIssues) > 0 {
		fmt.Fprintln(out, "\nSchema issues:")
		for _, issue := range report.SchemaIssues {
			fmt.Fprintf(out, "  %s: %s: %s\n", issue.Severity, issue.Path, issue.Message)
		}
	}
	if len(report.Uncovered) > 0 {
		fmt.Fprintln(out, "\nUncovered changes:")
		limit := len(report.Uncovered)
		if maxItems > 0 && maxItems < limit {
			limit = maxItems
		}
		for _, atom := range report.Uncovered[:limit] {
			fmt.Fprintf(out, "  %s\n    %s\n", coverage.DescribeAtom(atom), atom.URI)
		}
		if limit < len(report.Uncovered) {
			fmt.Fprintf(out, "  … and %d more (use --max 0 or --json)\n", len(report.Uncovered)-limit)
		}
	}
	if len(report.Orphans) > 0 {
		fmt.Fprintln(out, "\nStale diff URIs:")
		for _, orphan := range report.Orphans {
			fmt.Fprintf(out, "  %s diff %d: %s\n", orphan.Assignment.DiffFile, orphan.Assignment.Diff, orphan.Reason)
		}
	}
}

func Serve(ctx context.Context, args []string, out io.Writer, openByDefault ...bool) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(out)
	addr := flags.String("addr", "127.0.0.1:7342", "listen address")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	openBrowser := flags.Bool("open", len(openByDefault) > 0 && openByDefault[0], "open the review in a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: saga serve [--addr ADDR] [--repo PATH] [--open] <saga>")
	}
	return reviewserver.Listen(ctx, flags.Arg(0), *repoDir, *addr, *openBrowser, out)
}

func Spec(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("spec", flag.ContinueOnError)
	flags.SetOutput(out)
	jsonOutput := flags.Bool("json", false, "emit the contract vocabulary as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: saga spec [--json]")
	}
	if *jsonOutput {
		return writeJSON(out, map[string]any{
			"version": 2, "chapter_suffix": ".chapter", "chapter_manifest": "chapter.json", "fragment_suffix": ".fragment", "fragment_manifest": "fragment.json",
			"hierarchy":     []string{"overview", "chapter", "section", "fragment"},
			"media_types":   []string{"text/markdown", "text/html", "text/plain", "image/svg+xml", "image/*"},
			"target_scheme": "urn:review-saga", "diff_scheme": "saga-diff://v1",
			"anchors":              []string{"target", "region", "drawing", "text", "note", "diff"},
			"thread_kinds":         []string{"comment", "suggestion"},
			"reserved_directories": []string{"___diffs", "___approvals", "___review"},
			"review_storage":       "append-only; one thread, message, or event record per path",
		})
	}
	fmt.Fprint(out, specText)
	return nil
}

type lineRange struct{ Start, End int }

func parseRanges(value string) ([]lineRange, error) {
	var ranges []lineRange
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, "-")
		if len(parts) > 2 {
			return nil, fmt.Errorf("invalid line range %q", raw)
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil || start < 1 {
			return nil, fmt.Errorf("invalid line range %q", raw)
		}
		end := start
		if len(parts) == 2 {
			end, err = strconv.Atoi(parts[1])
			if err != nil || end < start {
				return nil, fmt.Errorf("invalid line range %q", raw)
			}
		}
		ranges = append(ranges, lineRange{Start: start, End: end})
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("at least one line range is required")
	}
	return ranges, nil
}

func discoverRepository(ctx context.Context, repoDir, explicit string) (string, string, error) {
	rootOutput, err := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("locate source repository: %s", strings.TrimSpace(string(rootOutput)))
	}
	root := strings.TrimSpace(string(rootOutput))
	if explicit != "" {
		parsed, err := url.Parse(explicit)
		if err != nil || !parsed.IsAbs() {
			return "", "", fmt.Errorf("--repository must be an absolute URI")
		}
		return explicit, root, nil
	}
	remoteOutput, remoteErr := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin").CombinedOutput()
	if remoteErr == nil && strings.TrimSpace(string(remoteOutput)) != "" {
		return normalizeRepositoryURI(strings.TrimSpace(string(remoteOutput)), root), root, nil
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String(), root, nil
}

func normalizeRepositoryURI(value, root string) string {
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	if at := strings.Index(value, "@"); at > 0 {
		if colon := strings.Index(value[at:], ":"); colon > 0 {
			colon += at
			return "ssh://" + value[:colon] + "/" + value[colon+1:]
		}
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	abs, _ := filepath.Abs(value)
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func resolveTarget(document *saga.Saga, value string, allowFragment bool) (string, string, error) {
	if strings.HasPrefix(value, "urn:review-saga:") {
		var foundDir string
		walkTargets(document.Root, document.Section, func(target, dir string, fragment bool) {
			if target == value && (allowFragment || !fragment) {
				foundDir = dir
			}
		})
		if value == saga.SagaTarget(document.Manifest.ID) {
			foundDir = document.Root
		}
		if foundDir == "" {
			return "", "", fmt.Errorf("target %q does not exist", value)
		}
		return foundDir, value, nil
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(document.Root, candidate)
	}
	candidateAbs, _ := filepath.Abs(candidate)
	var directTarget string
	walkTargets(document.Root, document.Section, func(target, dir string, fragment bool) {
		dirAbs, _ := filepath.Abs(dir)
		if dirAbs == candidateAbs && (allowFragment || !fragment) {
			directTarget = target
		}
	})
	if directTarget != "" {
		return candidateAbs, directTarget, nil
	}
	dir, err := store.ResolveSection(document.Root, value)
	if err != nil {
		return "", "", err
	}
	abs, _ := filepath.Abs(dir)
	if abs == document.Root {
		return abs, saga.SagaTarget(document.Manifest.ID), nil
	}
	var foundTarget string
	var isFragment bool
	walkTargets(document.Root, document.Section, func(target, candidate string, fragment bool) {
		candidateAbs, _ := filepath.Abs(candidate)
		if candidateAbs == abs {
			foundTarget, isFragment = target, fragment
		}
	})
	if foundTarget == "" || isFragment && !allowFragment {
		return "", "", fmt.Errorf("target %q is not a valid %s", value, map[bool]string{true: "chapter, section, or fragment", false: "chapter or section"}[allowFragment])
	}
	return abs, foundTarget, nil
}

func walkTargets(root string, section *saga.Section, fn func(target, dir string, fragment bool)) {
	// Section paths are resolved by the caller from the saga root; fragment
	// directories are already absolute because they can also live in messages.
	for _, fragment := range section.Fragments {
		fn(fragment.Target, fragment.Directory, true)
		for index := range fragment.Landmarks {
			landmark := &fragment.Landmarks[index]
			fn(landmark.Target, landmark.Directory, true)
		}
	}
	for _, child := range section.Children {
		fn(child.Target, filepath.Join(root, filepath.FromSlash(child.Path)), false)
		walkTargets(root, child, fn)
	}
}

func targetIDExists(document *saga.Saga, id string) bool {
	found := id == document.Manifest.ID || id == document.Section.ID
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Path != "" && section.ID == id {
			found = true
		}
		for _, fragment := range section.Fragments {
			if fragment.ID == id {
				found = true
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return found
}

func createFragment(dir string, manifest saga.FragmentManifest, source string, content []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := store.WriteJSON(filepath.Join(dir, "fragment.json"), manifest, true); err != nil {
		return err
	}
	target := filepath.Join(dir, manifest.Entrypoint)
	if source != "" {
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyFragmentPackage(source, dir); err != nil {
				return err
			}
			if entry, err := os.Stat(target); err != nil || entry.IsDir() {
				return fmt.Errorf("entrypoint %q does not exist in source directory", manifest.Entrypoint)
			}
			return nil
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}
	return os.WriteFile(target, content, 0o644)
}

func fragmentContent(kind, explicitType, source string) (string, []byte, string, error) {
	mediaType := explicitType
	if mediaType == "" {
		switch kind {
		case "markdown":
			mediaType = "text/markdown"
		case "html":
			mediaType = "text/html"
		case "svg":
			mediaType = "image/svg+xml"
		case "text":
			mediaType = "text/plain"
		case "image":
			mediaType = mime.TypeByExtension(strings.ToLower(filepath.Ext(source)))
		default:
			return "", nil, "", fmt.Errorf("unsupported fragment type %q", kind)
		}
	}
	if source != "" {
		if info, err := os.Stat(source); err == nil && info.IsDir() {
			switch mediaType {
			case "text/html":
				return "index.html", nil, mediaType, nil
			case "image/svg+xml":
				return "image.svg", nil, mediaType, nil
			default:
				return "", nil, "", fmt.Errorf("--entrypoint is required for a %s source directory", mediaType)
			}
		}
		return filepath.Base(source), nil, mediaType, nil
	}
	switch mediaType {
	case "text/markdown":
		return "content.md", []byte("Write this review fragment.\n"), mediaType, nil
	case "text/plain":
		return "content.txt", []byte("Write this review fragment.\n"), mediaType, nil
	case "text/html":
		return "index.html", []byte(defaultHTMLFragment), mediaType, nil
	case "image/svg+xml":
		return "image.svg", []byte(defaultSVGFragment), mediaType, nil
	default:
		return "", nil, "", fmt.Errorf("--source is required for %s", mediaType)
	}
}

func copyFragmentPackage(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fragment source cannot contain symlink %s", rel)
		}
		if strings.HasPrefix(entry.Name(), "___") || rel == "fragment.json" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func supportedMediaType(value string) bool {
	return value == "text/markdown" || value == "text/html" || value == "text/plain" || value == "image/svg+xml" || strings.HasPrefix(value, "image/")
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

const defaultHTMLFragment = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style>body{font:16px system-ui;margin:0;padding:24px}button{font:inherit}</style></head>
<body><h1>Interactive fragment</h1><p>Bundle JavaScript, CSS, images, and data beside this file.</p><button id="demo">Clicks: 0</button><script>let n=0;document.querySelector('#demo').onclick=e=>e.target.textContent='Clicks: '+(++n)</script></body></html>
`

const defaultSVGFragment = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 300" role="img" aria-label="Diagram placeholder">
  <rect width="800" height="300" rx="24" fill="#eef3ee"/><text x="400" y="150" text-anchor="middle" dominant-baseline="middle" font-family="system-ui" font-size="28" fill="#244235">Replace with a useful diagram</text>
</svg>
`

const specText = `Review Saga format v2 (experimental)

A saga begins with its overview and direct *.chapter directories. Chapters are
independently reviewable boundaries roughly corresponding to the PRs one might
have created when splitting the change. Sections recurse inside chapters and
contain directory-backed fragments. Each *.fragment has fragment.json and an
entrypoint. Markdown, HTML, SVG, text, and raster images are supported. HTML and SVG may bundle JavaScript
and assets; the reference viewer executes them in sandboxed frames.

Any saga, chapter, section, or fragment may own ___diffs/*.json evidence. Every
evidence entry is an absolute saga-diff://v1 URI containing repository URI, immutable
base/head identities, and a line range or file event.

Review threads live under ___review/threads. They target stable
urn:review-saga:* identifiers and anchor to a whole fragment, normalized shapes,
freehand drawings, quoted text, a placed sticky note, or an absolute diff URI.
A sticky note carries its visible text, a normalized centre point, and an
optional color; moving, rewording, or recoloring it appends an anchor event. Thread messages contain
fragments, so replies may include Markdown, HTML, SVG, and images. Suggestion
threads include replacement code. Append-only file URI events track reviewed
state, and approvals may target the saga, a chapter, a section, or a fragment.
Every comment owns a thread directory, every reply owns a message directory, and
each state transition is a new file; review operations never update shared arrays.

The authoritative specification is SPEC.md in the Review Saga repository.
`
