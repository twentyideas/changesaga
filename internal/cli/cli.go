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
	"sort"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/reviewstore"
	"github.com/twentyideas/changesaga/internal/saga"
	reviewserver "github.com/twentyideas/changesaga/internal/server"
	"github.com/twentyideas/changesaga/internal/store"
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

// commandOrder fixes the order the top-level help lists commands in;
// commandUsage is the single source of each command's usage line so the
// overview, the per-command -h banner, and argument errors cannot drift apart.
var commandOrder = []string{
	"init", "add-chapter", "add-section", "add-fragment", "set-fragment-content", "add-landmark", "cover", "remove-coverage", "replace-coverage", "add-claim", "verify-claim",
	"thread", "reply", "review", "validate", "status", "compare", "query",
	"serve", "open", "install-skill", "spec",
}

var commandUsage = map[string]string{
	"init":                 "change-saga init [flags] <name.saga>",
	"add-chapter":          "change-saga add-chapter [flags] <saga> <name>",
	"add-section":          "change-saga add-section [flags] <saga> <section/path>",
	"add-fragment":         "change-saga add-fragment [flags] <saga>",
	"set-fragment-content": "change-saga set-fragment-content --target TARGET --source FILE|- [--json|--quiet] <saga>",
	"add-landmark":         "change-saga add-landmark [flags] <saga>",
	"cover":                "change-saga cover [flags] [--batch FILE|-] [--dry-run] <saga>",
	"remove-coverage":      "change-saga remove-coverage --record PATH [--dry-run] [--json|--quiet] <saga>",
	"replace-coverage":     "change-saga replace-coverage --record PATH [coverage flags] [--batch FILE|-] [--dry-run] <saga>",
	"add-claim":            "change-saga add-claim --target TARGET --kind KIND --statement TEXT --diff URI [--diff URI...] <saga>",
	"verify-claim":         "change-saga verify-claim --claim ID --status STATUS --summary TEXT [flags] <saga>",
	"thread":               "change-saga thread [flags] <saga>",
	"reply":                "change-saga reply [flags] <saga>",
	"review":               "change-saga review [flags] <saga>",
	"validate":             "change-saga validate [--json] [--fix] <saga>",
	"status":               "change-saga status [--json] [--repo PATH] <saga>",
	"compare":              "change-saga compare [--json] [--repo PATH] (--against-saga PATH | --base REV [--head REV]) <saga>",
	"query":                "change-saga query <operation> --saga PATH [--repo PATH] [operation flags]",
	"serve":                "change-saga serve [--addr ADDR] [--repo PATH] [--open] [--detach] <saga>",
	"open":                 "change-saga open [--addr ADDR] [--repo PATH] [--detach] <saga>",
	"install-skill":        "change-saga install-skill",
	"spec":                 "change-saga spec [--json]",
}

func PrintHelp(out io.Writer) {
	fmt.Fprint(out, "Change Saga — make every part of a large change reviewable\n\nUsage:\n")
	for _, command := range commandOrder {
		fmt.Fprintf(out, "  %s\n", commandUsage[command])
	}
	fmt.Fprint(out, `
Run "change-saga <command> -h" for command-specific options.

Using a coding agent?
  If its Change Saga skill is not installed or current, run
  "change-saga install-skill" and give the resulting agent-agnostic bootstrap
  prompt to the agent. The command does not modify the repository or create a Saga.
`)
}

// commandFlags builds a flag set whose -h output always leads with the command
// it was actually invoked as. The stock flag banner prints the flag set's name
// and nothing else, which made "change-saga open -h" claim to be serve and gave
// a flagless command like install-skill an empty, unexplained banner.
func commandFlags(name, usage string, out io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() {
		fmt.Fprintf(out, "Usage:\n  %s\n", usage)
		if description := commandDescription[name]; description != "" {
			fmt.Fprintf(out, "\n%s\n", description)
		}
		count := 0
		flags.VisitAll(func(*flag.Flag) { count++ })
		if count > 0 {
			fmt.Fprint(out, "\nFlags:\n")
			flags.PrintDefaults()
		}
	}
	return flags
}

