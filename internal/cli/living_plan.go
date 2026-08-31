package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/twentyideas/changesaga/internal/livingid"
	"github.com/twentyideas/changesaga/internal/workplan"
)

var planOperations = []string{
	"add-wave", "revise-wave", "add-item", "revise-item", "add-dependency",
	"add-contract", "assign", "progress", "record-merge",
}

func Plan(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return livingFamilyHelp("plan", planOperations, out)
	}
	operation := "plan " + args[0]
	var err error
	switch args[0] {
	case "add-wave":
		err = planAddWave(ctx, args[1:], out)
	case "revise-wave":
		err = planReviseWave(ctx, args[1:], out)
	case "add-item":
		err = planAddItem(ctx, args[1:], out)
	case "revise-item":
		err = planReviseItem(ctx, args[1:], out)
	case "add-dependency":
		err = planAddDependency(ctx, args[1:], out)
	case "add-contract":
		err = planAddContract(ctx, args[1:], out)
	case "assign":
		err = planAssign(ctx, args[1:], out)
	case "progress":
		err = planProgress(ctx, args[1:], out)
	case "record-merge":
		err = planRecordMerge(ctx, args[1:], out)
	default:
		err = fmt.Errorf("usage: %s", commandUsage["plan"])
	}
	if err != nil && jsonFlagRequested(args) {
		return reportLivingMutationFailure(out, operation, err)
	}
	return err
}

func writePlanResult(out io.Writer, operation string, result workplan.MutationResult, jsonOutput bool) error {
	return writeLivingMutation(out, operation, result.URN, result.Path, result.Created, result.EventIDs, result.Replayed, jsonOutput)
}

