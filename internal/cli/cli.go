package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/change-saga/change-saga/internal/coverage"
	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/gitdiff"
	"github.com/change-saga/change-saga/internal/reviewstore"
	"github.com/change-saga/change-saga/internal/saga"
	reviewserver "github.com/change-saga/change-saga/internal/server"
	"github.com/change-saga/change-saga/internal/store"
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
	info, _ := debug.ReadBuildInfo()
	return versionString(Version, Commit, BuildDate, info)
}

func versionString(version, commit, buildDate string, info *debug.BuildInfo) string {
	// `go install module@version` cannot supply linker flags. Preserve release
	// injection when present, but make an installed module report its module and
	// VCS metadata instead of the source-tree development placeholder.
	if info != nil {
		if strings.HasSuffix(version, "-dev") && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = strings.TrimPrefix(info.Main.Version, "v")
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if commit == "" {
					commit = setting.Value
					if len(commit) > 12 {
						commit = commit[:12]
					}
				}
			case "vcs.time":
				if buildDate == "" {
					buildDate = setting.Value
				}
			}
		}
	}

	out := version
	if commit != "" {
		out += " (" + commit + ")"
	}
	if buildDate != "" {
		out += " built " + buildDate
	}
	return out
}

type StatusError struct{ Code int }

func (e *StatusError) Error() string { return "command reported a non-success status" }

