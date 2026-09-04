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
	"init", "upgrade", "story", "citation", "relation", "design", "plan", "add-deck", "add-slide", "set-slide-content", "add-item", "add-chapter", "add-section", "add-fragment", "set-fragment-content", "add-landmark", "cover", "remove-coverage", "replace-coverage", "rebase-evidence", "add-claim", "verify-claim",
	"thread", "reply", "review", "validate", "status", "compare", "query",
	"serve", "open", "install-skill", "spec",
}

var commandUsage = map[string]string{
	"init":                        "change-saga init [flags] <name.saga>",
	"upgrade":                     "change-saga upgrade --to 3 [--json] <saga>",
	"story":                       "change-saga story <add|revise|set-state> [flags] <saga>",
	"story add":                   "change-saga story add --id ID --revision ID --event ID --title TEXT --statement TEXT --priority TEXT [flags] <saga>",
	"story revise":                "change-saga story revise --story URN --revision ID --parent URN... --title TEXT --statement TEXT --priority TEXT [flags] <saga>",
	"story set-state":             "change-saga story set-state --story URN --event ID --parent URN... --state STATE [flags] <saga>",
	"citation":                    "change-saga citation add [flags] <saga>",
	"citation add":                "change-saga citation add --id ID --kind KIND --title TEXT --reference LOCATOR [flags] <saga>",
	"relation":                    "change-saga relation <add|supersede> [flags] <saga>",
	"relation add":                "change-saga relation add --id ID --type TYPE --from URN --to URN --rationale TEXT [flags] <saga>",
	"relation supersede":          "change-saga relation supersede --relation URN [--request-id ID] [--json] <saga>",
	"design":                      "change-saga design <operation> [flags] <saga>",
	"design-add-chapter":          "change-saga design add-chapter [flags] <saga> <name>",
	"design-add-section":          "change-saga design add-section [flags] <saga> <section/path>",
	"design-add-fragment":         "change-saga design add-fragment [flags] <saga>",
	"design-set-fragment-content": "change-saga design set-fragment-content --target TARGET --source FILE|- [--json|--quiet] <saga>",
	"plan":                        "change-saga plan <add-wave|revise-wave|add-item|revise-item|add-dependency|add-contract|assign|progress|record-merge> [flags] <saga>",
	"plan add-wave":               "change-saga plan add-wave --id ID --revision ID --title TEXT --objective TEXT --request-id ID [flags] <saga>",
	"plan revise-wave":            "change-saga plan revise-wave --wave URN --revision ID --parent URN... --title TEXT --objective TEXT --request-id ID [flags] <saga>",
	"plan add-item":               "change-saga plan add-item --id ID --revision ID --title TEXT --objective TEXT --deliverable TEXT... --request-id ID [flags] <saga>",
	"plan revise-item":            "change-saga plan revise-item --item URN --revision ID --parent URN... --title TEXT --objective TEXT --deliverable TEXT... --request-id ID [flags] <saga>",
	"plan add-dependency":         "change-saga plan add-dependency --id ID --prerequisite URN --dependent URN --condition KIND --reason TEXT --request-id ID [flags] <saga>",
	"plan add-contract":           "change-saga plan add-contract --id ID --revision ID --kind KIND --provider URN --consumer URN --statement TEXT --acceptance TEXT... --request-id ID [flags] <saga>",
	"plan assign":                 "change-saga plan assign --item URN --workspace UUID --repository-id ID --branch NAME --request-id ID [flags] <saga>",
	"plan progress":               "change-saga plan progress --item URN --from EVENT... --to STATE --request-id ID [flags] <saga>",
	"plan record-merge":           "change-saga plan record-merge --item URN --unit ID --state STATE --request-id ID [flags] <saga>",
	"add-deck":                    "change-saga add-deck [flags] <saga> <name>",
	"add-slide":                   "change-saga add-slide --deck TARGET --intent INTENT --layout LAYOUT [flags] <saga> <name>",
	"set-slide-content":           "change-saga set-slide-content --target TARGET --source FILE|- [--json|--quiet] <saga>",
	"add-item":                    "change-saga add-item --slide TARGET --kind KIND [selector] [flags] <saga>",
	"add-chapter":                 "change-saga add-chapter [flags] <saga> <name>",
	"add-section":                 "change-saga add-section [flags] <saga> <section/path>",
	"add-fragment":                "change-saga add-fragment [flags] <saga>",
	"set-fragment-content":        "change-saga set-fragment-content --target TARGET --source FILE|- [--json|--quiet] <saga>",
	"add-landmark":                "change-saga add-landmark [flags] <saga>",
	"cover":                       "change-saga cover [flags] [--batch FILE|-] [--dry-run] <saga>",
	"remove-coverage":             "change-saga remove-coverage --record PATH [--dry-run] [--json|--quiet] <saga>",
	"replace-coverage":            "change-saga replace-coverage --record PATH [coverage flags] [--batch FILE|-] [--dry-run] <saga>",
	"rebase-evidence":             "change-saga rebase-evidence [--repo PATH] [--carry-verifications] [--dry-run] [--json|--quiet] <saga>",
	"add-claim":                   "change-saga add-claim --target TARGET --kind KIND --statement TEXT --diff URI [--diff URI...] <saga>",
	"verify-claim":                "change-saga verify-claim --claim ID --status STATUS --summary TEXT [flags] <saga>",
	"thread":                      "change-saga thread [flags] <saga>",
	"reply":                       "change-saga reply [flags] <saga>",
	"review":                      "change-saga review [flags] <saga>",
	"validate":                    "change-saga validate [--json] [--fix] <saga>",
	"status":                      "change-saga status [--json] [--repo PATH] <saga>",
	"compare":                     "change-saga compare [--json] [--repo PATH] (--against-saga PATH | --base REV [--head REV]) <saga>",
	"query":                       "change-saga query <operation> --saga PATH [--repo PATH] [operation flags]",
	"serve":                       "change-saga serve [--addr ADDR] [--repo PATH] [--open] [--detach] <saga>",
	"open":                        "change-saga open [--addr ADDR] [--repo PATH] <saga>",
	"install-skill":               "change-saga install-skill",
	"spec":                        "change-saga spec [--json]",
}

