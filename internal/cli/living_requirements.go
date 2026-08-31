package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/twentyideas/changesaga/internal/requirements"
)

func Story(ctx context.Context, args []string, out io.Writer) error {
	operation := "story"
	var err error
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return livingFamilyHelp("story", []string{"add", "revise", "set-state"}, out)
	}
	operation += " " + args[0]
	switch args[0] {
	case "add":
		err = storyAdd(ctx, args[1:], out)
	case "revise":
		err = storyRevise(ctx, args[1:], out)
	case "set-state":
		err = storySetState(ctx, args[1:], out)
	default:
		err = fmt.Errorf("usage: %s", commandUsage["story"])
	}
	if err != nil && jsonFlagRequested(args) {
		return reportLivingMutationFailure(out, operation, err)
	}
	return err
}

func Citation(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return livingFamilyHelp("citation", []string{"add"}, out)
	}
	operation := "citation " + args[0]
	var err error
	if args[0] == "add" {
		err = citationAdd(ctx, args[1:], out)
	} else {
		err = fmt.Errorf("usage: %s", commandUsage["citation"])
	}
	if err != nil && jsonFlagRequested(args) {
		return reportLivingMutationFailure(out, operation, err)
	}
	return err
}

func Relation(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return livingFamilyHelp("relation", []string{"add", "supersede"}, out)
	}
	operation := "relation " + args[0]
	var err error
	switch args[0] {
	case "add":
		err = relationAdd(ctx, args[1:], out)
	case "supersede":
		err = relationSupersede(ctx, args[1:], out)
	default:
		err = fmt.Errorf("usage: %s", commandUsage["relation"])
	}
	if err != nil && jsonFlagRequested(args) {
		return reportLivingMutationFailure(out, operation, err)
	}
	return err
}

func requirementSagaID(root string) (string, error) {
	document, err := requirements.Load(root, "")
	if err != nil {
		return "", err
	}
	return document.SagaID, nil
}

