package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/impact"
	"github.com/twentyideas/changesaga/internal/saga"
)

// Compare projects an incoming source comparison onto an existing Saga's
// evidence graph. Authored fragment bytes are never read for comparison.
func Compare(ctx context.Context, args []string, out io.Writer) error {
	flags := commandFlags("compare", commandUsage["compare"], out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable JSON")
	repoDir := flags.String("repo", "", "source repository checkout")
	againstSagaRoot := flags.String("against-saga", "", "incoming Saga whose source comparison should be projected")
	againstRepoDir := flags.String("against-repo", "", "source checkout for --against-saga; defaults to --repo")
	base := flags.String("base", "", "incoming Git comparison base")
	head := flags.String("head", "", "incoming Git comparison head; defaults to HEAD with --base")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["compare"])
	}
	if *againstSagaRoot != "" && (*base != "" || *head != "") {
		return fmt.Errorf("--against-saga cannot be combined with --base or --head")
	}
	if *againstSagaRoot == "" && *base == "" {
		return fmt.Errorf("provide --against-saga or --base")
	}
	if *againstSagaRoot == "" && *head == "" {
		*head = "HEAD"
	}

	root := flags.Arg(0)
	document, validation, err := saga.Load(root)
	if err != nil {
		return fmt.Errorf("load maintained Saga: %w", err)
	}
	if !validation.Valid {
		return fmt.Errorf("maintained Saga is structurally invalid; run change-saga validate")
	}
	checkout := firstNonEmpty(*repoDir, document.Root)
	readOptions := gitdiff.ReadOptions{AllowRepositoryMismatch: *allowRepositoryMismatch}

	mode := "saga_to_diff"
	var incomingDocument *saga.Saga
	var incoming gitdiff.ChangeSet
	if *againstSagaRoot != "" {
		mode = "saga_to_saga"
		var incomingValidation saga.Validation
		incomingDocument, incomingValidation, err = saga.Load(*againstSagaRoot)
		if err != nil {
			return fmt.Errorf("load incoming Saga: %w", err)
		}
		if !incomingValidation.Valid {
			return fmt.Errorf("incoming Saga is structurally invalid; run change-saga validate")
		}
		if incomingDocument.Manifest.Source.Repository != document.Manifest.Source.Repository {
			return fmt.Errorf("cannot compare Sagas from different source repositories")
		}
		incomingCheckout := firstNonEmpty(*againstRepoDir, *repoDir, incomingDocument.Root)
		incoming, err = gitdiff.ReadWithOptions(ctx, incomingCheckout, incomingDocument.Manifest.Source.Repository, incomingDocument.Manifest.Source.Base, incomingDocument.Manifest.Source.Head, readOptions)
	} else {
		incoming, err = gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, *base, *head, readOptions)
	}
	if err != nil {
		return fmt.Errorf("read incoming source diff: %w", err)
	}
	if incoming.Repository != document.Manifest.Source.Repository {
		return fmt.Errorf("incoming comparison repository does not match maintained Saga repository")
	}

	// The incoming comparison's resolved merge base is the exact code tree the
	// maintained Saga must describe before impact can be projected safely.
	baseline, err := gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, incoming.BaseOID, readOptions)
	if err != nil {
		return fmt.Errorf("reconstruct maintained Saga at incoming base: %w", err)
	}
	report := coverage.Evaluate(document, validation, baseline)
	result := impact.Analyze(document, baseline, report, incoming, mode, incomingDocument)
	if *jsonOutput {
		if err := writeJSON(out, result); err != nil {
			return err
		}
	} else {
		printImpact(out, result)
	}
	if !result.Baseline.Complete {
		return &StatusError{Code: 3}
	}
	return nil
}

func printImpact(out io.Writer, result impact.Result) {
	fmt.Fprintf(out, "%d source changes affect %d existing Saga targets; %d changes need new content\n",
		result.Summary.IncomingAtoms,
		result.Summary.TargetsMustUpdate+result.Summary.TargetsConsiderUpdate,
		result.Summary.NewContentRequired,
	)
	fmt.Fprintln(out, "Impact is derived only from source diffs and existing evidence ownership; Saga content is not compared.")
	fmt.Fprintf(out, "Direct intersections: %d  Contextual additions: %d  Baseline mapping: %d/%d\n",
		result.Summary.DirectIntersections, result.Summary.ContextualAdditions,
		result.Baseline.Covered, result.Baseline.Total,
	)
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(out, "\nWARNING %s: %s\n", diagnostic.Code, diagnostic.Message)
	}
	for _, action := range []string{"must_update", "consider_update"} {
		var targets []impact.TargetImpact
		for _, target := range result.Targets {
			if target.Action == action {
				targets = append(targets, target)
			}
		}
		if len(targets) == 0 {
			continue
		}
		heading := "Must update"
		if action == "consider_update" {
			heading = "Consider updating"
		}
		fmt.Fprintf(out, "\n%s:\n", heading)
		for _, target := range targets {
			fmt.Fprintf(out, "  %s [%s]\n    %s", target.Title, target.Kind, target.Target)
			if target.ContentPath != "" {
				fmt.Fprintf(out, "\n    content: %s", target.ContentPath)
			}
			if len(target.EvidenceFiles) > 0 {
				fmt.Fprintf(out, "\n    evidence: %v", target.EvidenceFiles)
			}
			fmt.Fprintln(out)
			for _, change := range target.Changes {
				fmt.Fprintf(out, "      %s: %s\n", change.Relationship, coverage.DescribeAtom(change.Atom))
			}
		}
	}
	if len(result.NewContent) > 0 {
		fmt.Fprintln(out, "\nNew content required:")
		values := append([]impact.UnownedChange(nil), result.NewContent...)
		sort.SliceStable(values, func(i, j int) bool { return values[i].Atom.URI < values[j].Atom.URI })
		for _, change := range values {
			fmt.Fprintf(out, "  %s\n    %s\n", coverage.DescribeAtom(change.Atom), change.Reason)
		}
	}
}