func PrintHelp(out io.Writer) {
	fmt.Fprint(out, `Change Saga — make every part of a large change reviewable

Choose the workflow:
  Visual review deck (v4 preview)
    Start with "init --mode slides". Explain the change as a sequence of 16:9
    visual arguments whose form matches the relationship being explained—not
    a repeated card template. Every meaningful node, edge, region, and callout
    is an Item; exact diff evidence and precise comments attach to Items, while
    approval applies explicitly to each complete slide.

  Existing implementation or PR
    Use a Saga when the change is large enough to need a guided review across
    multiple behaviors, risks, systems, or workstreams. For a small focused
    change, a normal PR may be enough. Author this Saga from the completed work
    and its exact diff; requirements, prototypes, design, and work plan are optional.

  New feature or exploration
    Start a living Saga early. Prototype the UX and UI, draft user stories with
    acceptance criteria, develop the technical design, then organize delivery
    into dependency-aware waves of parallel workspaces that converge cleanly.
    These are overlapping surfaces, not gates: prototypes and stories can
    inform each other while design and work planning continue to mature.

  Parallel by design
    Partition Saga work by stable stories, prototypes, design fragments, and
    work items. They are Git-native and intended to merge as agents fan out and
    consolidate. Before peer review, connect the delivered commits and exact
    diffs, verify acceptance-criterion coverage, and validate the whole Saga.

Usage:
`)
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
	"init":                        "Start a reviewer guide or living Saga. Small focused changes may not need a Saga.\nChoose --mode slides for the intentionally incompatible v4 visual review format;\nexisting reports are never silently paginated.",
	"upgrade":                     "Atomically adopt the v3 Saga container. Existing v2 narrative and review\nrecords are preserved; requirements, design, and work-plan roots remain optional.",
	"story":                       "Create and append revisions or lifecycle events to user stories and acceptance\ncriteria. Stories may lead, follow, or evolve alongside prototypes; cite their source\nand revise them as the feature is clarified.",
	"story add":                   "Add a sourced user story and its first complete acceptance-criteria revision.\nIt may begin from a prototype, precede one, or evolve alongside one.",
	"story revise":                "Append a complete story revision as requirements or prototypes evolve. Name every\ncurrent parent head when reconciling concurrent edits; prior revisions remain history.",
	"story set-state":             "Append a lifecycle decision without rewriting the story. Acceptance records intent,\nnot implementation completion or peer-review approval.",
	"citation":                    "Create immutable provenance records for requirements and design decisions.",
	"citation add":                "Record where a requirement or decision came from: an external URL, issue, document,\nrepository commit, or recorded decision. Provenance is context, not delivery evidence.",
	"relation":                    "Create or explicitly supersede pinned traceability relations.",
	"relation add":                "Link stories, criteria, design, work items, and evidence with a typed rationale.\nPin mutable endpoints to their current revision or content digest so later edits go stale.",
	"relation supersede":          "Retire one relation without erasing it. Add its corrected replacement separately;\na pivot is represented by normal requirement, design, plan, and relation revisions.",
	"design":                      "Develop technical-design chapters, sections, and fragments that trace to user\nstories and acceptance criteria. Design may evolve alongside prototypes and requirements;\nits addressable packages are partitioned for parallel authoring and clean merges.",
	"design-add-chapter":          "Add one independently authored technical-design concern. Chapters may be developed\nin parallel and should cite the requirements their contained design addresses.",
	"design-add-section":          "Partition a design chapter around one coherent subsystem, workflow, or decision so\nconcurrent agents can work without contending on a shared fragment.",
	"design-add-fragment":         "Add an addressable technical-design artifact. Prefer a diagram, prototype walkthrough,\nor concrete example and relate it to the stories and acceptance criteria it develops.",
	"design-set-fragment-content": "Replace authored design content while preserving its stable target. Refresh\ncontent-digest-pinned relations after intentional changes so downstream work is not stale.",
	"plan":                        "Turn requirements and design into dependency-aware waves, parallel workspace\nassignments, explicit convergence, progress, and immutable merge evidence. Planning\nmay begin while the technical design is still maturing and must track later revisions.",
	"plan add-wave":               "Add one delivery phase with explicit entry and exit conditions. Waves describe when\nparallel workspace lanes may fan out or must converge; display order is not dependency.",
	"plan revise-wave":            "Append a complete wave revision as sequencing or convergence changes. Reconcile every\ncurrent parent head so concurrent planning remains visible until intentionally merged.",
	"plan add-item":               "Add one independently assignable, mergeable unit of work and link it to the\nrequirements and design it advances. Keep ownership narrow enough for parallel work.",
	"plan revise-item":            "Append a complete work-item revision when scope, touch areas, deliverables, or merge\nunits change. Preserve prior plans and refresh revision-pinned downstream contracts.",
	"plan add-dependency":         "Record a real prerequisite between work items. Do not serialize independent items;\nuse dependencies to express the minimum safe fan-out and convergence graph.",
	"plan add-contract":           "Define the versioned interface between parallel provider and consumer work items.\nUse its acceptance checks as the stable seam that lets both workspaces proceed safely.",
	"plan assign":                 "Bind a work item to a concrete workspace and branch so progress can be shown in the\nlive Saga. Assignment is coordination state, not evidence of implementation.",
	"plan progress":               "Append explicit workspace progress against the item. Progress helps coordination but\nnever proves correctness, acceptance-criterion coverage, or delivery.",
	"plan record-merge":           "Append merge evidence for a declared merge unit. A merged state contributes delivery\nevidence only when its immutable commit and diff links resolve.",
	"add-deck":                    "Add an independently reviewable deck to a v4 slide-native Saga. Exactly one deck is\nthe overview; change decks organize one coherent reviewer concern.",
	"add-slide":                   "Add one visual argument to a v4 deck. Intent names the reviewer job; layout names\ngeometry, not meaning. Establish the system model, then foreground consequential\ntradeoffs, hidden coupling, and deviations that may surprise a reviewer.",
	"set-slide-content":           "Replace a slide's visual entrypoint while preserving its stable target and items.",
	"add-item":                    "Add one semantic visual item, including an evidence-bearing callout overlay, and append\nit to the slide reading order. Exact diff evidence attaches here.",
	"set-fragment-content":        "Replace a fragment entrypoint through the supported authoring API. Use --source -\nto read content from standard input; the fragment media type and metadata are preserved.",
	"add-chapter":                 "Add one independently reviewable chapter to a legacy v2/v3 report Saga. New visual\npresentations use add-deck in the intentionally separate v4 format.",
	"add-section":                 "Group related narrative content inside a legacy report chapter.",
	"add-fragment":                "Add a narrative artifact to a legacy v2/v3 report Saga. New slide-native Sagas use\nadd-slide with a visual entrypoint and semantic evidence-bearing Items.",
	"add-landmark":                "Create a coverable target for one Markdown heading, exact text span, HTML/SVG\nelement, or normalized image region inside a fragment. An SVG --element-id is\nmeasured into an on-canvas link automatically; --hotspot overrides its bounds.\nHTML elements need --hotspot for an on-canvas link. Visual landmarks require a\nsemantic --description for non-visual consumers.",
	"cover": `Attach the exact diff atoms a review target explains. In v4 the target must
be an Item. Report mode also accepts section/fragment paths, target URNs, and
<fragment-path>#<landmark-id>.
--batch reads newline-delimited JSON records (or one JSON array) with the
per-record fields target, path, side, lines, changed_lines, event, old_path,
new_path, note, name, and uris; the whole batch is resolved before anything is written, and a
failing record leaves the saga untouched.`,
	"remove-coverage":  "Delete one exact coverage record named by query mappings or fragment-diffs.",
	"replace-coverage": "Atomically replace one coverage record with one or more newly resolved records.\nUse --batch to split or retarget broad evidence without leaving partial coverage.",
	"rebase-evidence":  "Refresh exact evidence after the Saga's declared base moves while the product diff is\nbyte-for-byte identical. The command refuses changed product identity, previews with\n--dry-run, rolls immutable claims forward, and only carries verification when requested.",
	"add-claim":        "Record one falsifiable author assertion and its exact supporting diff evidence.\nClaims do not count toward coverage and are independently verified.",
	"verify-claim":     "Append an independent verification result without rewriting the claim or prior results.",
	"open":             "Start a managed loopback reviewer, open it in a browser, and return after\nprinting the PID and active URL.",
	"serve":            "Serve the saga on loopback for review. Detached instances are managed with\nchange-saga serve status [SAGA] and change-saga serve stop [SAGA].",
	"install-skill":    "Print the agent-agnostic prompt that installs the change-saga authoring skill.\nPipe it to a coding agent; it neither writes to this repository nor creates a saga.",
	"validate":         "Check the format and authoring completeness, including a warning for every Markdown\nfootnote without an evidence-bearing exact-text landmark. --fix adds missing stable\nheading anchors and changes nothing else.",
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
	mode := flags.String("mode", "report", "document mode: report or slides (intentionally incompatible)")
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
	if *mode != "report" && *mode != "slides" {
		return fmt.Errorf("--mode must be report or slides")
	}
	repositoryURI, _, err := discoverRepository(ctx, *repoDir, *repository, *allowLocalRepository, *allowRepositoryMismatch)
	if err != nil {
		return err
	}
	manifest := saga.Manifest{Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: *id, Title: *title, Source: saga.Source{Repository: repositoryURI, Base: *base, Head: *head}}
	if *mode == "slides" {
		manifest.Schema = saga.V4SchemaURL
		manifest.Version = saga.SlideSagaVersion
		manifest.Presentation = &saga.Presentation{Mode: "slides", AspectRatio: "16:9", OverviewDeck: "overview"}
	}
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
	if *mode == "slides" {
		if err := saga.ValidateFlatRoot(absRoot); err != nil {
			return err
		}
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
	if *mode == "slides" {
		if err := saga.ValidateFlatRoot(filepath.Join(parent, filepath.Base(absRoot))); err != nil {
			return err
		}
	}
	err = store.CommitDir(parent, filepath.Join(parent, filepath.Base(absRoot)), func(stage string) error {
		if err := os.Chmod(stage, 0o755); err != nil {
			return err
		}
		reservedDirs := []string{"___approvals", "___claims", "___verifications", filepath.Join("___review", "threads"), filepath.Join("___review", "diffs"), "___diffs"}
		if *mode == "slides" {
			reservedDirs = nil
		}
		for _, dir := range reservedDirs {
			if err := os.MkdirAll(filepath.Join(stage, dir), 0o755); err != nil {
				return err
			}
		}
		manifestName := "saga.json"
		if *mode == "slides" {
			manifestName = saga.FlatManifestName
		}
		if err := store.WriteJSON(filepath.Join(stage, manifestName), manifest, true); err != nil {
			return err
		}
		readme := reviewerBootstrapREADME
		if *mode == "slides" {
			readme = slideNativeBootstrapREADME
		}
		readmeName := "README.md"
		if *mode == "slides" {
			readmeName = "01-readme.md"
		}
		if err := store.WriteFile(filepath.Join(stage, readmeName), []byte(readme), 0o644, true); err != nil {
			return err
		}
		if *mode == "slides" {
			deck := saga.DeckManifest{Version: saga.SlideSagaVersion, ID: "overview", Title: "Overview", Role: "overview", Rank: 0, Objective: "Orient the reviewer to the change and its review path."}
			name, err := saga.FlatDeckFilename(saga.DeckTarget(manifest.ID, deck.ID), deck.Rank)
			if err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(stage, name), deck, true)
		}
		return populateFragment(filepath.Join(stage, "overview.fragment"), overview, "", nil)
	})
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists", root)
	}
	if err != nil {
		return err
	}
	if *mode == "slides" {
		fmt.Fprintf(out, "Created slide-native %s\nNext: change-saga add-slide --deck overview --intent orient --layout hero --title \"What changed\" %s change-overview\n", root, root)
	} else {
		fmt.Fprintf(out, "Created %s\nNext: change-saga add-chapter --title \"Architecture\" %s architecture\n", root, root)
	}
	return nil
}