func planAddWave(_ context.Context, args []string, out io.Writer) error {
	name := "plan add-wave"
	flags := commandFlags(name, commandUsage[name], out)
	id := flags.String("id", "", "stable wave id")
	revision := flags.String("revision", "", "stable initial revision id")
	title := flags.String("title", "", "wave title")
	objective := flags.String("objective", "", "wave objective")
	order := flags.Int("order", 0, "display order (not a dependency)")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var entry, exit stringList
	flags.Var(&entry, "entry-condition", "entry condition; repeatable")
	flags.Var(&exit, "exit-condition", "exit condition; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *revision, *title, *objective, *requestID); err != nil {
		return err
	}
	result, err := workplan.CreateWave(flags.Arg(0), *id, workplan.WaveRevision{
		ID: *revision, Title: *title, Objective: *objective, Order: *order,
		EntryConditions: entry, ExitConditions: exit,
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

func planReviseWave(_ context.Context, args []string, out io.Writer) error {
	name := "plan revise-wave"
	flags := commandFlags(name, commandUsage[name], out)
	wave := flags.String("wave", "", "canonical wave URN")
	revision := flags.String("revision", "", "stable revision id")
	title := flags.String("title", "", "complete revised title")
	objective := flags.String("objective", "", "complete revised objective")
	order := flags.Int("order", 0, "complete revised display order")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents, entry, exit stringList
	flags.Var(&parents, "parent", "current revision head URN; repeatable")
	flags.Var(&entry, "entry-condition", "complete entry condition; repeatable")
	flags.Var(&exit, "exit-condition", "complete exit condition; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *wave, *revision, *title, *objective, *requestID); err != nil {
		return err
	}
	result, err := workplan.ReviseWave(flags.Arg(0), workplan.WaveRevision{
		ID: *revision, Wave: *wave, Parents: parents, Title: *title, Objective: *objective,
		Order: *order, EntryConditions: entry, ExitConditions: exit,
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

type itemFlags struct {
	id, revision, item, title, objective, wave, requestID     *string
	parents, deliverables, relations, dependencies, contracts stringList
	touchAreas, completionChecks, mergeUnits                  stringList
	jsonOutput                                                *bool
}

func addItemFlags(name string, out io.Writer, revise bool) (*itemFlags, interface {
	Parse([]string) error
	NArg() int
	Arg(int) string
}) {
	flags := commandFlags(name, commandUsage[name], out)
	value := &itemFlags{}
	if revise {
		value.item = flags.String("item", "", "canonical work-item URN")
	} else {
		value.id = flags.String("id", "", "stable work-item id")
	}
	value.revision = flags.String("revision", "", "stable definition revision id")
	value.title = flags.String("title", "", "work-item title")
	value.objective = flags.String("objective", "", "independently assignable objective")
	value.wave = flags.String("wave", "", "current wave URN")
	value.requestID = flags.String("request-id", "", "required idempotency key")
	value.jsonOutput = flags.Bool("json", false, "emit a machine-readable result")
	flags.Var(&value.parents, "parent", "current revision head URN; repeatable")
	flags.Var(&value.deliverables, "deliverable", "declared deliverable; repeatable")
	flags.Var(&value.relations, "relation", "requirements/design relation URN; repeatable")
	flags.Var(&value.dependencies, "dependency", "dependency URN; repeatable")
	flags.Var(&value.contracts, "contract", "contract or contract-revision URN; repeatable")
	flags.Var(&value.touchAreas, "touch-area", "touch-area JSON object; repeatable")
	flags.Var(&value.completionChecks, "completion-check", "completion check; repeatable")
	flags.Var(&value.mergeUnits, "merge-unit", "merge-unit JSON object; repeatable")
	return value, flags
}

func buildWorkItemRevision(value *itemFlags) (workplan.WorkItemRevision, error) {
	revision := workplan.WorkItemRevision{
		ID: *value.revision, Parents: value.parents, Title: *value.title, Objective: *value.objective,
		Deliverables: value.deliverables, Wave: *value.wave, Relations: value.relations,
		Dependencies: value.dependencies, Contracts: value.contracts, CompletionChecks: value.completionChecks,
		ExpectedTouchAreas: []workplan.TouchArea{}, MergeUnits: []workplan.MergeUnit{},
	}
	for index, raw := range value.touchAreas {
		var area workplan.TouchArea
		if err := decodeInlineJSON(raw, fmt.Sprintf("--touch-area %d", index+1), &area); err != nil {
			return revision, err
		}
		revision.ExpectedTouchAreas = append(revision.ExpectedTouchAreas, area)
	}
	for index, raw := range value.mergeUnits {
		var unit workplan.MergeUnit
		if err := decodeInlineJSON(raw, fmt.Sprintf("--merge-unit %d", index+1), &unit); err != nil {
			return revision, err
		}
		revision.MergeUnits = append(revision.MergeUnits, unit)
	}
	return revision, nil
}

func planAddItem(_ context.Context, args []string, out io.Writer) error {
	name := "plan add-item"
	value, parser := addItemFlags(name, out, false)
	if err := parser.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if parser.NArg() != 1 || *value.id == "" || *value.revision == "" || *value.title == "" || *value.objective == "" || len(value.deliverables) == 0 || *value.requestID == "" {
		return fmt.Errorf("usage: %s", commandUsage[name])
	}
	revision, err := buildWorkItemRevision(value)
	if err != nil {
		return err
	}
	result, err := workplan.CreateWorkItem(parser.Arg(0), *value.id, revision, *value.requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *value.jsonOutput)
}

func planReviseItem(_ context.Context, args []string, out io.Writer) error {
	name := "plan revise-item"
	value, parser := addItemFlags(name, out, true)
	if err := parser.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if parser.NArg() != 1 || *value.item == "" || *value.revision == "" || len(value.parents) == 0 || *value.title == "" || *value.objective == "" || len(value.deliverables) == 0 || *value.requestID == "" {
		return fmt.Errorf("usage: %s", commandUsage[name])
	}
	revision, err := buildWorkItemRevision(value)
	if err != nil {
		return err
	}
	revision.WorkItem = *value.item
	result, err := workplan.ReviseWorkItem(parser.Arg(0), revision, *value.requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *value.jsonOutput)
}

func planAddDependency(_ context.Context, args []string, out io.Writer) error {
	name := "plan add-dependency"
	flags := commandFlags(name, commandUsage[name], out)
	id := flags.String("id", "", "stable dependency id")
	prerequisite := flags.String("prerequisite", "", "prerequisite work-item URN")
	dependent := flags.String("dependent", "", "dependent work-item URN")
	condition := flags.String("condition", "", "progress_done, merge_integrated, or contract_fulfilled")
	contractRevision := flags.String("contract-revision", "", "exact contract revision for contract_fulfilled")
	reason := flags.String("reason", "", "why the dependency is necessary")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *prerequisite, *dependent, *condition, *reason, *requestID); err != nil {
		return err
	}
	result, err := workplan.CreateDependency(flags.Arg(0), workplan.Dependency{
		ID: *id, Prerequisite: *prerequisite, Dependent: *dependent, Reason: *reason,
		Condition: workplan.DependencyCondition{Kind: *condition, ContractRevision: *contractRevision},
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

func planAddContract(_ context.Context, args []string, out io.Writer) error {
	name := "plan add-contract"
	flags := commandFlags(name, commandUsage[name], out)
	id := flags.String("id", "", "stable contract id")
	revision := flags.String("revision", "", "stable initial revision id")
	kind := flags.String("kind", "", "deliverable, interface, handoff, or quality_gate")
	provider := flags.String("provider", "", "provider work-item URN")
	consumer := flags.String("consumer", "", "consumer work-item URN")
	statement := flags.String("statement", "", "contract statement")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var acceptance stringList
	flags.Var(&acceptance, "acceptance", "acceptance check; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *revision, *kind, *provider, *consumer, *statement, *requestID); err != nil {
		return err
	}
	if len(acceptance) == 0 {
		return fmt.Errorf("--acceptance is required")
	}
	result, err := workplan.CreateContract(flags.Arg(0), *id, workplan.ContractRevision{
		ID: *revision, Kind: *kind, Provider: *provider, Consumer: *consumer,
		Statement: *statement, Acceptance: acceptance,
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

func itemIDForSaga(root, urn string) (string, error) {
	ref, err := livingid.Parse(urn)
	if err != nil || ref.Kind != livingid.KindWorkItem {
		return "", fmt.Errorf("--item must be a canonical work-item URN")
	}
	plan, validation, err := workplan.Load(root)
	if err != nil {
		return "", err
	}
	if !validation.Valid {
		return "", fmt.Errorf("cannot mutate invalid work plan")
	}
	if ref.SagaID != plan.SagaID {
		return "", fmt.Errorf("--item must name a work item in this Saga")
	}
	return ref.ID, nil
}

func planAssign(_ context.Context, args []string, out io.Writer) error {
	name := "plan assign"
	flags := commandFlags(name, commandUsage[name], out)
	item := flags.String("item", "", "canonical work-item URN")
	workspace := flags.String("workspace", "", "DevSwarm workspace UUID")
	repositoryID := flags.String("repository-id", "", "portable repository identity")
	branch := flags.String("branch", "", "workspace branch")
	sourceBranch := flags.String("source-branch", "", "workspace source branch")
	label := flags.String("label", "", "optional workspace label")
	role := flags.String("role", "owner", "owner, contributor, or observer")
	event := flags.String("event", "", "stable event id; generated when omitted")
	summary := flags.String("summary", "", "assignment summary")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents stringList
	flags.Var(&parents, "parent", "current assignment head URN; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *item, *workspace, *repositoryID, *branch, *requestID); err != nil {
		return err
	}
	root := flags.Arg(0)
	id, err := itemIDForSaga(root, *item)
	if err != nil {
		return err
	}
	result, err := workplan.RecordWorkspace(root, id, workplan.WorkspaceEvent{
		EventBase: workplan.EventBase{ID: *event, Parents: parents, Summary: *summary},
		Action:    "assigned", Role: *role, Workspace: workplan.Workspace{
			Provider: "devswarm", ID: *workspace, RepositoryID: *repositoryID, Branch: *branch,
			SourceBranch: *sourceBranch, Label: *label,
		},
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

func planProgress(_ context.Context, args []string, out io.Writer) error {
	name := "plan progress"
	flags := commandFlags(name, commandUsage[name], out)
	item := flags.String("item", "", "canonical work-item URN")
	to := flags.String("to", "", "planned, ready, in_progress, blocked, done, or cancelled")
	event := flags.String("event", "", "stable event id; generated when omitted")
	reason := flags.String("reason", "", "reason, required for blocked and cancelled")
	summary := flags.String("summary", "", "progress summary")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents stringList
	flags.Var(&parents, "from", "current progress head event URN; repeatable for reconciliation")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *item, *to, *requestID); err != nil {
		return err
	}
	root := flags.Arg(0)
	id, err := itemIDForSaga(root, *item)
	if err != nil {
		return err
	}
	result, err := workplan.RecordProgress(root, id, workplan.ProgressEvent{
		EventBase: workplan.EventBase{ID: *event, Parents: parents, Summary: *summary}, State: *to, Reason: *reason,
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}

func planRecordMerge(_ context.Context, args []string, out io.Writer) error {
	name := "plan record-merge"
	flags := commandFlags(name, commandUsage[name], out)
	item := flags.String("item", "", "canonical work-item URN")
	unit := flags.String("unit", "", "merge-unit id from the current work-item revision")
	state := flags.String("state", "", "planned, ready, integrated, reverted, or abandoned")
	event := flags.String("event", "", "stable event id; generated when omitted")
	commit := flags.String("commit", "", "full merge commit OID (alias for --merge-oid)")
	headOID := flags.String("head-oid", "", "full delivered head OID")
	mergeOID := flags.String("merge-oid", "", "full integration commit OID")
	revertOID := flags.String("revert-oid", "", "full revert commit OID")
	summary := flags.String("summary", "", "merge-event summary")
	requestID := flags.String("request-id", "", "required idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents stringList
	flags.Var(&parents, "from", "current merge head event URN; repeatable for reconciliation")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *item, *unit, *state, *requestID); err != nil {
		return err
	}
	if *commit != "" {
		if *mergeOID != "" && *mergeOID != *commit {
			return fmt.Errorf("--commit and --merge-oid disagree")
		}
		*mergeOID = *commit
	}
	root := flags.Arg(0)
	id, err := itemIDForSaga(root, *item)
	if err != nil {
		return err
	}
	result, err := workplan.RecordMerge(root, id, workplan.MergeEvent{
		EventBase: workplan.EventBase{ID: *event, Parents: parents, Summary: *summary}, Unit: *unit,
		State: *state, HeadOID: *headOID, MergeOID: *mergeOID, RevertOID: *revertOID,
	}, *requestID)
	if err != nil {
		return err
	}
	return writePlanResult(out, name, result, *jsonOutput)
}
