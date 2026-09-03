package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingapp"
	"github.com/twentyideas/changesaga/internal/reviewapp"
	"github.com/twentyideas/changesaga/internal/saga"
)

const querySchema = "change-saga.ai/v1"
const slideQuerySchema = "change-saga.ai/v2"

const (
	maxQueryPageSize     = 1000
	maxFragmentChunkSize = 1024 * 1024
)

// These transport-local requests deliberately contain no indexing or loading
// logic. A small adapter converts them to reviewapp requests once a session is
// opened; keeping this seam also makes the CLI contract independently testable.
type queryOpenOptions struct {
	SagaRoot    string
	SourceDir   string
	SummaryOnly bool
	Operation   string
	SlideMode   bool
}

type overviewQuery struct{}

type childrenQuery struct {
	Parent string
	Cursor string
	Limit  int
}

type fragmentQuery struct {
	Target string
	Offset int64
	Limit  int
}

type fragmentDiffQuery struct {
	Target string
	Cursor string
	Limit  int
}

type diffOwnerQuery struct {
	Diff   string
	Cursor string
	Limit  int
}

type reviewQuery struct {
	Target string
	Thread string
	State  string
	Cursor string
	Limit  int
}

type gapQuery struct {
	Kind   string
	Cursor string
	Limit  int
}

type mappingQuery struct {
	Target       string
	Sort         string
	MinimumScore int
	Cursor       string
	Limit        int
}

type claimQuery struct {
	Target string
	Status string
	Cursor string
	Limit  int
}

type verificationQuery struct {
	Claim  string
	Status string
	Cursor string
	Limit  int
}

// livingQuery is a transport-only request. The application package owns all
// graph composition, readiness policy, and cursor validation.
type livingQuery struct {
	Operation string
	Filters   livingapp.Filters
	Cursor    string
	Limit     int
}

type queryPage struct {
	Data any
	Page queryPageEnvelope
}

type querySession interface {
	Snapshot() string
	Overview(context.Context, overviewQuery) (any, error)
	Children(context.Context, childrenQuery) (queryPage, error)
	ReadFragment(context.Context, fragmentQuery) (any, error)
	FragmentDiffs(context.Context, fragmentDiffQuery) (queryPage, error)
	DiffOwners(context.Context, diffOwnerQuery) (queryPage, error)
	Reviews(context.Context, reviewQuery) (queryPage, error)
	Gaps(context.Context, gapQuery) (queryPage, error)
	Mappings(context.Context, mappingQuery) (queryPage, error)
	Claims(context.Context, claimQuery) (queryPage, error)
	Verifications(context.Context, verificationQuery) (queryPage, error)
	Living(context.Context, livingQuery) (queryPage, error)
}

type querySessionOpener func(context.Context, queryOpenOptions) (querySession, error)

// Tests replace this constructor with a deterministic in-memory session.
var openQuerySession querySessionOpener = openReviewAppSession

type queryError struct {
	Code      string
	Message   string
	Retryable bool
	Details   any
}

func (e *queryError) Error() string { return e.Message }

type queryErrorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Details   any    `json:"details,omitempty"`
}