func AddChapter(ctx context.Context, args []string, out io.Writer) error {
	return addChapter(ctx, args, out, narrativeAuthoring)
}

func addChapter(_ context.Context, args []string, out io.Writer, scope authoringScope) error {
	command := scope.command("add-chapter")
	flags := commandFlags(command, commandUsage[command], out)
	title := flags.String("title", "", "chapter title")
	id := flags.String("id", "", "stable chapter identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: %s", commandUsage[command])
	}
	name := strings.TrimSuffix(flags.Arg(1), ".chapter")
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Base(name) != name || strings.HasPrefix(name, "___") {
		return fmt.Errorf("chapter name must be a single non-reserved path component")
	}
	var created, createdID, createdTarget string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if document.Manifest.Version == saga.SlideSagaVersion {
			return fmt.Errorf("add-chapter is unavailable for v4 slide-native Sagas; use add-deck")
		}
		hierarchyRoot, err := scope.hierarchyRoot(document)
		if err != nil {
			return err
		}
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
		dir := filepath.Join(hierarchyRoot, store.Slug(name)+".chapter")
		manifest := saga.ChapterManifest{Version: saga.CurrentVersion, ID: chapterID, Title: chapterTitle, Order: *order}
		overview := saga.FragmentManifest{Version: saga.CurrentVersion, ID: overviewID, MediaType: "text/markdown", Entrypoint: "content.md"}
		// One staged rename: a failed chapter never leaves a manifest-less
		// directory behind that would invalidate the saga for every later
		// command.
		err = store.CommitDir(document.Root, dir, func(stage string) error {
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
		rel, _ := filepath.Rel(document.Root, dir)
		created = filepath.ToSlash(rel)
		createdID = chapterID
		createdTarget = saga.ChapterTarget(document.Manifest.ID, chapterID)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added chapter %s\n", created)
	fmt.Fprintf(out, "Target: %s\n", createdTarget)
	fmt.Fprintf(out, "Next: %s --title \"Section title\" %s %s/section-name\n", scope.commandText("add-section"), flags.Arg(0), createdID)
	return nil
}

func AddSection(ctx context.Context, args []string, out io.Writer) error {
	return addSection(ctx, args, out, narrativeAuthoring)
}

func addSection(_ context.Context, args []string, out io.Writer, scope authoringScope) error {
	command := scope.command("add-section")
	flags := commandFlags(command, commandUsage[command], out)
	title := flags.String("title", "", "section title")
	id := flags.String("id", "", "stable section identifier")
	order := flags.Int("order", 0, "display order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: %s", commandUsage[command])
	}
	sectionPath := filepath.Clean(flags.Arg(1))
	parentPath, name := filepath.Dir(sectionPath), filepath.Base(sectionPath)
	if parentPath == "." || name == "." || strings.HasPrefix(name, "___") || strings.HasSuffix(name, ".chapter") || strings.HasSuffix(name, ".fragment") {
		return fmt.Errorf("sections must be created inside an existing chapter or section")
	}
	var created, createdID, createdTarget string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if document.Manifest.Version == saga.SlideSagaVersion {
			return fmt.Errorf("add-section is unavailable for v4 slide-native Sagas; use add-slide")
		}
		parentDir, _, err := scope.resolveTarget(document, parentPath, false)
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
	fmt.Fprintf(out, "Next: %s --section %s --title \"Fragment title\" %s\n", scope.commandText("add-fragment"), createdID, flags.Arg(0))
	return nil
}