var commandDescription = map[string]string{
	"set-fragment-content": "Replace a fragment entrypoint through the supported authoring API. Use --source -\nto read content from standard input; the fragment media type and metadata are preserved.",
	"add-landmark":         "Create a coverable target for one Markdown heading, exact text span, HTML/SVG\nelement, or normalized image region inside a fragment. An SVG --element-id is\nmeasured into an on-canvas link automatically; --hotspot overrides its bounds.\nHTML elements need --hotspot for an on-canvas link. Visual landmarks require a\nsemantic --description for non-visual consumers.",
	"cover": `Attach the exact diff atoms a narrative target explains. --target accepts a
section or fragment path, a target URN, or <fragment-path>#<landmark-id>.
--batch reads newline-delimited JSON records (or one JSON array) with the
per-record fields target, path, side, lines, changed_lines, event, old_path,
new_path, note, name, and uris; the whole batch is resolved before anything is written, and a
failing record leaves the saga untouched.`,
	"remove-coverage":  "Delete one exact coverage record named by query mappings or fragment-diffs.",
	"replace-coverage": "Atomically replace one coverage record with one or more newly resolved records.\nUse --batch to split or retarget broad evidence without leaving partial coverage.",
	"add-claim":        "Record one falsifiable author assertion and its exact supporting diff evidence.\nClaims do not count toward coverage and are independently verified.",
	"verify-claim":     "Append an independent verification result without rewriting the claim or prior results.",
	"open":             "Serve the saga on loopback and open it in a browser. --detach returns after\nprinting the PID and active URL.",
	"serve":            "Serve the saga on loopback for review. Detached instances are managed with\nchange-saga serve status [SAGA] and change-saga serve stop [SAGA].",
	"install-skill":    "Print the agent-agnostic prompt that installs the change-saga authoring skill.\nPipe it to a coding agent; it neither writes to this repository nor creates a saga.",
	"validate":         "Check the saga against the format. --fix adds missing stable anchors to Markdown\nheadings in narrative fragments and changes nothing else.",
	"compare":          "Project an incoming Git comparison onto the maintained Saga's source evidence.\nThe command never compares prose or visual content. Direct intersections identify\ntargets that must update; nearby additions identify targets to reconsider; ownerless\nchanges require new Saga content.",
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == name {
			found = true
		}
	})
	return found
}

func Init(ctx context.Context, args []string, out io.Writer) error {
	flags := commandFlags("init", commandUsage["init"], out)
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
		return fmt.Errorf("usage: %s", commandUsage["init"])
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
		for _, dir := range []string{"___diffs", "___approvals", "___claims", "___verifications", filepath.Join("___review", "threads"), filepath.Join("___review", "diffs")} {
			if err := os.MkdirAll(filepath.Join(stage, dir), 0o755); err != nil {
				return err
			}
		}
		if err := store.WriteJSON(filepath.Join(stage, "saga.json"), manifest, true); err != nil {
			return err
		}
		if err := store.WriteFile(filepath.Join(stage, "README.md"), []byte(reviewerBootstrapREADME), 0o644, true); err != nil {
			return err
		}
		return populateFragment(filepath.Join(stage, "overview.fragment"), overview, "", nil)
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
	flags := commandFlags("add-chapter", commandUsage["add-chapter"], out)
	title := flags.String("title", "", "chapter title")
	id := flags.String("id", "", "stable chapter identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: %s", commandUsage["add-chapter"])
	}
	name := strings.TrimSuffix(flags.Arg(1), ".chapter")
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Base(name) != name || strings.HasPrefix(name, "___") {
		return fmt.Errorf("chapter name must be a single non-reserved path component")
	}
	var created, createdID, createdTarget string
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
		overview := saga.FragmentManifest{Version: saga.CurrentVersion, ID: overviewID, MediaType: "text/markdown", Entrypoint: "content.md"}
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
			return populateFragment(filepath.Join(stage, "overview.fragment"), overview, "", nil)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("chapter %s already exists", filepath.Base(dir))
		}
		created = filepath.Base(dir)
		createdID = chapterID
		createdTarget = saga.ChapterTarget(document.Manifest.ID, chapterID)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added chapter %s\n", created)
	fmt.Fprintf(out, "Target: %s\n", createdTarget)
	fmt.Fprintf(out, "Next: change-saga add-section --title \"Section title\" %s %s/section-name\n", flags.Arg(0), createdID)
	return nil
}

func AddSection(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-section", commandUsage["add-section"], out)
	title := flags.String("title", "", "section title")
	id := flags.String("id", "", "stable section identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: %s", commandUsage["add-section"])
	}
	sectionPath := filepath.Clean(flags.Arg(1))
	parentPath, name := filepath.Dir(sectionPath), filepath.Base(sectionPath)
	if parentPath == "." || name == "." || strings.HasPrefix(name, "___") || strings.HasSuffix(name, ".chapter") || strings.HasSuffix(name, ".fragment") {
		return fmt.Errorf("sections must be created inside an existing chapter or section")
	}
	var created, createdID, createdTarget string
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
		if err == nil {
			rel, _ := filepath.Rel(document.Root, dir)
			created = filepath.ToSlash(rel)
			createdID = sectionID
			createdTarget = saga.SectionTarget(document.Manifest.ID, sectionID)
		}
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added section %s\n", created)
	fmt.Fprintf(out, "Target: %s\n", createdTarget)
	fmt.Fprintf(out, "Next: change-saga add-fragment --section %s --title \"Fragment title\" %s\n", createdID, flags.Arg(0))
	return nil
}