func PrintHelp(out io.Writer) {
	fmt.Fprint(out, `Change Saga — make every part of a large change reviewable

Usage:
  change-saga init [flags] <name.saga>
  change-saga add-chapter [flags] <saga> <name>
  change-saga add-section [flags] <saga> <section/path>
  change-saga add-fragment [flags] <saga>
  change-saga cover [flags] <saga>
  change-saga thread [flags] <saga>
  change-saga reply [flags] <saga>
  change-saga review [flags] <saga>
  change-saga validate [--json] <saga>
  change-saga status [--json] [--repo PATH] <saga>
  change-saga query <operation> --saga PATH [--repo PATH] [operation flags]
  change-saga serve [--addr ADDR] [--repo PATH] [--open] <saga>
  change-saga open [--addr ADDR] [--repo PATH] <saga>
  change-saga install-skill
  change-saga spec [--json]

Run "change-saga <command> -h" for command-specific options.
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
	repository := flags.String("repository", "", "portable absolute source repository URI; defaults to origin")
	allowLocalRepository := flags.Bool("allow-local-repository", false, "persist a local file:// repository identity when origin is unavailable")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "accept an explicitly declared repository that differs from origin")
	prNumber := flags.Int("pr", 0, "pull request number")
	prURL := flags.String("pr-url", "", "pull request URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: change-saga init [flags] <name.saga>")
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
	if *prURL != "" {
		if parsed, err := url.Parse(*prURL); err != nil || !parsed.IsAbs() {
			return fmt.Errorf("--pr-url must be an absolute URI")
		}
	}
	repositoryURI, _, err := discoverRepository(ctx, *repoDir, *repository, *allowLocalRepository, *allowRepositoryMismatch)
	if err != nil {
		return err
	}
	manifest := saga.Manifest{Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: *id, Title: *title, Source: saga.Source{Repository: repositoryURI, Base: *base, Head: *head}}
	if *prNumber != 0 || *prURL != "" {
		manifest.PR = &saga.PR{URL: *prURL}
		if *prNumber != 0 {
			manifest.PR.Number = prNumber
		}
	}
	overview := saga.FragmentManifest{Version: saga.CurrentVersion, ID: *id + "-overview", Title: "Overview", MediaType: "text/markdown", Entrypoint: "content.md"}
	// A failed init must not leave a half-built .saga behind, because the
	// directory would then block a retry while never loading.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	// The saga itself is staged and published atomically; its containing
	// directories are ordinary parents and may be created up front. The parent
	// is then resolved, because a saga may legitimately live under a symlinked
	// ancestor such as macOS /tmp even though nothing inside a saga may be a
	// symlink.
	if err := os.MkdirAll(filepath.Dir(absRoot), 0o755); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absRoot))
	if err != nil {
		return err
	}
	err = store.CommitDir(parent, filepath.Join(parent, filepath.Base(absRoot)), func(stage string) error {
		if err := os.Chmod(stage, 0o755); err != nil {
			return err
		}
		for _, dir := range []string{"___diffs", "___approvals", filepath.Join("___review", "threads"), filepath.Join("___review", "diffs")} {
			if err := os.MkdirAll(filepath.Join(stage, dir), 0o755); err != nil {
				return err
			}
		}
		if err := store.WriteJSON(filepath.Join(stage, "saga.json"), manifest, true); err != nil {
			return err
		}
		return populateFragment(filepath.Join(stage, "overview.fragment"), overview, "", []byte("Explain the change as a whole. Lead with the context that makes the rest of the saga easier to navigate.\n"))
	})
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists", root)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %s\nNext: change-saga add-chapter --title \"Architecture\" %s architecture\n", root, root)
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
		return fmt.Errorf("usage: change-saga add-chapter [flags] <saga> <name>")
	}
	name := strings.TrimSuffix(flags.Arg(1), ".chapter")
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Base(name) != name || strings.HasPrefix(name, "___") {
		return fmt.Errorf("chapter name must be a single non-reserved path component")
	}
	var created string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		chapterTitle, chapterID := *title, *id
		if chapterTitle == "" {
			chapterTitle = strings.ReplaceAll(name, "-", " ")
		}
		if chapterID == "" {
			chapterID = store.Slug(name)
		}
		overviewID := chapterID + "-overview"
		if !saga.ValidID(chapterID) || !saga.ValidID(overviewID) || targetIDExists(document, chapterID) || targetIDExists(document, overviewID) {
			return fmt.Errorf("chapter id %q or its overview id is invalid or already used", chapterID)
		}
		dir := filepath.Join(document.Root, store.Slug(name)+".chapter")
		manifest := saga.ChapterManifest{Version: saga.CurrentVersion, ID: chapterID, Title: chapterTitle, Order: *order}
		overview := saga.FragmentManifest{Version: saga.CurrentVersion, ID: overviewID, Title: "Chapter overview", MediaType: "text/markdown", Entrypoint: "content.md"}
		content := []byte("Explain this chapter as an independently reviewable change. Describe its boundary, behavior, and risks.\n")
		// One staged rename: a failed chapter never leaves a manifest-less
		// directory behind that would invalidate the saga for every later
		// command.
		err := store.CommitDir(document.Root, dir, func(stage string) error {
			if err := os.Chmod(stage, 0o755); err != nil {
				return err
			}
			for _, reserved := range []string{"___diffs", "___approvals"} {
				if err := os.Mkdir(filepath.Join(stage, reserved), 0o755); err != nil {
					return err
				}
			}
			if err := store.WriteJSON(filepath.Join(stage, "chapter.json"), manifest, true); err != nil {
				return err
			}
			return populateFragment(filepath.Join(stage, "overview.fragment"), overview, "", content)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("chapter %s already exists", filepath.Base(dir))
		}
		created = filepath.Base(dir)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added chapter %s\n", created)
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
		return fmt.Errorf("usage: change-saga add-section [flags] <saga> <section/path>")
	}
	sectionPath := filepath.Clean(flags.Arg(1))
	parentPath, name := filepath.Dir(sectionPath), filepath.Base(sectionPath)
	if parentPath == "." || name == "." || strings.HasPrefix(name, "___") || strings.HasSuffix(name, ".chapter") || strings.HasSuffix(name, ".fragment") {
		return fmt.Errorf("sections must be created inside an existing chapter or section")
	}
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		parentDir, _, err := resolveTarget(document, parentPath, false)
		if err != nil {
			return fmt.Errorf("resolve parent: %w", err)
		}
		dir := filepath.Join(parentDir, name)
		sectionTitle, sectionID := *title, *id
		if sectionTitle == "" {
			sectionTitle = strings.ReplaceAll(name, "-", " ")
		}
		if sectionID == "" {
			sectionID = store.Slug(strings.ReplaceAll(filepath.ToSlash(flags.Arg(1)), "/", "-"))
		}
		if !saga.ValidID(sectionID) || targetIDExists(document, sectionID) {
			return fmt.Errorf("section id %q is invalid or already used", sectionID)
		}
		manifest := saga.SectionManifest{Version: saga.CurrentVersion, ID: sectionID, Title: sectionTitle, Order: *order}
		err = store.CommitDir(document.Root, dir, func(stage string) error {
			if err := os.Chmod(stage, 0o755); err != nil {
				return err
			}
			for _, reserved := range []string{"___diffs", "___approvals"} {
				if err := os.Mkdir(filepath.Join(stage, reserved), 0o755); err != nil {
					return err
				}
			}
			return store.WriteJSON(filepath.Join(stage, "section.json"), manifest, true)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("section %s already exists", flags.Arg(1))
		}
		return err
	})
	if err != nil {
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
		return fmt.Errorf("usage: change-saga add-fragment [flags] <saga>")
	}
	entrypoint, content, resolvedType, err := fragmentContent(*kind, *mediaType, *source)
	if err != nil {
		return err
	}
	if *entrypointFlag != "" {
		entrypoint = filepath.ToSlash(*entrypointFlag)
	}
	if reason := saga.EntrypointError(entrypoint); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if !saga.ValidMediaType(resolvedType) {
		return fmt.Errorf("unsupported fragment media type %q", resolvedType)
	}
	var created string
	err = authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		sectionDir, _, err := resolveTarget(document, *section, false)
		if err != nil {
			return err
		}
		fragmentName, fragmentID := *name, *id
		if fragmentName == "" {
			fragmentName = store.Slug(firstNonEmpty(*title, fragmentID, *kind))
		}
		if fragmentID == "" {
			fragmentID = store.Slug(document.Manifest.ID + "-" + strings.ReplaceAll(filepath.ToSlash(*section), "/", "-") + "-" + fragmentName)
		}
		if !saga.ValidID(fragmentID) || targetIDExists(document, fragmentID) {
			return fmt.Errorf("fragment id %q is invalid or already used", fragmentID)
		}
		fragmentDir := filepath.Join(sectionDir, store.Slug(fragmentName)+".fragment")
		manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: fragmentID, Title: *title, MediaType: resolvedType, Entrypoint: entrypoint, Order: *order}
		err = createFragment(document.Root, fragmentDir, manifest, *source, content)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("fragment %s already exists", fragmentDir)
		}
		rel, _ := filepath.Rel(document.Root, fragmentDir)
		created = filepath.ToSlash(rel)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added fragment %s\n", created)
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
	event := flags.String("event", "", "file event: add, delete, type-change, rename, mode, binary, or modify")
	oldPath := flags.String("old-path", "", "old path for a rename event")
	newPath := flags.String("new-path", "", "new path for a rename event")
	note := flags.String("note", "", "optional explanation for report authors")
	name := flags.String("name", "", "coverage filename without .json")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	var uris stringList
	flags.Var(&uris, "uri", "absolute saga-diff URI; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: change-saga cover [flags] <saga>")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	for _, value := range uris {
		reference, err := diffuri.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid --uri: %w", err)
		}
		repository, err := diffuri.CanonicalRepository(document.Manifest.Source.Repository)
		if err != nil {
			return fmt.Errorf("invalid declared source repository: %w", err)
		}
		if reference.Repository != repository {
			return fmt.Errorf("invalid --uri: repository %q does not match saga source repository %q", reference.Repository, repository)
		}
	}
	if *path != "" || *event != "" {
		checkout := firstNonEmpty(*repoDir, document.Root)
		changes, err := gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head, gitdiff.ReadOptions{AllowRepositoryMismatch: *allowRepositoryMismatch})
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
	// The Git comparison is read before the lock is taken so a slow diff does
	// not stall other writers; only target resolution and the record write are
	// serialized.
	var created string
	if err := authorMutation(flags.Arg(0), func(locked *saga.Saga) error {
		targetDir, _, err := resolveTarget(locked, *target, true)
		if err != nil {
			return err
		}
		diffDir, err := store.EnsureDirWithin(locked.Root, filepath.Join(targetDir, "___diffs"))
		if err != nil {
			return err
		}
		targetPath := filepath.Join(diffDir, store.Slug(*name)+".json")
		if err := store.WriteJSON(targetPath, file, true); err != nil {
			return err
		}
		rel, _ := filepath.Rel(locked.Root, targetPath)
		created = filepath.ToSlash(rel)
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added %s\n", created)
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
		return fmt.Errorf("usage: change-saga thread [flags] <saga>")
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
		return fmt.Errorf("usage: change-saga reply [flags] <saga>")
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
		return fmt.Errorf("usage: change-saga review [flags] <saga>")
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
		return fmt.Errorf("usage: change-saga validate [--json] <saga>")
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
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: change-saga status [--json] [--repo PATH] <saga>")
	}
	report, err := buildReport(ctx, flags.Arg(0), *repoDir, *allowRepositoryMismatch)
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

func buildReport(ctx context.Context, root, repoDir string, allowRepositoryMismatch ...bool) (coverage.Report, error) {
	document, validation, err := saga.Load(root)
	if err != nil {
		return coverage.Report{}, err
	}
	checkout := firstNonEmpty(repoDir, document.Root)
	allowMismatch := len(allowRepositoryMismatch) > 0 && allowRepositoryMismatch[0]
	changes, err := gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head, gitdiff.ReadOptions{AllowRepositoryMismatch: allowMismatch})
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
	addr := flags.String("addr", "127.0.0.1:7342", "loopback listen address; remote serving is disabled")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	openBrowser := flags.Bool("open", len(openByDefault) > 0 && openByDefault[0], "open the review in a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: change-saga serve [--addr ADDR] [--repo PATH] [--open] <saga>")
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
		return fmt.Errorf("usage: change-saga spec [--json]")
	}
	if *jsonOutput {
		return writeJSON(out, map[string]any{
			"version": 2, "chapter_suffix": ".chapter", "chapter_manifest": "chapter.json", "fragment_suffix": ".fragment", "fragment_manifest": "fragment.json",
			"hierarchy":     []string{"overview", "chapter", "section", "fragment"},
			"media_types":   []string{"text/markdown", "text/html", "text/plain", "image/svg+xml", "image/*"},
			"target_scheme": "urn:change-saga", "diff_scheme": "saga-diff://v1",
			"anchors":              []string{"target", "region", "drawing", "text", "note", "diff"},
			"thread_kinds":         []string{"comment", "suggestion"},
			"reserved_directories": []string{"___diffs", "___approvals", "___review"},
			"review_storage":       "append-only; one thread, message, or event record per path",
		})
	}
	fmt.Fprint(out, specText)
	return nil
}

// InstallSkill prints an agent-agnostic bootstrap prompt. The active coding
// agent owns the platform-specific skill location and format; the saga binary
// supplies the behavior contract without mutating the user's repository.
func InstallSkill(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	flags.SetOutput(out)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: change-saga install-skill")
	}
	_, err := io.WriteString(out, installSkillPrompt)
	return err
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

func discoverRepository(ctx context.Context, repoDir, explicit string, options ...bool) (string, string, error) {
	allowLocal := len(options) > 0 && options[0]
	allowMismatch := len(options) > 1 && options[1]
	rootOutput, err := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("locate source repository: %s", strings.TrimSpace(string(rootOutput)))
	}
	root := strings.TrimSpace(string(rootOutput))
	if explicit != "" {
		canonical, err := diffuri.CanonicalRepository(explicit)
		if err != nil {
			return "", "", fmt.Errorf("--repository: %w", err)
		}
		parsed, _ := url.Parse(canonical)
		if !allowMismatch && (parsed.Scheme == "file" || repositoryOriginAvailable(ctx, root)) {
			if err := gitdiff.VerifyRepository(ctx, root, canonical); err != nil {
				return "", "", err
			}
		}
		return canonical, root, nil
	}
	remoteOutput, remoteErr := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin").CombinedOutput()
	if remoteErr == nil && strings.TrimSpace(string(remoteOutput)) != "" {
		canonical, err := normalizeRepositoryURI(strings.TrimSpace(string(remoteOutput)), root)
		if err != nil {
			return "", "", fmt.Errorf("canonicalize origin repository: %w", err)
		}
		parsed, _ := url.Parse(canonical)
		if parsed.Scheme == "file" && !allowLocal {
			return "", "", fmt.Errorf("origin resolves to a local file repository; provide a portable --repository URI or explicitly opt in with --allow-local-repository")
		}
		return canonical, root, nil
	}
	if !allowLocal {
		return "", "", fmt.Errorf("origin is unavailable; provide a portable --repository URI or explicitly opt in with --allow-local-repository")
	}
	canonical, err := diffuri.FileRepository(root)
	return canonical, root, err
}

func repositoryOriginAvailable(ctx context.Context, root string) bool {
	output, err := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin").Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

func normalizeRepositoryURI(value, root string) (string, error) {
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return diffuri.CanonicalRepository(parsed.String())
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		if colon := strings.Index(value[at:], ":"); colon > 0 {
			colon += at
			return diffuri.CanonicalRepository("ssh://" + value[at+1:colon] + "/" + value[colon+1:])
		}
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return diffuri.FileRepository(abs)
}

func resolveTarget(document *saga.Saga, value string, allowFragment bool) (string, string, error) {
	if strings.HasPrefix(value, "urn:change-saga:") {
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

// createFragment publishes a complete fragment package with one rename. A
// fragment that is missing its entrypoint invalidates the whole saga, so a
// half-written package must never become visible under its final name.
func createFragment(root, dir string, manifest saga.FragmentManifest, source string, content []byte) error {
	return store.CommitDir(root, dir, func(stage string) error {
		if err := os.Chmod(stage, 0o755); err != nil {
			return err
		}
		return populateFragment(stage, manifest, source, content)
	})
}

// populateFragment fills an already-created directory. Callers that are
// themselves staging a larger entity reuse it directly.
func populateFragment(dir string, manifest saga.FragmentManifest, source string, content []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := store.WriteJSON(filepath.Join(dir, "fragment.json"), manifest, true); err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(manifest.Entrypoint))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
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

// authorMutation serializes an authoring write against every other supported
// writer and re-resolves the saga under that lock, so target resolution cannot
// go stale between the read and the commit.
func authorMutation(root string, operation func(*saga.Saga) error) error {
	return store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		document, _, err := saga.Load(root)
		if err != nil {
			return err
		}
		return operation(document)
	})
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

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

const defaultHTMLFragment = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style>body{font:16px system-ui;margin:0;padding:24px}button{font:inherit}</style></head>
<body><h1>Interactive fragment</h1><p>Bundle JavaScript, CSS, images, and data beside this file.</p><button id="demo">Clicks: 0</button><script>let n=0;document.querySelector('#demo').onclick=e=>e.target.textContent='Clicks: '+(++n)</script></body></html>
`

const installSkillPrompt = `Install or update a project-local agent skill named "change-saga" using this coding agent's native skill mechanism. Do not create a Change Saga as part of installation. Preserve the following behavior in the installed skill:

# Change Saga authoring

A Change Saga is the authored change proposal submitted for review: a visual,
executable successor to a flat pull-request title and description. It is the
thing to be reviewed, not the review itself.

When a user asks to draft, prepare, or create a pull request or otherwise make a
large change ready for review, use the repository's existing PR-authoring
processes, templates, issue context, conventions, and checks, but express the
result as a Change Saga. Do not replace useful existing authoring discipline;
extend it into this format.

During authoring, speak as the change author and guide. Do not create review
comments, findings, approvals, rejections, or other review-overlay records.
Only perform those actions when the user explicitly asks to conduct a review of
an already-authored saga.

Use the installed "change-saga" CLI as the source of truth. Begin with
"change-saga --help" and "change-saga spec" when necessary. Resolve the exact PR, branch, commit range, or
working-tree comparison; inspect the full change and its existing PR context;
then initialize and author the .saga directory.

Use this authoring loop, consulting each command's "-h" output for exact flags:

1. "change-saga init" records the exact repository, base, head, title, and PR identity.
2. "change-saga status --json" lists the uncovered product changes.
3. "change-saga add-chapter", "change-saga add-section", and "change-saga add-fragment" build the
   reviewer-oriented narrative and its Markdown, SVG, image, or HTML packages.
4. "change-saga cover" connects a focused fragment or landmark to the exact diff atoms
   it explains and includes a concise what-and-why note.
5. Repeat status, then run "change-saga validate --json" before "change-saga open" presents
   the authored proposal for review.

Lead with pictures and show by example. The root should establish the goal,
system/change map, affected workflows, and chapter path before dense prose.
Every substantial chapter should begin with an SVG diagram, self-contained
interactive HTML walkthrough, or concrete before/after example. Highlight
end-to-end workflows, data flows, data models, state transitions, boundaries,
failure paths, compatibility, and observable outcomes. Give meaningful diagram
nodes and interactive elements stable landmarks so they can link to the exact
code that realizes them.

Organize the change into independently reviewable chapters based on behavior,
risk, architecture, or reviewer intent rather than file type. Use Markdown to
orient and connect visual artifacts, not as the default container for the whole
explanation.

Run "change-saga status --json <name>.saga" as the coverage work queue. Attach only
the exact diff atoms a fragment or landmark explains, with concise notes saying
what changed and why that content owns it. Never widen mappings only to reach
100 percent. Iterate until every product change is accounted for and no mapping
is stale, then run "change-saga validate --json" and "change-saga status --json" before
handoff.

Opening the saga presents the authored proposal for a human to review. It does
not authorize the agent to conduct that review.
`

const defaultSVGFragment = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 300" role="img" aria-label="Diagram placeholder">
  <rect width="800" height="300" rx="24" fill="#eef3ee"/><text x="400" y="150" text-anchor="middle" dominant-baseline="middle" font-family="system-ui" font-size="28" fill="#244235">Replace with a useful diagram</text>
</svg>
`

const specText = `Change Saga format v2 (experimental)

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
urn:change-saga:* identifiers and anchor to a whole fragment, normalized shapes,
freehand drawings, quoted text, a placed sticky note, or an absolute diff URI.
A sticky note carries its visible text, a normalized centre point, and an
optional color; moving, rewording, or recoloring it appends an anchor event. Thread messages contain
fragments, so replies may include Markdown, HTML, SVG, and images. Suggestion
threads include replacement code. Append-only file URI events track reviewed
state, and approvals may target the saga, a chapter, a section, or a fragment.
Every comment owns a thread directory, every reply owns a message directory, and
each state transition is a new file; review operations never update shared arrays.

The authoritative specification is SPEC.md in the Change Saga repository.
`