func AddFragment(ctx context.Context, args []string, out io.Writer) error {
	return addFragment(ctx, args, out, narrativeAuthoring)
}

func addFragment(_ context.Context, args []string, out io.Writer, scope authoringScope) error {
	command := scope.command("add-fragment")
	flags := commandFlags(command, commandUsage[command], out)
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
		return fmt.Errorf("usage: %s", commandUsage[command])
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
		if document.Manifest.Version == saga.SlideSagaVersion {
			return fmt.Errorf("add-fragment is unavailable for v4 slide-native Sagas; use add-slide")
		}
		sectionDir, _, err := scope.resolveTarget(document, *section, false)
		if err != nil {
			return err
		}
		fragmentName, fragmentID := *name, *id
		if fragmentName == "" {
			fragmentName = store.Slug(firstNonEmpty(*title, fragmentID, *kind))
		}
		if fragmentID == "" {
			sectionName := filepath.ToSlash(*section)
			if scope.design {
				sectionName = "design-" + sectionName
			}
			fragmentID = store.Slug(document.Manifest.ID + "-" + strings.ReplaceAll(sectionName, "/", "-") + "-" + fragmentName)
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
	fmt.Fprintf(out, "Next: %s --target %s --source FILE|- %s\n", scope.commandText("set-fragment-content"), createdTarget, flags.Arg(0))
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
	target := flags.String("target", ".", "review target path, ID, or URN; v4 decisions are per slide")
	state := flags.String("state", "", "approved, rejected, closed, or open")
	body := flags.String("body", "", "optional review note")
	reviewerKind := flags.String("reviewer-kind", "", "required reviewer persona: human or ai")
	reviewerName := flags.String("reviewer-name", "", "distinct AI reviewer name, for example Claude 1 (required for AI reviews)")
	agent := flags.String("agent", "", "AI agent kind, for example codex (required for AI reviews)")
	model := flags.String("model", "", "AI model name (required for AI reviews)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["review"])
	}
	if strings.TrimSpace(*reviewerKind) == "" {
		return fmt.Errorf("review requires --reviewer-kind human or ai")
	}
	reviewer := saga.ReviewerIdentity{Kind: *reviewerKind, Name: *reviewerName, Agent: *agent, Model: *model}
	if err := saga.ValidateReviewerIdentity(&reviewer); err != nil {
		return err
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}
	targetDir, resolvedTarget, err := resolveTarget(document, *target, true)
	if err != nil {
		return err
	}
	reviewTarget := targetDir
	if document.Manifest.Version == saga.SlideSagaVersion {
		if _, ok := saga.MutationIndexFromDocument(document).ReviewTargets[resolvedTarget]; !ok {
			return fmt.Errorf("v4 approval decisions must target a slide; use a thread to comment on an Item")
		}
		reviewTarget = resolvedTarget
	}
	if err := reviewstore.AddReview(document.Root, reviewTarget, *state, *body, reviewer); err != nil {
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

// Serve runs the loopback reviewer. It is reached as both "serve" and "open":
// open launches a managed background reviewer and browser, while serve stays
// attached by default. The flag set is named after the command the user typed
// so its help describes that public behavior.
func Serve(ctx context.Context, args []string, out io.Writer, openByDefault ...bool) error {
	opensBrowser := len(openByDefault) > 0 && openByDefault[0]
	detach := false
	name := "serve"
	if opensBrowser {
		name = "open"
		var err error
		args, detach, err = normalizeLegacyOpenDetach(args)
		if err != nil {
			return err
		}
	}
	if name == "serve" && len(args) > 0 && (args[0] == "status" || args[0] == "stop") {
		return manageDetachedServers(ctx, args[0], args[1:], out)
	}
	flags := commandFlags(name, commandUsage[name], out)
	addr := flags.String("addr", "127.0.0.1:7342", "loopback listen address; remote serving is disabled")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	openBrowser := flags.Bool("open", opensBrowser, "open the review in a browser")
	if !opensBrowser {
		flags.BoolVar(&detach, "detach", false, "run in the background and return the PID and active URL")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage[name])
	}
	if detach {
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

// normalizeLegacyOpenDetach keeps pre-0.0.9 `open --detach` calls working
// without carrying the implementation detail in the public open command. An
// explicit false retains the old foreground behavior; `serve --open` is the
// documented spelling for that mode.
func normalizeLegacyOpenDetach(args []string) ([]string, bool, error) {
	detach := true
	normalized := make([]string, 0, len(args))
	afterTerminator := false
	for _, arg := range args {
		if afterTerminator {
			normalized = append(normalized, arg)
			continue
		}
		if arg == "--" {
			afterTerminator = true
			normalized = append(normalized, arg)
			continue
		}
		if arg == "-detach" || arg == "--detach" {
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if hasValue && (name == "-detach" || name == "--detach") {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, false, fmt.Errorf("invalid value %q for -detach: %w", value, err)
			}
			detach = parsed
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized, detach, nil
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
			"slide_native_v4": map[string]any{
				"manifest": saga.FlatManifestName, "layout": "flat", "max_basename": saga.FlatMaxBasename, "max_absolute_path": saga.FlatMaxPath,
				"categories": map[string]string{"10-d": "deck", "20-s": "slide", "30-i": "item", "40-e": "evidence", "50-c": "claim", "60-v": "verification", "80-85": "review"},
				"content":    "one self-contained visual file sharing its slide manifest stem",
				"visual_forms": map[string]string{
					"system-context": "actors, external systems, boundaries, and changed interfaces", "architecture": "containment, dependencies, and responsibilities",
					"data-flow": "directed inputs, transformations, storage, and outputs", "sequence": "participants, time, calls, responses, and exceptional returns",
					"state-machine": "states, events, guards, and terminal states", "entity-relationship": "entities, keys, ownership, and cardinality",
					"logic-flow": "predicates, branches, joins, loops, and outcomes", "before-after": "matched axes and the meaningful delta",
					"failure-path": "trigger, propagation, containment, cleanup, recovery, and outcome", "evidence": "claims or risks connected to tests, measurements, or observations",
				},
				"editorial_goal": "establish the system model, then maximize reviewer information gain by surfacing consequential surprises",
				"surprise_contract": map[string]any{
					"sequence":             []string{"reasonable expectation", "actual behavior", "rationale", "consequence"},
					"preferred_expression": "an evidence-bearing callout Item attached to the responsible node, edge, state, or transition",
					"grounding":            "documented behavior, established repository patterns, historical design, or a plausible reviewer mental model; never manufactured novelty",
				},
				"composition_audits": []string{"silhouette", "relationship", "surprise", "contact-sheet"},
			},
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
	if dir, target, found := resolveTargetRecordPath(document, candidateAbs, allowFragment); found {
		return dir, target, nil
	}
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

// resolveTargetRecordPath recognizes the manifest filename printed by v4
// authoring commands. All v4 targets share the Saga root as their storage
// directory, so comparing Directory alone would make every slide and Item
// collide. The Path field remains unique and is also useful for legacy targets.
func resolveTargetRecordPath(document *saga.Saga, candidateAbs string, allowFragment bool) (string, string, bool) {
	var foundDir, foundTarget string
	var walk func(*saga.Section)
	walk = func(section *saga.Section) {
		if section.Path != "" {
			pathAbs, _ := filepath.Abs(filepath.Join(document.Root, filepath.FromSlash(section.Path)))
			if pathAbs == candidateAbs {
				foundDir = pathAbs
				if document.Manifest.Version == saga.SlideSagaVersion {
					foundDir = document.Root
				}
				foundTarget = section.Target
			}
		}
		if allowFragment {
			for _, fragment := range section.Fragments {
				pathAbs, _ := filepath.Abs(filepath.Join(document.Root, filepath.FromSlash(fragment.Path)))
				if fragment.Path != "" && pathAbs == candidateAbs {
					foundDir, foundTarget = fragment.Directory, fragment.Target
				}
				for index := range fragment.Landmarks {
					landmark := &fragment.Landmarks[index]
					pathAbs, _ := filepath.Abs(filepath.Join(document.Root, filepath.FromSlash(landmark.Path)))
					if landmark.Path != "" && pathAbs == candidateAbs {
						foundDir, foundTarget = landmark.Directory, landmark.Target
					}
				}
			}
		}
		for _, child := range section.Children {
			walk(child)
		}
	}
	walk(document.Section)
	return foundDir, foundTarget, foundTarget != ""
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
	_, fragmentTarget, err := resolveTarget(document, fragmentPath, true)
	if err != nil {
		return "", "", err
	}
	fragment := findFragmentByTarget(document.Section, fragmentTarget)
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

func findFragmentByTarget(section *saga.Section, target string) *saga.Fragment {
	var found *saga.Fragment
	var walk func(*saga.Section)
	walk = func(current *saga.Section) {
		for _, fragment := range current.Fragments {
			if fragment.Target == target {
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
		if strings.HasPrefix(entry.Name(), "___") || rel == "fragment.json" || rel == "slide.json" {
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

## Slide-native v4

For a new visual review deck, initialize with ` + "`change-saga init --mode slides`" + `.
This is an intentionally incompatible format, not a pagination option for an
existing report. Its spine is Saga → Deck → Slide → Item. Author with
` + "`add-deck`" + `, ` + "`add-slide`" + `, ` + "`set-slide-content`" + `, and ` + "`add-item`" + `.
Each slide is one 16:9 visual argument with one takeaway and no more than seven
semantic Items in a standard layout. Nodes, edges, regions, transitions,
examples, risks, metrics, statements, and overlaid callouts are all Items. A
callout may name another Item and may own its own exact diff evidence. Attach
every product diff atom to the narrowest Item; v4 refuses Saga-, deck-, and
slide-level coverage. Read it with the v2 ` + "`slide`" + ` and ` + "`slide-diffs`" + ` query
operations. Never migrate by editing a report's manifest version.

V4 storage is flat and deliberately opaque: ` + "`00-saga.json`" + ` plus compact
category-prefixed records (` + "`10-d`" + ` decks, ` + "`20-s`" + ` slides, ` + "`30-i`" + ` Items,
` + "`40-e`" + ` evidence, and ` + "`80`" + `–` + "`85`" + ` reviews). Titles and source paths belong
inside records, never filenames. Always use CLI commands and query targets;
never invent, rename, nest, glob, or infer meaning from v4 storage files. Slide
HTML must be self-contained.

Optimize for reviewer understanding and information gain, not exhaustive
retelling. Establish enough of the surrounding system for the reviewer to form
an accurate mental model, then foreground where that model may break:
counterintuitive behavior, hidden coupling, consequential constraints,
intentional deviations from repository conventions, rejected alternatives,
and tradeoffs whose costs land elsewhere. For each meaningful surprise, show
the reasonable reviewer expectation, actual behavior, rationale, and consequence. A
surprise is especially effective as a callout Item attached to the node, edge,
state, or transition that creates it; give the callout exact evidence when it
makes a code-backed claim. Do not manufacture novelty when the investigation
finds no material deviation.

### Storyboard visual questions before creating slides

Do not start by choosing a reusable SVG template. First inspect the change and
write a private storyboard. For every proposed slide, name:

- the specific reviewer question it answers, not merely its topic;
- its rhetorical intent and one-sentence takeaway;
- whether it establishes the system model or resolves a specific reviewer
  surprise, including expectation, actuality, rationale, and consequence;
- the relationship the picture must make visible;
- the visual form that truthfully encodes that relationship; and
- the meaningful nodes, edges, states, regions, or callouts that will become
  evidence-bearing Items.

Choose visual form from the explanation, not from styling convenience:

- a system-context diagram shows actors, external systems, trust or ownership
  boundaries, and the changed interface;
- an architecture/composition diagram shows containment, dependencies,
  responsibilities, and what moved or was introduced;
- a data-flow diagram shows direction, inputs, transformations, storage, and
  outputs;
- a sequence diagram shows participants, time ordering, calls, responses, and
  exceptional returns;
- a state machine shows states, labeled events, guards, and terminal states;
- an entity-relationship diagram shows entities, keys, ownership, and
  cardinality;
- a logic or decision flow shows predicates, branches, joins, loops, and
  outcomes;
- a before/after comparison uses matched axes and highlights the meaningful
  delta;
- a failure-path diagram traces trigger, propagation, containment, cleanup,
  recovery, and observable outcome; and
- an evidence view connects a concrete claim or risk to tests, measurements,
  or observable results.

` + "`intent`" + ` states the slide's job and ` + "`layout`" + ` states its canvas arrangement;
neither is a substitute for the correct visual form. A grid or row of labeled
cards is valid only when category membership or matched comparison is itself
the relationship being explained. Do not use cards as a universal container
for architecture, flow, lifecycle, data, or failure semantics. Boxes connected
only by reading order are an outline, not a diagram.

Before handoff, run three visual audits:

1. **Silhouette test:** mentally remove labels, prose, and color. The remaining
   topology should still communicate whether this is containment, flow,
   sequence, state, entity structure, branching, or comparison.
2. **Relationship test:** every relationship essential to the takeaway is
   visibly encoded with an edge, boundary, lane, nesting, cardinality, axis,
   or transition—not left to nearby prose.
3. **Surprise test:** after reading the deck, a reviewer can name the system
   model, the highest-consequence deviation from likely expectation, why it
   exists, and the tradeoff it creates.
4. **Contact-sheet test:** inspect all slides together. Reuse a visual grammar
   only when the underlying relationship is genuinely the same. If unrelated
   slides reduce to the same number and arrangement of cards, rewrite them.

Perform these audits before chasing complete diff coverage. Coverage is the
final omission check; it must not rationalize a generic visual after the fact.

## Choose the workflow before authoring

First determine whether the user is documenting an existing implementation or
starting a new body of work.

For an existing PR, branch, or focused changeset, decide whether the change is
large enough to benefit from a guided review. Size means review complexity—not
just line count—including multiple behaviors, risks, systems, or workstreams.
A small focused change may be better served by the repository's normal PR
process. For a large change with no existing saga, author the saga from the
completed implementation and exact diff as the review guide. Requirements,
prototypes, technical design, and a work plan remain optional historical
context; do not invent them after the fact merely to fill every surface.

For a new feature or exploration, begin a living saga early. A common
progression is:

1. prototype the UX and UI aesthetic;
2. draft sourced user stories and acceptance criteria;
3. develop a technical design that traces to those requirements; and
4. organize delivery into dependency-aware waves, parallel workspace lanes,
   and explicit convergence points.

This progression is not a waterfall. Prototypes and stories may be created in
either order and iterated together. Design can proceed while they mature, and
work-plan drafting can overlap design. Treat revisions as normal living
changes, preserving their history and refreshing stale downstream links.

Parallel authoring is a core property of the document, not just of the code
change. Partition ownership by stable stories, prototype packages, design
fragments, and work items so agents can fan out and merge their Saga changes as
well as their implementation. Before peer review, consolidate the lanes and
connect the delivered commits and exact diffs through the acceptance criteria,
design, and work plan that explain them.

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
   Apply the same completion rule to prose citations and visual nodes: a
   footnote marker and definition are not linked until the definition is an
   exact-text landmark with focused diffs. Repair every per-footnote validation
   warning before handoff. Requirements provenance from "citation add" records
   source context and does not replace implementation evidence.
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
   If a merged base refresh makes otherwise unchanged mappings stale, run
   "change-saga rebase-evidence --repo PATH --dry-run" and inspect the exact
   old/new base, product identity, atom count, selector count, and claim impact.
   Apply only when the product patch is unchanged; the command refuses real
   product changes and rolls immutable claims forward.
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
"--quiet" when no successful output is needed. "change-saga open" starts a
managed background reviewer; inspect or stop it with "change-saga serve
status" and "change-saga serve stop". Use "change-saga serve --open" only
when the reviewer should remain attached to the current terminal.

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
When recording a decision, always declare the reviewer persona. Use
"--reviewer-kind human" only for a decision the human made directly. For the
agent's own review, use "--reviewer-kind ai" together with an independent
"--reviewer-name", "--agent", and the exact "--model"; never turn an AI pass
into a human approval. Give simultaneous passes stable distinct names such as
"Claude 1" and "Claude 2" even when their model is identical. Multiple reviewers
may decide the same target, and one persona's later decision supersedes only
that same persona's prior decision.
`

const defaultSVGFragment = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 300" role="img" aria-label="Diagram placeholder">
  <rect width="800" height="300" rx="24" fill="#eef3ee"/><text x="400" y="150" text-anchor="middle" dominant-baseline="middle" font-family="system-ui" font-size="28" fill="#244235">Replace with a useful diagram</text>
</svg>
`

const specText = `Change Saga formats (experimental)

Slide-native v4 is an intentionally incompatible visual format. Its root
manifest is 00-saga.json and every persistent object is a regular file at the
Saga root. Compact prefixes group decks (10-d), slides (20-s), Items (30-i),
evidence (40-e), claims/verifications (50-c/60-v), and review history (80-85).
Fixed-width ranks and deterministic keys make ordinary filename sorting stable;
semantic IDs, titles, parentage, and durable target URNs remain in the records.
Basenames are at most 64 characters and the default portability budget is 240
characters for an absolute path. Each slide owns one self-contained SVG, image,
or HTML file sharing its manifest stem. Evidence may target only Items.

For authoring, ` + "`intent`" + ` names the reviewer job and ` + "`layout`" + ` names the canvas
arrangement; neither names the diagram's meaning. Storyboard the specific
question, relationship, and visual form before creating a slide. Use system
boundaries for context, containment and dependencies for architecture,
directed transformations for data flow, lanes and messages for sequence,
labeled transitions for state, keys and cardinality for entity relationships,
branches for logic, matched axes for before/after, and
trigger-to-recovery paths for failure behavior. A repeated row of cards is not
a neutral visual language. Audit silhouette, encoded relationships, and the
whole contact sheet before treating coverage as complete.

Optimize the deck for reviewer understanding and surprise reduction. Establish
the minimum system model, then foreground evidence-backed gaps between a
reasonable reviewer expectation and the actual behavior: hidden coupling,
counterintuitive outcomes, consequential constraints, tradeoffs, and deliberate
deviations from repository norms. Show expectation, actual behavior, rationale,
and consequence together. Prefer a callout Item attached to the responsible
visual element and give it exact evidence. Do not manufacture novelty. The
surprise audit fails when a reviewer cannot identify the system model, the
highest-consequence deviation, why it exists, and the tradeoff it creates.

Change Saga format v2

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

When a declared base advances but the exact product patch does not,
change-saga rebase-evidence proves the base-independent product identity before
rewriting only selector base identities. It refuses product changes. Affected
claims are replaced through v3 supersedes relations and verification carry-forward
is explicit rather than automatic.

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
A prose citation and a code-bearing visual node have the same completion rule:
each needs a stable landmark with focused diff evidence. A rendered footnote
without that association is incomplete. Requirements provenance citations are
separate and do not prove implementation.

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
Approval events declare a human or AI reviewer persona in addition to their
Git-derived author. AI personas name an independent review seat, their agent
kind, and model. The latest event is projected per author and persona,
preserving concurrent decisions by
other reviewers; legacy events without persona metadata remain unspecified.
Every decision is an independent file, so parallel review branches add records
instead of rewriting a shared reviewer list.

All-atoms-mapped is an omission invariant, not a correctness or explanation-
quality verdict. Use query mappings --sort scrutiny to inspect broad or thin
coverage records, and query claims/verifications to independently test author
assertions.

The authoritative specification is SPEC.md in the Change Saga repository.
`