func AddFragment(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-fragment", commandUsage["add-fragment"], out)
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
		return fmt.Errorf("usage: %s", commandUsage["add-fragment"])
	}
	entrypoint, content, resolvedType, err := fragmentContent(*kind, *mediaType, *source)
	if err != nil {
		return err
	}
	if *entrypointFlag != "" {
		// Entrypoints use the format's portable slash-path grammar. Do not
		// normalize a Windows backslash into a different, accepted path.
		entrypoint = *entrypointFlag
	}
	if reason := saga.EntrypointError(entrypoint); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if !saga.ValidMediaType(resolvedType) {
		return fmt.Errorf("unsupported fragment media type %q", resolvedType)
	}
	var created, createdTarget string
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
		createdTarget = saga.FragmentTarget(document.Manifest.ID, fragmentID)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added fragment %s\n", created)
	fmt.Fprintf(out, "Target: %s\n", createdTarget)
	fmt.Fprintf(out, "Next: change-saga set-fragment-content --target %s --source FILE|- %s\n", createdTarget, flags.Arg(0))
	return nil
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func Thread(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("thread", commandUsage["thread"], out)
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
		return fmt.Errorf("usage: %s", commandUsage["thread"])
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
	flags := commandFlags("reply", commandUsage["reply"], out)
	threadID := flags.String("thread", "", "thread identifier")
	body := flags.String("body", "", "Markdown reply")
	state := flags.String("state", "", "optionally set thread to open, resolved, or withdrawn")
	var attachments stringList
	flags.Var(&attachments, "attachment", "attachment; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["reply"])
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
	flags := commandFlags("review", commandUsage["review"], out)
	target := flags.String("target", ".", "saga, chapter, section, or fragment path")
	state := flags.String("state", "", "approved, rejected, closed, or open")
	body := flags.String("body", "", "optional review note")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["review"])
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
	flags := commandFlags("validate", commandUsage["validate"], out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON")
	fix := flags.Bool("fix", false, "add missing stable anchors to Markdown headings in narrative fragments")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["validate"])
	}
	fixes := []AnchorFix{}
	if *fix {
		applied, err := fixHeadingAnchors(flags.Arg(0))
		if err != nil {
			return err
		}
		fixes = applied
	}
	_, validation, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeJSON(out, validationOutput{Validation: validation, Fixes: fixes}); err != nil {
			return err
		}
	} else {
		for _, applied := range fixes {
			fmt.Fprintf(out, "Fixed %s:%d heading %q now anchors as {#%s}\n", applied.Path, applied.Line, applied.Heading, applied.Anchor)
		}
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

// AnchorFix is one heading that --fix gave a stable anchor.
type AnchorFix struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Heading string `json:"heading"`
	Anchor  string `json:"anchor"`
}

// validationOutput keeps validate --json a single JSON value while still
// reporting what --fix changed. "fixes" is always present, empty when nothing
// was rewritten, so a consumer never has to test for the key.
type validationOutput struct {
	saga.Validation
	Fixes []AnchorFix `json:"fixes"`
}

// fixHeadingAnchors is the only mutating part of validate. It rewrites narrative
// Markdown fragments in place and deliberately never touches review-overlay
// fragments: thread messages are append-only history, not authored content.
func fixHeadingAnchors(root string) ([]AnchorFix, error) {
	var applied []AnchorFix
	err := authorMutation(root, func(document *saga.Saga) error {
		var walk func(*saga.Section) error
		walk = func(section *saga.Section) error {
			for _, fragment := range section.Fragments {
				if fragment.MediaType != "text/markdown" || fragment.Entrypoint == "" {
					continue
				}
				entrypoint := filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))
				content, err := os.ReadFile(entrypoint)
				if err != nil {
					// A fragment whose entrypoint cannot be read is already an
					// invalid saga; report that through validation rather than
					// failing the fix of every other fragment.
					continue
				}
				fixed, added := saga.FixMarkdownHeadingAnchors(content, reservedLandmarkIDs(fragment))
				if len(added) == 0 {
					continue
				}
				if err := store.WriteFile(entrypoint, fixed, 0o644, false); err != nil {
					return err
				}
				for _, anchor := range added {
					applied = append(applied, AnchorFix{Path: filepath.ToSlash(filepath.Join(fragment.Path, fragment.Entrypoint)), Line: anchor.Line, Heading: anchor.Heading, Anchor: anchor.Anchor})
				}
			}
			for _, child := range section.Children {
				if err := walk(child); err != nil {
					return err
				}
			}
			return nil
		}
		return walk(document.Section)
	})
	return applied, err
}

