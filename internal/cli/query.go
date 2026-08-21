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

	"github.com/twentyideas/changesaga/internal/reviewapp"
)

const querySchema = "change-saga.ai/v1"

const (
	maxQueryPageSize     = 1000
	maxFragmentChunkSize = 1024 * 1024
)

// These transport-local requests deliberately contain no indexing or loading
// logic. A small adapter converts them to reviewapp requests once a session is
// opened; keeping this seam also makes the CLI contract independently testable.
type queryOpenOptions struct {
	SagaRoot  string
	SourceDir string
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
	"diff-owners",
	"reviews",
	"gaps",
	"mappings",
	"claims",
	"verifications",
}

// queryPurpose says what each operation answers. It is keyed by the same
// operation names the dispatcher uses so documentation generated from it — the
// install-skill prompt in particular — cannot describe an operation the CLI
// does not have, or omit one it does.
var queryPurpose = map[string]string{
	"schema":         "the response paths and pagination contract for a query operation; no saga is required",
	"overview":       "saga identity, source comparison, coverage summary, and the top of the hierarchy",
	"children":       "one level of children under a target; a fragment's children are its landmarks",
	"fragment":       "bounded fragment content by byte range, without reading files directly",
	"fragment-diffs": "the diff atoms a saga, chapter, section, fragment, or landmark owns",
	"diff-owners":    "the narrative targets that own a given diff atom, event, or file",
	"reviews":        "the normalized review overlay: threads, messages, events, and approvals",
	"gaps":           "uncovered atoms, stale selectors, and overlapping coverage",
	"mappings":       "coverage records ranked by breadth and justification signals so scrutiny starts at the weakest mappings",
	"claims":         "falsifiable author assertions, exact evidence, current mapping state, and latest verification result",
	"verifications":  "append-only verification history for author claims",
}

var queryUsage = map[string]string{
	"":               "change-saga query <operation> --saga PATH [--repo PATH] [operation flags]",
	"schema":         "change-saga query schema <operation>",
	"overview":       "change-saga query overview --saga PATH [--repo PATH]",
	"children":       "change-saga query children --saga PATH --parent TARGET [--cursor TOKEN] [--limit N] [--repo PATH]",
	"fragment":       "change-saga query fragment --saga PATH --target FRAGMENT [--offset N] [--limit N] [--repo PATH]",
	"fragment-diffs": "change-saga query fragment-diffs --saga PATH --target TARGET [--cursor TOKEN] [--limit N] [--repo PATH]",
	"diff-owners":    "change-saga query diff-owners --saga PATH --diff URI [--cursor TOKEN] [--limit N] [--repo PATH]",
	"reviews":        "change-saga query reviews --saga PATH [--target TARGET] [--thread ID] [--state STATE] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"gaps":           "change-saga query gaps --saga PATH [--kind uncovered|stale|overlap] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"mappings":       "change-saga query mappings --saga PATH [--target TARGET] [--sort scrutiny|target|path] [--minimum-score N] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"claims":         "change-saga query claims --saga PATH [--target TARGET] [--status unverified|verified|failed|inconclusive] [--cursor TOKEN] [--limit N] [--repo PATH]",
	"verifications":  "change-saga query verifications --saga PATH [--claim ID] [--status unverified|verified|failed|inconclusive] [--cursor TOKEN] [--limit N] [--repo PATH]",
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
		return writeQuerySuccess(out, "", queryHelpFor(operation), nil)
	}
	if err != nil {
		return writeQueryFailure(out, &queryError{Code: "invalid_argument", Message: err.Error()})
	}

	session, err := open(ctx, options)
	if err != nil {
		return writeQueryFailure(out, normalizeQueryError(err))
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
	default:
		err = errors.New("unsupported query request")
	}
	if err != nil {
		return writeQueryFailure(out, normalizeQueryError(err))
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
		"overview":       {"data.saga", "data.source", "data.root", "data.overview_fragments", "data.chapters", "data.coverage"},
		"children":       {"data.children"},
		"fragment":       {"data.target", "data.content.data", "data.content.next_offset", "data.assets", "data.landmarks"},
		"fragment-diffs": {"data.selectors", "data.atoms", "data.stale"},
		"diff-owners":    {"data.atoms"},
		"reviews":        {"data.items"},
		"gaps":           {"data.gaps"},
		"mappings":       {"data.mappings"},
		"claims":         {"data.claims"},
		"verifications":  {"data.verifications"},
	}
	pagination := queryPaginationDescription{Kind: "none"}
	if operation == "fragment" {
		pagination = queryPaginationDescription{Kind: "byte-offset", NextOffsetPath: "data.content.next_offset"}
	} else if operation != "overview" {
		pagination = queryPaginationDescription{
			Kind: "cursor", TotalPath: "page.total", ReturnedPath: "page.returned",
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
	var offset int64
	var limit optionalInt
	var minimumScore optionalInt
	switch operation {
	case "children":
		flags.StringVar(&parent, "parent", "", "parent target URN")
		flags.StringVar(&cursor, "cursor", "", "pagination cursor")
		flags.Var(&limit, "limit", "page size")
	case "fragment":
		flags.StringVar(&target, "target", "", "fragment target URN")
		flags.Int64Var(&offset, "offset", 0, "content byte offset")
		flags.Var(&limit, "limit", "content byte limit")
	case "fragment-diffs":
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
	if operation == "fragment" {
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
	if (operation == "fragment" || operation == "fragment-diffs") && strings.TrimSpace(target) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--target is required")
	}
	if operation == "diff-owners" && strings.TrimSpace(diff) == "" {
		return nil, queryOpenOptions{}, false, errors.New("--diff is required")
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

	options := queryOpenOptions{SagaRoot: *sagaRoot, SourceDir: *sourceDir}
	switch operation {
	case "overview":
		return overviewQuery{}, options, false, nil
	case "children":
		return childrenQuery{Parent: parent, Cursor: cursor, Limit: limit.value}, options, false, nil
	case "fragment":
		return fragmentQuery{Target: target, Offset: offset, Limit: limit.value}, options, false, nil
	case "fragment-diffs":
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

func writeQuerySuccess(out io.Writer, snapshot string, data any, page *queryPageEnvelope) error {
	if page == nil {
		page = &queryPageEnvelope{Total: 1, Returned: 1}
	}
	return encodeQueryEnvelope(out, queryEnvelope{
		Schema:   querySchema,
		OK:       true,
		Snapshot: snapshot,
		Data:     data,
		Page:     page,
	})
}

func writeQueryFailure(out io.Writer, queryErr *queryError) error {
	if err := encodeQueryEnvelope(out, queryEnvelope{
		Schema: querySchema,
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