type queryPageEnvelope struct {
	Total      int     `json:"total"`
	Returned   int     `json:"returned"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

type queryEnvelope struct {
	Schema   string              `json:"schema"`
	OK       bool                `json:"ok"`
	Snapshot string              `json:"snapshot,omitempty"`
	Data     any                 `json:"data,omitempty"`
	Page     *queryPageEnvelope  `json:"page,omitempty"`
	Error    *queryErrorEnvelope `json:"error,omitempty"`
}

type queryHelp struct {
	Usage      string                      `json:"usage"`
	Operations []string                    `json:"operations,omitempty"`
	Operation  string                      `json:"operation,omitempty"`
	Purpose    string                      `json:"purpose,omitempty"`
	DataPaths  []string                    `json:"data_paths,omitempty"`
	Pagination *queryPaginationDescription `json:"pagination,omitempty"`
}

type queryPaginationDescription struct {
	Kind           string `json:"kind"`
	CountedPath    string `json:"counted_path,omitempty"`
	TotalPath      string `json:"total_path,omitempty"`
	ReturnedPath   string `json:"returned_path,omitempty"`
	HasMorePath    string `json:"has_more_path,omitempty"`
	NextCursorPath string `json:"next_cursor_path,omitempty"`
	NextOffsetPath string `json:"next_offset_path,omitempty"`
}

type querySchemaDescription struct {
	Operation  string                     `json:"operation"`
	Purpose    string                     `json:"purpose"`
	Usage      string                     `json:"usage"`
	DataPaths  []string                   `json:"data_paths"`
	Pagination queryPaginationDescription `json:"pagination"`
}

var queryOperations = []string{
	"schema",
	"overview",
	"children",
	"fragment",
	"fragment-diffs",
	"slide",
	"slide-diffs",
	"diff-owners",
	"reviews",
	"gaps",
	"mappings",
	"claims",
	"verifications",
	"requirements",
	"requirement-history",
	"citations",
	"relations",
	"waves",
	"work-items",
	"work-events",
	"work-conflicts",
	"traceability",
	"readiness",
}

// queryPurpose says what each operation answers. It is keyed by the same
// operation names the dispatcher uses so documentation generated from it — the
// install-skill prompt in particular — cannot describe an operation the CLI
// does not have, or omit one it does.
var queryPurpose = map[string]string{
	"schema":              "the response paths and pagination contract for a query operation; no saga is required",
	"overview":            "saga identity, source comparison, coverage summary, and the top of the hierarchy",
	"children":            "one level of children under a target; a fragment's children are its landmarks",
	"fragment":            "bounded fragment content by byte range, without reading files directly",
	"fragment-diffs":      "the diff atoms a saga, chapter, section, fragment, or landmark owns",
	"slide":               "bounded visual slide content and its ordered semantic Items",
	"slide-diffs":         "the exact diff atoms owned by a slide Item",
	"diff-owners":         "the narrative targets that own a given diff atom, event, or file",
	"reviews":             "the normalized review overlay: threads, messages, events, and approvals",
	"gaps":                "uncovered atoms, stale selectors, and overlapping coverage",
	"mappings":            "coverage records ranked by breadth and justification signals so scrutiny starts at the weakest mappings",
	"claims":              "falsifiable author assertions, exact evidence, current mapping state, and latest verification result",
	"verifications":       "append-only verification history for author claims",
	"requirements":        "current requirement definitions and lifecycle heads without fabricating winners for conflicts",
	"requirement-history": "append-only revision and lifecycle history in deterministic graph order",
	"citations":           "immutable requirement provenance records",
	"relations":           "typed current, stale, and superseded living-Saga relations",
	"waves":               "ordered work-plan coordination cohorts and derived item counts",
	"work-items":          "current work-item definitions, progress, explicit dependency blockers, workspaces, and merge evidence",
	"work-events":         "normalized append-only progress, workspace, merge, and contract events",
	"work-conflicts":      "deterministically identified work-plan conflicts and competing heads",
	"traceability":        "current requirement-to-design-to-work-to-evidence paths and transitive blockers",
	"readiness":           "independent requirement, plan, and delivery coverage axes; only immutable delivery evidence gates peer-review readiness",
}

var queryUsage = map[string]string{
	"":                    "change-saga query <operation> --saga PATH [--repo PATH] [operation flags]",
	"schema":              "change-saga query schema <operation>",
	"overview":            "change-saga query overview --saga PATH [--repo PATH]",
	"children":            "change-saga query children --saga PATH --parent TARGET [--cursor TOKEN] [--limit N] [--repo PATH]",
	"fragment":            "change-saga query fragment --saga PATH --target FRAGMENT [--offset N] [--limit N] [--repo PATH]",
	"fragment-diffs":      "change-saga query fragment-diffs --saga PATH --target TARGET [--cursor TOKEN] [--limit N] [--repo PATH]",
	"slide":               "change-saga query slide --saga PATH --target SLIDE [--offset N] [--limit N] [--repo PATH]",
	"slide-diffs":         "change-saga query slide-diffs --saga PATH --target ITEM [--cursor TOKEN] [--limit N] [--repo PATH]",
	"diff-owners":         "change-saga query diff-owners --saga PATH --diff URI [--cursor TOKEN] [--limit N] [--repo PATH]",
	"reviews":             "change-saga query reviews --saga PATH [--target TARGET] [--thread ID] [--state STATE] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"gaps":                "change-saga query gaps --saga PATH [--kind uncovered|stale|overlap] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"mappings":            "change-saga query mappings --saga PATH [--target TARGET] [--sort scrutiny|target|path] [--minimum-score N] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"claims":              "change-saga query claims --saga PATH [--target TARGET] [--status unverified|verified|failed|inconclusive] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"verifications":       "change-saga query verifications --saga PATH [--claim ID] [--status unverified|verified|failed|inconclusive] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"requirements":        "change-saga query requirements --saga PATH [--requirement ID|URN] [--state STATE] [--cursor TOKEN] [--limit N]",
	"requirement-history": "change-saga query requirement-history --saga PATH --requirement ID|URN [--cursor TOKEN] [--limit N]",
	"citations":           "change-saga query citations --saga PATH [--citation ID|URN] [--requirement ID|URN] [--cursor TOKEN] [--limit N]",
	"relations":           "change-saga query relations --saga PATH [--relation ID|URN] [--type TYPE] [--from URN] [--to URN] [--state STATE] [--cursor TOKEN] [--limit N]",
	"waves":               "change-saga query waves --saga PATH [--wave ID|URN] [--cursor TOKEN] [--limit N]",
	"work-items":          "change-saga query work-items --saga PATH [--item ID|URN] [--wave ID|URN] [--status STATE] [--cursor TOKEN] [--limit N]",
	"work-events":         "change-saga query work-events --saga PATH [--item ID|URN] [--kind KIND] [--cursor TOKEN] [--limit N]",
	"work-conflicts":      "change-saga query work-conflicts --saga PATH [--item ID|URN] [--wave ID|URN] [--kind KIND] [--cursor TOKEN] [--limit N]",
	"traceability":        "change-saga query traceability --saga PATH [--requirement ID|URN] [--criterion ID|URN] [--cursor TOKEN] [--limit N]",
	"readiness":           "change-saga query readiness --saga PATH [--requirement ID|URN] [--status ready|blocked] [--cursor TOKEN] [--limit N]",
}

// Query executes one read-only application query and writes exactly one JSON
// value for every ordinary outcome, including help and invalid input. The
// returned StatusError only communicates the already-rendered exit status to
// the process entrypoint; callers must not print it.
func Query(ctx context.Context, args []string, out io.Writer) error {
	return queryWithOpener(ctx, args, out, openQuerySession)
}

func queryWithOpener(ctx context.Context, args []string, out io.Writer, open querySessionOpener) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		return writeQuerySuccess(out, "", queryHelp{Usage: queryUsage[""], Operations: queryOperations}, nil)
	}

	operation := args[0]
	if _, ok := queryUsage[operation]; !ok {
		return writeQueryFailure(out, &queryError{
			Code:    "invalid_argument",
			Message: "unknown query operation",
			Details: map[string]any{"allowed": queryOperations},
		})
	}
	if operation == "schema" {
		return writeQuerySchema(args[1:], out)
	}

	request, options, help, err := parseQuery(operation, args[1:])
	if help {
		if operation == "slide" || operation == "slide-diffs" {
			return writeQuerySuccessSchema(out, slideQuerySchema, "", queryHelpFor(operation), nil)
		}
		return writeQuerySuccess(out, "", queryHelpFor(operation), nil)
	}
	if err != nil {
		return writeQueryOperationFailure(out, operation, &queryError{Code: "invalid_argument", Message: err.Error()})
	}
	options.SummaryOnly = operation == "overview" || operation == "children"
	options.Operation = operation
	manifest, manifestErr := saga.ReadManifest(options.SagaRoot)
	if manifestErr == nil {
		options.SlideMode = manifest.Version == saga.SlideSagaVersion
	}
	if operation == "slide" || operation == "slide-diffs" || operation == "fragment" || operation == "fragment-diffs" {
		if manifestErr != nil && (operation == "slide" || operation == "slide-diffs") {
			return writeQueryOperationFailure(out, operation, normalizeQueryError(manifestErr))
		}
		if manifestErr == nil && (operation == "slide" || operation == "slide-diffs") && manifest.Version != saga.SlideSagaVersion {
			return writeQueryOperationFailure(out, operation, &queryError{Code: "invalid_argument", Message: "slide queries require a v4 slide-native Saga"})
		}
		if manifestErr == nil && (operation == "fragment" || operation == "fragment-diffs") && manifest.Version == saga.SlideSagaVersion {
			return writeQueryOperationFailure(out, operation, &queryError{Code: "invalid_argument", Message: "v4 does not expose slides as fragments; use slide or slide-diffs"})
		}
	}

	session, err := open(ctx, options)
	if err != nil {
		return writeQueryOperationFailure(out, operation, normalizeQueryError(err))
	}

	var result any
	var responsePage *queryPageEnvelope
	switch request := request.(type) {
	case overviewQuery:
		result, err = session.Overview(ctx, request)
	case childrenQuery:
		var page queryPage
		page, err = session.Children(ctx, request)
		result, responsePage = page.Data, &page.Page
	case fragmentQuery:
		result, err = session.ReadFragment(ctx, request)
	case fragmentDiffQuery:
		var page queryPage
		page, err = session.FragmentDiffs(ctx, request)
		result, responsePage = page.Data, &page.Page
	case diffOwnerQuery:
		var page queryPage
		page, err = session.DiffOwners(ctx, request)
		result, responsePage = page.Data, &page.Page
	case reviewQuery:
		var page queryPage
		page, err = session.Reviews(ctx, request)
		result, responsePage = page.Data, &page.Page
	case gapQuery:
		var page queryPage
		page, err = session.Gaps(ctx, request)
		result, responsePage = page.Data, &page.Page
	case mappingQuery:
		var page queryPage
		page, err = session.Mappings(ctx, request)
		result, responsePage = page.Data, &page.Page
	case claimQuery:
		var page queryPage
		page, err = session.Claims(ctx, request)
		result, responsePage = page.Data, &page.Page
	case verificationQuery:
		var page queryPage
		page, err = session.Verifications(ctx, request)
		result, responsePage = page.Data, &page.Page
	case livingQuery:
		var page queryPage
		page, err = session.Living(ctx, request)
		result, responsePage = page.Data, &page.Page
	default:
		err = errors.New("unsupported query request")
	}
	if err != nil {
		return writeQueryOperationFailure(out, operation, normalizeQueryError(err))
	}
	if options.SlideMode || operation == "slide" || operation == "slide-diffs" {
		return writeQuerySuccessSchema(out, slideQuerySchema, session.Snapshot(), result, responsePage)
	}
	return writeQuerySuccess(out, session.Snapshot(), result, responsePage)
}

func writeQuerySchema(args []string, out io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && isHelpArg(args[0])) {
		return writeQuerySuccess(out, "", queryHelp{Usage: queryUsage["schema"], Operations: queryDataOperations()}, nil)
	}
	if len(args) != 1 {
		return writeQueryFailure(out, &queryError{Code: "invalid_argument", Message: "schema requires exactly one query operation"})
	}
	operation := args[0]
	if operation == "schema" || queryUsage[operation] == "" {
		return writeQueryFailure(out, &queryError{Code: "invalid_argument", Message: "unknown schema operation", Details: map[string]any{"allowed": queryDataOperations()}})
	}
	description := querySchemaFor(operation)
	if operation == "slide" || operation == "slide-diffs" {
		return writeQuerySuccessSchema(out, slideQuerySchema, "", description, nil)
	}
	return writeQuerySuccess(out, "", description, nil)
}

func queryDataOperations() []string {
	return append([]string(nil), queryOperations[1:]...)
}

func queryHelpFor(operation string) queryHelp {
	description := querySchemaFor(operation)
	return queryHelp{
		Usage: queryUsage[operation], Operation: operation, Purpose: description.Purpose,
		DataPaths: description.DataPaths, Pagination: &description.Pagination,
	}
}

func querySchemaFor(operation string) querySchemaDescription {
	paths := map[string][]string{
		"overview":            {"data.saga", "data.source", "data.root", "data.overview_fragments", "data.chapters", "data.coverage"},
		"children":            {"data.children"},
		"fragment":            {"data.target", "data.content.data", "data.content.next_offset", "data.assets", "data.landmarks"},
		"fragment-diffs":      {"data.selectors", "data.atoms", "data.stale"},
		"slide":               {"data.target", "data.intent", "data.layout", "data.takeaway", "data.content.data", "data.assets", "data.items", "data.reading_order"},
		"slide-diffs":         {"data.selectors", "data.atoms", "data.stale"},
		"diff-owners":         {"data.atoms"},
		"reviews":             {"data.items"},
		"gaps":                {"data.gaps"},
		"mappings":            {"data.mappings"},
		"claims":              {"data.claims"},
		"verifications":       {"data.verifications"},
		"requirements":        {"data.requirements"},
		"requirement-history": {"data.events"},
		"citations":           {"data.citations"},
		"relations":           {"data.relations"},
		"waves":               {"data.waves"},
		"work-items":          {"data.items"},
		"work-events":         {"data.events"},
		"work-conflicts":      {"data.conflicts"},
		"traceability":        {"data.criteria"},
		"readiness":           {"data.summary", "data.requirements"},
	}
	countedPaths := map[string]string{
		"children":            "data.children",
		"fragment-diffs":      "data.selectors",
		"slide-diffs":         "data.selectors",
		"diff-owners":         "data.atoms",
		"reviews":             "data.items",
		"gaps":                "data.gaps",
		"mappings":            "data.mappings",
		"claims":              "data.claims",
		"verifications":       "data.verifications",
		"requirements":        "data.requirements",
		"requirement-history": "data.events",
		"citations":           "data.citations",
		"relations":           "data.relations",
		"waves":               "data.waves",
		"work-items":          "data.items",
		"work-events":         "data.events",
		"work-conflicts":      "data.conflicts",
		"traceability":        "data.criteria",
		"readiness":           "data.requirements",
	}
	pagination := queryPaginationDescription{Kind: "none"}
	if operation == "fragment" || operation == "slide" {
		pagination = queryPaginationDescription{Kind: "byte-offset", NextOffsetPath: "data.content.next_offset"}
	} else if operation != "overview" {
		pagination = queryPaginationDescription{
			Kind: "cursor", CountedPath: countedPaths[operation], TotalPath: "page.total", ReturnedPath: "page.returned",
			HasMorePath: "page.has_more", NextCursorPath: "page.next_cursor",
		}
	}
	return querySchemaDescription{
		Operation: operation, Purpose: queryPurpose[operation], Usage: queryUsage[operation],
		DataPaths: append([]string(nil), paths[operation]...), Pagination: pagination,
	}
}

func parseQuery(operation string, args []string) (any, queryOpenOptions, bool, error) {
	flags := flag.NewFlagSet("query "+operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sagaRoot := flags.String("saga", "", "saga root")
	sourceDir := flags.String("repo", "", "source repository checkout")

	var parent, target, diff, cursor, thread, state, kind, sortOrder, claim string
	var requirement, citation, relation, from, to, wave, item, criterion string
	var offset int64
	var limit optionalInt
	var minimumScore optionalInt
	switch operation {
	case "children":
		flags.StringVar(&parent, "parent", "", "parent target URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "fragment", "slide":
		flags.StringVar(&target, "target", "", "fragment target URN")
		flags.Int64Var(&offset, "offset", 0, "content byte offset")
		flags.Var(&limit, "limit", "content byte limit")
	case "fragment-diffs", "slide-diffs":
		flags.StringVar(&target, "target", "", "saga target URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "diff-owners":
		flags.StringVar(&diff, "diff", "", "diff atom, event, or file URI")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "reviews":
		flags.StringVar(&target, "target", "", "target URN")
		flags.StringVar(&thread, "thread", "", "thread ID")
		flags.StringVar(&state, "state", "", "review or thread state")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "gaps":
		flags.StringVar(&kind, "kind", "", "uncovered, stale, or overlap")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "mappings":
		flags.StringVar(&target, "target", "", "optional narrative target URN")
		flags.StringVar(&sortOrder, "sort", "scrutiny", "scrutiny, target, or path")
		flags.Var(&minimumScore, "minimum-score", "minimum scrutiny score from 0 to 100")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "claims":
		flags.StringVar(&target, "target", "", "optional narrative target URN")
		flags.StringVar(&state, "status", "", "unverified, verified, failed, or inconclusive")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "verifications":
		flags.StringVar(&claim, "claim", "", "optional claim id")
		flags.StringVar(&state, "status", "", "unverified, verified, failed, or inconclusive")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "requirements":
		flags.StringVar(&requirement, "requirement", "", "optional requirement ID or URN")
		flags.StringVar(&state, "state", "", "optional lifecycle state")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "requirement-history":
		flags.StringVar(&requirement, "requirement", "", "requirement ID or URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "citations":
		flags.StringVar(&citation, "citation", "", "optional citation ID or URN")
		flags.StringVar(&requirement, "requirement", "", "optional requirement ID or URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "relations":
		flags.StringVar(&relation, "relation", "", "optional relation ID or URN")
		flags.StringVar(&kind, "type", "", "optional relation type")
		flags.StringVar(&from, "from", "", "optional source endpoint URN")
		flags.StringVar(&to, "to", "", "optional target endpoint URN")
		flags.StringVar(&state, "state", "", "active, superseded, stale, or current")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "waves":
		flags.StringVar(&wave, "wave", "", "optional wave ID or URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "work-items":
		flags.StringVar(&item, "item", "", "optional work-item ID or URN")
		flags.StringVar(&wave, "wave", "", "optional wave ID or URN")
		flags.StringVar(&state, "status", "", "optional progress state")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "work-events":
		flags.StringVar(&item, "item", "", "optional work-item ID or URN")
		flags.StringVar(&kind, "kind", "", "progress, workspace, merge, or contract")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "work-conflicts":
		flags.StringVar(&item, "item", "", "optional work-item ID or URN")
		flags.StringVar(&wave, "wave", "", "optional wave ID or URN")
		flags.StringVar(&kind, "kind", "", "optional conflict kind")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "traceability":
		flags.StringVar(&requirement, "requirement", "", "optional requirement ID or URN")
		flags.StringVar(&criterion, "criterion", "", "optional criterion ID or URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "readiness":
		flags.StringVar(&requirement, "requirement", "", "optional requirement ID or URN")
		flags.StringVar(&state, "status", "", "ready or blocked")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, queryOpenOptions{}, true, nil
		}
		return nil, queryOpenOptions{}, false, fmt.Errorf("invalid flags for %s: %w", operation, err)
	}
	if flags.NArg() != 0 {
		return nil, queryOpenOptions{}, false, fmt.Errorf("%s accepts no positional arguments", operation)
	}
	cursorSet := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "cursor" {
			cursorSet = true
		}
	})
	if cursorSet && cursor == "" {
		return nil, queryOpenOptions{}, false, errors.New("--cursor cannot be empty")
	}
	if strings.TrimSpace(*sagaRoot) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--saga is required")
	}
	maxLimit := maxQueryPageSize
	if operation == "fragment" || operation == "slide" {
		maxLimit = maxFragmentChunkSize
	}
	if limit.set && (limit.value < 1 || limit.value > maxLimit) {
		return nil, queryOpenOptions{}, false, fmt.Errorf("--limit must be between 1 and %d", maxLimit)
	}
	if offset < 0 {
		return nil, queryOpenOptions{}, false, errors.New("--offset cannot be negative")
	}
	if minimumScore.set && (minimumScore.value < 0 || minimumScore.value > 100) {
		return nil, queryOpenOptions{}, false, errors.New("--minimum-score must be between 0 and 100")
	}
	if operation == "children" && strings.TrimSpace(parent) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--parent is required")
	}
	if (operation == "fragment" || operation == "fragment-diffs" || operation == "slide" || operation == "slide-diffs") && strings.TrimSpace(target) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--target is required")
	}
	if operation == "diff-owners" && strings.TrimSpace(diff) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--diff is required")
	}
	if operation == "requirement-history" && strings.TrimSpace(requirement) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--requirement is required")
	}
	if operation == "gaps" && kind != "" && kind != "uncovered" && kind != "stale" && kind != "overlap" {
		return nil, queryOpenOptions{}, false, errors.New("--kind must be uncovered, stale, or overlap")
	}
	if operation == "mappings" && sortOrder != "scrutiny" && sortOrder != "target" && sortOrder != "path" {
		return nil, queryOpenOptions{}, false, errors.New("--sort must be scrutiny, target, or path")
	}
	if (operation == "claims" || operation == "verifications") && state != "" && state != "unverified" && state != "verified" && state != "failed" && state != "inconclusive" {
		return nil, queryOpenOptions{}, false, errors.New("--status must be unverified, verified, failed, or inconclusive")
	}
	if operation == "requirements" && state != "" && state != "proposed" && state != "accepted" && state != "deferred" && state != "rejected" && state != "retired" && state != "conflicted" {
		return nil, queryOpenOptions{}, false, errors.New("--state must be proposed, accepted, deferred, rejected, retired, or conflicted")
	}
	if operation == "relations" && state != "" && state != "active" && state != "superseded" && state != "stale" && state != "current" {
		return nil, queryOpenOptions{}, false, errors.New("--state must be active, superseded, stale, or current")
	}
	if operation == "work-items" && state != "" && state != "planned" && state != "ready" && state != "in_progress" && state != "blocked" && state != "done" && state != "cancelled" && state != "conflicted" {
		return nil, queryOpenOptions{}, false, errors.New("--status is not a valid progress state")
	}
	if operation == "work-events" && kind != "" && kind != "progress" && kind != "workspace" && kind != "merge" && kind != "contract" {
		return nil, queryOpenOptions{}, false, errors.New("--kind must be progress, workspace, merge, or contract")
	}
	if operation == "readiness" && state != "" && state != "ready" && state != "blocked" {
		return nil, queryOpenOptions{}, false, errors.New("--status must be ready or blocked")
	}

	options := queryOpenOptions{SagaRoot: *sagaRoot, SourceDir: *sourceDir}
	switch operation {
	case "overview":
		return overviewQuery{}, options, false, nil
	case "children":
		return childrenQuery{Parent: parent, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "fragment", "slide":
		return fragmentQuery{Target: target, Offset: offset, Limit: limit.value}, options, false, nil
	case "fragment-diffs", "slide-diffs":
		return fragmentDiffQuery{Target: target, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "diff-owners":
		return diffOwnerQuery{Diff: diff, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "reviews":
		return reviewQuery{Target: target, Thread: thread, State: state, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "gaps":
		return gapQuery{Kind: kind, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "mappings":
		return mappingQuery{Target: target, Sort: sortOrder, MinimumScore: minimumScore.value, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "claims":
		return claimQuery{Target: target, Status: state, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "verifications":
		return verificationQuery{Claim: claim, Status: state, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "requirements", "requirement-history", "citations", "relations", "waves", "work-items", "work-events", "work-conflicts", "traceability", "readiness":
		filters := livingapp.Filters{
			Requirement: requirement, Kind: firstNonempty(kind, criterion), Citation: citation,
			Relation: relation, From: from, To: to, Wave: wave, Item: item,
		}
		if operation == "requirements" || operation == "relations" {
			filters.State = state
		} else if operation == "work-items" || operation == "readiness" {
			filters.Status = state
		}
		return livingQuery{Operation: operation, Filters: filters, Cursor: cursor, Limit: limit.value}, options, false, nil
	default:
		return nil, queryOpenOptions{}, false, fmt.Errorf("unknown query operation %q", operation)
	}
}

type optionalInt struct {
	value int
	set   bool
}

func (v *optionalInt) String() string { return strconv.Itoa(v.value) }

func (v *optionalInt) Set(raw string) error {
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return errors.New("must be an integer")
	}
	v.value, v.set = parsed, true
	return nil
}

func isHelpArg(arg string) bool { return arg == "help" || arg == "-h" || arg == "--help" }

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeQuerySuccess(out io.Writer, snapshot string, data any, page *queryPageEnvelope) error {
	return writeQuerySuccessSchema(out, querySchema, snapshot, data, page)
}

func writeQuerySuccessSchema(out io.Writer, schema string, snapshot string, data any, page *queryPageEnvelope) error {
	if page == nil {
		page = &queryPageEnvelope{Total: 1, Returned: 1}
	}
	return encodeQueryEnvelope(out, queryEnvelope{
		Schema:   schema,
		OK:       true,
		Snapshot: snapshot,
		Data:     data,
		Page:     page,
	})
}

func writeQueryFailure(out io.Writer, queryErr *queryError) error {
	return writeQueryFailureSchema(out, querySchema, queryErr)
}

func writeQueryOperationFailure(out io.Writer, operation string, queryErr *queryError) error {
	schema := querySchema
	if operation == "slide" || operation == "slide-diffs" {
		schema = slideQuerySchema
	}
	return writeQueryFailureSchema(out, schema, queryErr)
}

func writeQueryFailureSchema(out io.Writer, schema string, queryErr *queryError) error {
	if err := encodeQueryEnvelope(out, queryEnvelope{
		Schema: schema,
		OK:     false,
		Error: &queryErrorEnvelope{
			Code:      queryErr.Code,
			Message:   queryErr.Message,
			Retryable: queryErr.Retryable,
			Details:   queryErr.Details,
		},
	}); err != nil {
		return err
	}
	return &StatusError{Code: queryExitCode(queryErr.Code)}
}

func encodeQueryEnvelope(out io.Writer, envelope queryEnvelope) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}

func normalizeQueryError(err error) *queryError {
	var domainErr *queryError
	if errors.As(err, &domainErr) {
		if _, ok := queryExitCodes[domainErr.Code]; ok {
			return domainErr
		}
	}
	var applicationErr *reviewapp.Error
	if errors.As(err, &applicationErr) {
		code := string(applicationErr.Code)
		if _, ok := queryExitCodes[code]; ok {
			return &queryError{Code: code, Message: applicationErr.Message, Retryable: applicationErr.Retryable, Details: applicationErr.Details}
		}
	}
	var livingErr *livingapp.Error
	if errors.As(err, &livingErr) {
		code := string(livingErr.Code)
		if _, ok := queryExitCodes[code]; ok {
			return &queryError{Code: code, Message: livingErr.Message, Retryable: livingErr.Retryable, Details: livingErr.Details}
		}
	}
	return &queryError{Code: "internal", Message: "an unexpected error occurred"}
}

var queryExitCodes = map[string]int{
	"invalid_argument":   2,
	"invalid_saga":       3,
	"stale_snapshot":     4,
	"conflict":           4,
	"not_found":          5,
	"unsafe_path":        6,
	"unsupported_media":  6,
	"too_large":          6,
	"source_unavailable": 7,
	"internal":           1,
}

func queryExitCode(code string) int {
	if exit, ok := queryExitCodes[code]; ok {
		return exit
	}
	return 1
}