// reservedLandmarkIDs keeps a generated heading anchor from claiming an id a
// non-heading landmark already owns, which the loader rejects as a conflict.
func reservedLandmarkIDs(fragment *saga.Fragment) map[string]bool {
	reserved := map[string]bool{}
	for index := range fragment.Landmarks {
		landmark := &fragment.Landmarks[index]
		if landmark.Selector.Type != "heading" {
			reserved[landmark.ID] = true
		}
	}
	return reserved
}

func Status(ctx context.Context, args []string, out io.Writer) error {
	flags := commandFlags("status", commandUsage["status"], out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON")
	maxItems := flags.Int("max", 100, "maximum uncovered items in text mode; 0 means all")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["status"])
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
	state := "MAPPING GAPS"
	if report.Complete {
		state = "ALL ATOMS MAPPED"
	}
	fmt.Fprintf(out, "%s — %d/%d product changes mapped\n", state, report.Summary.Covered, report.Summary.Total)
	fmt.Fprintln(out, "Mapping detects omissions; it does not establish explanation quality or correctness.")
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

// Serve runs the loopback reviewer. It is reached as both "serve" and "open",
// which differ only in whether a browser is launched, so the flag set is named
// after the command the user actually typed and its -h says so.
func Serve(ctx context.Context, args []string, out io.Writer, openByDefault ...bool) error {
	opensBrowser := len(openByDefault) > 0 && openByDefault[0]
	name := "serve"
	if opensBrowser {
		name = "open"
	}
	if name == "serve" && len(args) > 0 && (args[0] == "status" || args[0] == "stop") {
		return manageDetachedServers(ctx, args[0], args[1:], out)
	}
	flags := commandFlags(name, commandUsage[name], out)
	addr := flags.String("addr", "127.0.0.1:7342", "loopback listen address; remote serving is disabled")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	openBrowser := flags.Bool("open", opensBrowser, "open the review in a browser")
	detach := flags.Bool("detach", false, "run in the background and return the PID and active URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage[name])
	}
	if *detach {
		if !flagWasSet(flags, "addr") {
			*addr = "127.0.0.1:0"
		}
		return startDetachedServer(ctx, flags.Arg(0), *repoDir, *addr, *openBrowser, out)
	}
	if statePath, token := os.Getenv(runtimeStateEnv), os.Getenv(runtimeTokenEnv); statePath != "" && token != "" {
		return runManagedServer(ctx, flags.Arg(0), *repoDir, *addr, *openBrowser, statePath, token, out)
	}
	return reviewserver.Listen(ctx, flags.Arg(0), *repoDir, *addr, *openBrowser, out)
}

func Spec(args []string, out io.Writer) error {
	flags := commandFlags("spec", commandUsage["spec"], out)
	jsonOutput := flags.Bool("json", false, "emit the contract vocabulary as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: %s", commandUsage["spec"])
	}
	if *jsonOutput {
		return writeJSON(out, map[string]any{
			"version": 2, "chapter_suffix": ".chapter", "chapter_manifest": "chapter.json", "fragment_suffix": ".fragment", "fragment_manifest": "fragment.json",
			"hierarchy":     []string{"overview", "chapter", "section", "fragment"},
			"media_types":   []string{"text/markdown", "text/html", "text/plain", "image/svg+xml", "image/*"},
			"target_scheme": "urn:change-saga", "diff_scheme": "saga-diff://v1",
			"anchors":              []string{"target", "region", "drawing", "text", "note", "diff"},
			"thread_kinds":         []string{"comment", "suggestion"},
			"reviewer_bootstrap":   "README.md",
			"reserved_directories": []string{"___diffs", "___approvals", "___claims", "___verifications", "___review"},
			"author_assertions":    "one claim per ___claims/*.json; one append-only result per ___verifications/*.json",
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
	flags := commandFlags("install-skill", commandUsage["install-skill"], out)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: %s", commandUsage["install-skill"])
	}
	_, err := io.WriteString(out, installSkillPrompt())
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
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start != ranges[j].Start {
			return ranges[i].Start < ranges[j].Start
		}
		return ranges[i].End < ranges[j].End
	})
	canonical := ranges[:0]
	for _, current := range ranges {
		if len(canonical) == 0 || current.Start > canonical[len(canonical)-1].End && current.Start-canonical[len(canonical)-1].End > 1 {
			canonical = append(canonical, current)
			continue
		}
		if current.End > canonical[len(canonical)-1].End {
			canonical[len(canonical)-1].End = current.End
		}
	}
	return canonical, nil
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
	// On Windows, url.Parse interprets the drive letter in C:\repo as a URI
	// scheme. Recognize native absolute paths before parsing portable URIs.
	if filepath.IsAbs(value) {
		return diffuri.FileRepository(value)
	}
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
	if fragmentPath, landmarkID, ok := splitLandmarkTarget(value); ok {
		return resolveLandmark(document, fragmentPath, landmarkID, allowFragment)
	}
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
			return "", "", fmt.Errorf("target %q does not exist%s", value, targetHint(document, allowFragment))
		}
		return foundDir, value, nil
	}
	if dir, target, found, err := resolveTargetID(document, value, allowFragment); found || err != nil {
		return dir, target, err
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
		return "", "", fmt.Errorf("target %q is not a valid %s%s", value, map[bool]string{true: "chapter, section, fragment, or landmark", false: "chapter or section"}[allowFragment], targetHint(document, allowFragment))
	}
	return abs, foundTarget, nil
}