func storyAdd(_ context.Context, args []string, out io.Writer) error {
	name := "story add"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	id := flags.String("id", "", "stable story id")
	revision := flags.String("revision", "", "stable initial revision id")
	event := flags.String("event", "", "stable initial proposed-event id")
	title := flags.String("title", "", "story title")
	statement := flags.String("statement", "", "complete user-story statement")
	priority := flags.String("priority", "", "story priority")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var citations, criteria stringList
	flags.Var(&citations, "citation", "citation URN; repeatable")
	flags.Var(&criteria, "criterion", "acceptance criterion as ID=STATEMENT; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *revision, *event, *title, *statement, *priority); err != nil {
		return err
	}
	parsed, err := parseCriteria(criteria)
	if err != nil {
		return err
	}
	values := make([]requirements.Criterion, len(parsed))
	for i := range parsed {
		values[i] = requirements.Criterion{ID: parsed[i].ID, Statement: parsed[i].Statement}
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.AddStory(root, sagaID, requirements.AddStoryInput{
		ID: *id, RevisionID: *revision, EventID: *event, Title: *title, Statement: *statement,
		Priority: *priority, Citations: citations, AcceptanceCriteria: values, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, nil, result.Replayed, *jsonOutput)
}

func storyRevise(_ context.Context, args []string, out io.Writer) error {
	name := "story revise"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	story := flags.String("story", "", "canonical story URN")
	revision := flags.String("revision", "", "stable revision id")
	title := flags.String("title", "", "complete revised title")
	statement := flags.String("statement", "", "complete revised user-story statement")
	priority := flags.String("priority", "", "complete revised priority")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents, citations, criteria stringList
	flags.Var(&parents, "parent", "current revision head URN; repeatable")
	flags.Var(&citations, "citation", "citation URN; repeatable")
	flags.Var(&criteria, "criterion", "acceptance criterion as ID=STATEMENT; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *story, *revision, *title, *statement, *priority); err != nil {
		return err
	}
	parsed, err := parseCriteria(criteria)
	if err != nil {
		return err
	}
	values := make([]requirements.Criterion, len(parsed))
	for i := range parsed {
		values[i] = requirements.Criterion{ID: parsed[i].ID, Statement: parsed[i].Statement}
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.ReviseStory(root, sagaID, requirements.ReviseStoryInput{
		Story: *story, ID: *revision, Parents: parents, Title: *title, Statement: *statement,
		Priority: *priority, Citations: citations, AcceptanceCriteria: values, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, nil, result.Replayed, *jsonOutput)
}

func storySetState(_ context.Context, args []string, out io.Writer) error {
	name := "story set-state"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	story := flags.String("story", "", "canonical story URN")
	event := flags.String("event", "", "stable lifecycle event id")
	state := flags.String("state", "", "proposed, accepted, deferred, rejected, or retired")
	reason := flags.String("reason", "", "reason for the lifecycle decision")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	var parents stringList
	flags.Var(&parents, "parent", "current lifecycle head URN; repeatable")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *story, *event, *state); err != nil {
		return err
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.SetStoryState(root, sagaID, requirements.SetStoryStateInput{
		Story: *story, ID: *event, Parents: parents, State: requirements.LifecycleState(*state),
		Reason: *reason, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, []string{*event}, result.Replayed, *jsonOutput)
}

func citationAdd(_ context.Context, args []string, out io.Writer) error {
	name := "citation add"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	id := flags.String("id", "", "stable citation id")
	kind := flags.String("kind", "", "url, repository_commit, issue, document, or decision")
	title := flags.String("title", "", "citation title")
	reference := flags.String("reference", "", "authoritative citation locator")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *kind, *title, *reference); err != nil {
		return err
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.AddCitation(root, sagaID, requirements.AddCitationInput{
		ID: *id, Kind: requirements.CitationKind(*kind), Title: *title, Reference: *reference, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, nil, result.Replayed, *jsonOutput)
}

func relationAdd(_ context.Context, args []string, out io.Writer) error {
	name := "relation add"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	id := flags.String("id", "", "stable relation id")
	typeName := flags.String("type", "", "refines, addresses, implements, verifies, supersedes, or conflicts_with")
	from := flags.String("from", "", "source endpoint URN")
	to := flags.String("to", "", "target endpoint URN")
	rationale := flags.String("rationale", "", "why the endpoints are related")
	fromRevision := flags.String("from-revision", "", "exact source definition revision URN")
	toRevision := flags.String("to-revision", "", "exact target definition revision URN")
	fromDigest := flags.String("from-content-digest", "", "exact source design content digest")
	toDigest := flags.String("to-content-digest", "", "exact target design content digest")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *id, *typeName, *from, *to, *rationale); err != nil {
		return err
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.AddRelation(root, sagaID, requirements.AddRelationInput{
		ID: *id, Type: requirements.RelationType(*typeName), From: *from, To: *to, Rationale: *rationale,
		FromRevision: *fromRevision, ToRevision: *toRevision, FromContentDigest: *fromDigest,
		ToContentDigest: *toDigest, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, nil, result.Replayed, *jsonOutput)
}

func relationSupersede(_ context.Context, args []string, out io.Writer) error {
	name := "relation supersede"
	usage := commandUsage[name]
	flags := commandFlags(name, usage, out)
	relation := flags.String("relation", "", "canonical relation URN")
	requestID := flags.String("request-id", "", "idempotency key")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	if err := flags.Parse(normalizeLivingArgs(args)); err != nil {
		return err
	}
	if err := requireLivingArgs(flags, *relation); err != nil {
		return err
	}
	root := flags.Arg(0)
	sagaID, err := requirementSagaID(root)
	if err != nil {
		return err
	}
	result, err := requirements.SupersedeRelation(root, sagaID, *relation, time.Time{}, *requestID)
	if err != nil {
		return err
	}
	return writeLivingMutation(out, name, result.URN, result.Path, []string{result.URN}, nil, result.Replayed, *jsonOutput)
}