// resolveTargetID makes the stable IDs printed by authoring commands usable
// everywhere a target is accepted. Paths and URNs remain supported, but an
// agent no longer has to rediscover a full URN merely to put a section under a
// chapter it just created. Landmark IDs may repeat across fragments, so an
// ambiguous shorthand is rejected with the matching URNs instead of guessed.
func resolveTargetID(document *saga.Saga, value string, allowFragment bool) (string, string, bool, error) {
	if value == "" || value == "." || filepath.IsAbs(value) || strings.ContainsAny(value, `/\\#`) {
		return "", "", false, nil
	}
	type match struct{ dir, target string }
	var matches []match
	if document.Manifest.ID == value || document.Section.ID == value {
		matches = append(matches, match{document.Root, saga.SagaTarget(document.Manifest.ID)})
	}
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Path != "" && section.ID == value {
			matches = append(matches, match{filepath.Join(document.Root, filepath.FromSlash(section.Path)), section.Target})
		}
		if allowFragment {
			for _, fragment := range section.Fragments {
				if fragment.ID == value {
					matches = append(matches, match{fragment.Directory, fragment.Target})
				}
				for index := range fragment.Landmarks {
					landmark := &fragment.Landmarks[index]
					if landmark.ID == value {
						matches = append(matches, match{landmark.Directory, landmark.Target})
					}
				}
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	if len(matches) == 0 {
		return "", "", false, nil
	}
	if len(matches) > 1 {
		targets := make([]string, len(matches))
		for i := range matches {
			targets[i] = matches[i].target
		}
		sort.Strings(targets)
		return "", "", true, fmt.Errorf("target id %q is ambiguous; use one of: %s", value, strings.Join(targets, ", "))
	}
	return matches[0].dir, matches[0].target, true, nil
}

// splitLandmarkTarget recognizes the <fragment>#<landmark-id> shorthand. A
// landmark lives inside a reserved ___landmarks directory that ordinary section
// resolution refuses to enter, so without this an author had to spell out the
// full landmark URN before they had any way to discover it.
func splitLandmarkTarget(value string) (string, string, bool) {
	if strings.HasPrefix(value, "urn:change-saga:") {
		return "", "", false
	}
	hash := strings.LastIndex(value, "#")
	if hash < 0 {
		return "", "", false
	}
	return value[:hash], value[hash+1:], true
}

func resolveLandmark(document *saga.Saga, fragmentPath, landmarkID string, allowFragment bool) (string, string, error) {
	if !allowFragment {
		return "", "", fmt.Errorf("landmark targets cannot contain chapters, sections, or fragments")
	}
	if strings.TrimSpace(fragmentPath) == "" {
		return "", "", fmt.Errorf("landmark target %q must name its fragment, as <fragment-path>#<landmark-id>", "#"+landmarkID)
	}
	fragmentDir, fragmentTarget, err := resolveTarget(document, fragmentPath, true)
	if err != nil {
		return "", "", err
	}
	fragment := findFragment(document.Section, fragmentDir)
	if fragment == nil {
		return "", "", fmt.Errorf("%q is not a fragment, so it cannot contain landmark %q", fragmentPath, landmarkID)
	}
	for index := range fragment.Landmarks {
		landmark := &fragment.Landmarks[index]
		if landmark.ID == landmarkID {
			return landmark.Directory, landmark.Target, nil
		}
	}
	if len(fragment.Landmarks) == 0 {
		return "", "", fmt.Errorf("fragment %q (%s) declares no landmarks; add %s/___landmarks/%s.landmark/landmark.json before covering it", fragmentPath, fragmentTarget, fragment.Path, landmarkID)
	}
	available := make([]string, 0, len(fragment.Landmarks))
	for index := range fragment.Landmarks {
		available = append(available, fragment.Landmarks[index].ID)
	}
	return "", "", fmt.Errorf("fragment %q has no landmark %q; it declares %s", fragmentPath, landmarkID, strings.Join(available, ", "))
}

func findFragment(section *saga.Section, dir string) *saga.Fragment {
	dirAbs, _ := filepath.Abs(dir)
	var found *saga.Fragment
	var walk func(*saga.Section)
	walk = func(current *saga.Section) {
		for _, fragment := range current.Fragments {
			if fragmentAbs, _ := filepath.Abs(fragment.Directory); fragmentAbs == dirAbs {
				found = fragment
			}
		}
		for _, child := range current.Children {
			walk(child)
		}
	}
	walk(section)
	return found
}

// maxTargetHints bounds the suggestion list so a large saga still produces a
// readable error instead of dumping its whole target space.
const maxTargetHints = 12

// targetHint turns "that target does not exist" into something actionable. The
// query API is the supported way to enumerate targets, so the hint names it
// rather than inviting the reader to go read metadata files.
func targetHint(document *saga.Saga, allowFragment bool) string {
	var targets []string
	targets = append(targets, saga.SagaTarget(document.Manifest.ID))
	walkTargets(document.Root, document.Section, func(target, _ string, fragment bool) {
		if allowFragment || !fragment {
			targets = append(targets, target)
		}
	})
	sort.Strings(targets)
	hint := "; run \"change-saga query children --saga " + filepath.Base(document.Root) + " --parent " + saga.SagaTarget(document.Manifest.ID) + "\" to list targets"
	if len(targets) == 0 {
		return hint
	}
	shown := targets
	suffix := ""
	if len(shown) > maxTargetHints {
		shown, suffix = shown[:maxTargetHints], fmt.Sprintf(", and %d more", len(targets)-maxTargetHints)
	}
	return fmt.Sprintf("%s. Known targets: %s%s", hint, strings.Join(shown, ", "), suffix)
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
		return "content.md", nil, mediaType, nil
	case "text/plain":
		return "content.txt", nil, mediaType, nil
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

// installSkillPrompt renders the bootstrap prompt. The query operation list is
// generated from the same table the query command dispatches on, so an
// operation can never be added to the CLI and left out of the installed skill.
func installSkillPrompt() string {
	var operations strings.Builder
	for _, operation := range queryOperations {
		fmt.Fprintf(&operations, "   - `%s` — %s\n     `%s`\n", operation, queryPurpose[operation], queryUsage[operation])
	}
	return strings.Replace(installSkillTemplate, "{{query_operations}}\n", operations.String()+"\n", 1)
}

const installSkillTemplate = `Install or update a project-local agent skill named "change-saga" using this coding agent's native skill mechanism. Do not create a Change Saga as part of installation. Preserve the following behavior in the installed skill:

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

## Read the saga through the versioned query API

Never read, glob, or grep saga metadata files to learn what a saga contains.
Files under a .saga directory are an implementation detail and their layout is
not a compatibility promise. Use "change-saga query", the versioned read API,
for every read. It is deterministic, paginated, and safe to call concurrently;
it never starts a server and never mutates either repository.

Pass "--saga <path>" to every query, and "--repo <source-checkout>" when the
source repository is separate from the saga. The exception is "change-saga
query schema <operation>", which describes the operation's data paths and
pagination contract without opening a saga. Use it instead of probing or
guessing response shapes. Each cursor schema names the response collection
counted by "page.total" and "page.returned" as "pagination.counted_path".
Every invocation writes exactly one JSON envelope
carrying "schema", "ok", "snapshot", "data", and "page"; failures carry
"error.code". Branch on "ok" and "error.code"; never parse message text. For
cursor-paginated operations, the current page length at
"pagination.counted_path" must equal "page.returned". Follow
"page.next_cursor" while "page.has_more" is true and confirm the aggregate
count equals "page.total". Do not raise "--limit"
to silently swallow a partial result. Compare "snapshot" across calls to detect
a saga that changed underneath a multi-step read.

The operations are:

{{query_operations}}
Start at "query overview", walk one level at a time with "query children", and
read narrative content through "query fragment". "query children" is also how
you discover landmark targets: a fragment's children are its landmarks, and each
one reports the target URN to pass to "change-saga cover --target".
Hierarchy nodes report inclusive "diffs.current" and "diffs.stale" totals plus
"direct_current", "direct_stale", "descendant_current", and
"descendant_stale", so evidence owned by a landmark is not mistaken for a
fragment with no explained code.

## Author the saga

Use this authoring loop, consulting each command's "-h" output for exact flags:

1. "change-saga init" records the exact repository, base, head, title, and PR identity.
2. "change-saga query gaps --kind uncovered --saga <path>" pages the coverage
   work queue. Use "--kind stale" for reconciliation and "--kind overlap" for
   mappings that need justification; preserve the returned snapshot across the
   loop.
3. "change-saga add-chapter", "change-saga add-section", and "change-saga add-fragment" build the
   reviewer-oriented narrative and its Markdown, SVG, image, or HTML packages.
   Write or replace an entrypoint only with "change-saga set-fragment-content
   --target TARGET --source FILE|-"; do not edit fragment package files directly.
4. "change-saga add-landmark" makes a Markdown heading, HTML/SVG element, exact
   text, or image region independently addressable. Give every meaningful
   visual landmark a semantic "--description" that explains its role without
   relying on geometry, color, or position. Every concrete prose claim about
   implementation, behavior, an invariant, or a data transition must carry a
   focused Markdown footnote citation or live under a deliberately
   evidence-bearing heading. Make each plain-text footnote definition an
   exact-text landmark and attach its exact evidence there. Do not finish a
   substantive implementation narrative with zero citations merely because
   its atoms are covered at fragment scope. Give each code-bearing SVG/HTML
   node, edge, arrow, transition, or state its own stable element ID and
   evidence-bearing landmark instead of covering only the enclosing fragment.
   SVG element bounds become on-canvas links automatically; use "--hotspot"
   only to override awkward geometry.
5. "change-saga cover" connects a focused fragment or landmark to the exact diff atoms
   it explains and includes a concise what-and-why note. "--target" accepts a
   path, a target URN, or the "<fragment-path>#<landmark-id>" shorthand. Use
   "--dry-run" to see exactly which records an invocation would write before
   writing them. When every changed atom in one file genuinely belongs to the
   same target, "--path FILE --changed-lines" derives its exact old/new line
   atoms, coalesces gapless lines into canonical dense ranges, and includes file
   events such as "add" automatically. Never use it to hide multiple concerns
   in one broad record. Generated evidence paths identify their selector set,
   so a second explanation for the same selectors requires "replace-coverage"
   rather than creating a duplicate record.
6. When attaching many selectors, pipe newline-delimited JSON records to
   "change-saga cover --batch -". Each record carries its own "target", "path",
   "side", "lines", "changed_lines", "event", "old_path", "new_path", "note", and "name". The
   whole batch is resolved before anything is written and a failing record
   leaves the saga untouched, so a batch is a delivery optimization only: every
   record still maps the exact atoms it explains, never a widened range.
7. Run "change-saga query mappings --sort scrutiny" and use each
   "evidence_file" as the stable repair handle. "change-saga replace-coverage
   --record PATH --batch -" atomically splits, retargets, or rewrites a record;
   "change-saga remove-coverage --record PATH" deletes one. Prefer
   landmark-level ownership when the score explains that a mapping deserves
   more skepticism. The score is a work queue, not a correctness grade.
   Then use "query children" and "query fragment-diffs" to audit every
   substantive fragment. Move direct fragment-level evidence to citations,
   headings, SVG nodes, or SVG edges whenever the authored content identifies
   that narrower target.
8. Record falsifiable assertions with "change-saga add-claim" and append an
   explicit result with "change-saga verify-claim". Claims never contribute to
   coverage. Use "unverified" when an assertion has not actually been checked;
   otherwise record the reproducible method and command when applicable.
9. Repeat status, then run "change-saga validate --json" before "change-saga open" presents
   the authored proposal for review. "change-saga validate --fix" adds missing
   stable anchors to Markdown headings and changes nothing else.

## Maintain a codebase Saga

Use "change-saga compare --json --repo <source-checkout> --base <incoming-base>
--head <incoming-head> <maintained.saga>" or supply "--against-saga
<incoming.saga>". This compares source diffs only, never authored fragment
content. Follow "must_update" targets for direct conflicting intersections,
"consider_update" targets for additions near owned code, and "new_content" for
ownerless changes. Use the returned target URNs, "content_path", and
"evidence_files" as the maintenance work queue. If "baseline.complete" is
false, repair the maintained Saga at the incoming base before treating the
impact list as exhaustive.

Lead with pictures and show by example. The root should establish the goal,
system/change map, affected workflows, and chapter path before dense prose.
Every substantial chapter should begin with an SVG diagram, self-contained
interactive HTML walkthrough, or concrete before/after example. Highlight
end-to-end workflows, data flows, data models, state transitions, boundaries,
failure paths, compatibility, and observable outcomes. Give meaningful diagram
nodes, edges, and interactive elements stable landmarks with "change-saga
add-landmark" so they can link to the exact code that realizes them. In prose,
use Markdown footnote citations for every concrete implementation claim and map
the exact-text reference definition to its supporting diff atoms; the renderer
makes both the inline marker and reference entry open those diffs. A
citation-free implementation narrative is unfinished even when status reports
complete coverage. Audit every visual before moving on: enumerate its
meaningful nodes and edges and attach focused evidence to each code-bearing
landmark. A visual with no landmarks or direct code mapping is unfinished, not
decorative completeness.

Organize the change into independently reviewable chapters based on behavior,
risk, architecture, or reviewer intent rather than file type. Use Markdown to
orient and connect visual artifacts, not as the default container for the whole
explanation.

Run "change-saga query gaps --kind uncovered --saga <name>.saga" as the coverage
work queue. Attach only the exact diff atoms a fragment or landmark explains, with
concise notes saying what changed and why that content owns it. Never widen
mappings only to reach 100 percent. Iterate until every product change is
mapped and no mapping is stale, then inspect "query mappings --sort scrutiny".
All-atoms-mapped is an omission invariant, not proof of explanation quality or
correctness. Then run "change-saga validate --json"
and "change-saga status --json" before handoff.

Use "--json" for bounded machine-readable coverage mutation summaries and
"--quiet" when no successful output is needed. Use "change-saga open --detach"
for a managed background reviewer, then inspect or stop it with "change-saga
serve status" and "change-saga serve stop".

Never leave generated instructions, blank scaffold fragments, or example
diagram/HTML content in the handed-off saga. Treat every validation warning as
an authoring task unless it is explicitly justified. For a PR request, verify
the provider-reported head OID/branch and changed files match the inspected
checkout; never guess a PR number, and omit PR metadata rather than recording
an unverified identity.

Opening the saga presents the authored proposal for a human to review. It does
not authorize the agent to conduct that review. When explicitly asked to review
one, first read the code diff independently and record provisional findings;
then inspect mappings, claims, verifications, and narrative intent; finally
reconcile contradictions and independently test author claims. Do not let the
author's explanation anchor the first correctness pass.
`

const defaultSVGFragment = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 300" role="img" aria-label="Diagram placeholder">
  <rect width="800" height="300" rx="24" fill="#eef3ee"/><text x="400" y="150" text-anchor="middle" dominant-baseline="middle" font-family="system-ui" font-size="28" fill="#244235">Replace with a useful diagram</text>
</svg>
`

const specText = `Change Saga format v2 (experimental)

A saga root includes a reviewer-facing README.md with safe installation,
opening, and structured-query guidance. The file is informational bootstrap
material rather than authored narrative or diff evidence.

A saga begins with its overview and direct *.chapter directories. Chapters are
independently reviewable boundaries roughly corresponding to the PRs one might
have created when splitting the change. Sections recurse inside chapters and
contain directory-backed fragments. Each *.fragment has fragment.json and an
entrypoint. Markdown, HTML, SVG, text, and raster images are supported. HTML and SVG may bundle JavaScript
and assets; the reference viewer executes them in sandboxed frames.

Any saga, chapter, section, or fragment may own ___diffs/*.json evidence. Every
evidence entry is an absolute saga-diff://v1 URI containing repository URI, immutable
base/head identities, and a line range or file event.

Addressable Markdown headings, exact text, HTML/SVG elements, and image regions
live in independent ___landmarks/<id>.landmark packages beneath a fragment.
Create them with change-saga add-landmark, then pass the printed path or URN to
change-saga cover so a reviewer can move directly from a visual node to code.
For SVG fragments, --element-id identifies the node and its rendered bounds
automatically become the on-canvas link. Use --hotspot only to override that
geometry. HTML element landmarks need an explicit hotspot to appear on-canvas;
without one they remain reachable through Marked places and deep links.
Meaningful visual landmarks include a semantic description so query clients can
understand their role without interpreting SVG or HTML geometry.
Markdown footnotes are the prose citation convention. When a footnote
definition is an exact-text landmark with evidence, the renderer turns both its
inline reference and footer entry into controls that open the linked diffs.

Falsifiable author assertions live as independent ___claims/<id>.json records.
Append-only ___verifications/<id>.json records mark them unverified, verified,
failed, or inconclusive and preserve the method and reproducible command. Claim
evidence never contributes to diff coverage. Git history supplies attribution.

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

All-atoms-mapped is an omission invariant, not a correctness or explanation-
quality verdict. Use query mappings --sort scrutiny to inspect broad or thin
coverage records, and query claims/verifications to independently test author
assertions.

The authoritative specification is SPEC.md in the Change Saga repository.
`
