package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/equiv"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/reader"
	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

type evaluation struct {
	document *saga.Saga
	report   coverage.Report
	load     time.Duration
	evaluate time.Duration
	shards   int
}

func evaluateLegacy(ctx context.Context, root, repo string, changes *gitdiff.ChangeSet) (evaluation, error) {
	start := time.Now()
	document, validation, err := saga.Load(root)
	if err != nil {
		return evaluation{}, err
	}
	load := time.Since(start)
	set, err := resolveChanges(ctx, document, repo, changes)
	if err != nil {
		return evaluation{}, err
	}
	start = time.Now()
	report := coverage.Evaluate(document, validation, set)
	return evaluation{document: document, report: report, load: load, evaluate: time.Since(start)}, nil
}

func evaluateConnectors(ctx context.Context, root, repo string, granularity connector.Granularity, changes *gitdiff.ChangeSet) (evaluation, error) {
	start := time.Now()
	document, validation, shards, err := reader.Load(root, granularity)
	if err != nil {
		return evaluation{}, err
	}
	load := time.Since(start)
	set, err := resolveChanges(ctx, document, repo, changes)
	if err != nil {
		return evaluation{}, err
	}
	start = time.Now()
	report := coverage.Evaluate(document, validation, set)
	return evaluation{document: document, report: report, load: load, evaluate: time.Since(start), shards: len(shards)}, nil
}

// resolveChanges reads the Git comparison once and reuses it. The comparison is
// a property of the source repository, not of the evidence encoding, so
// re-reading it per encoding would only add noise to the timings.
func resolveChanges(ctx context.Context, document *saga.Saga, repo string, cached *gitdiff.ChangeSet) (gitdiff.ChangeSet, error) {
	if cached != nil && cached.Repository != "" {
		return *cached, nil
	}
	source := document.Manifest.Source
	set, err := gitdiff.Read(ctx, repo, source.Repository, source.Base, source.Head)
	if err != nil {
		return gitdiff.ChangeSet{}, err
	}
	if cached != nil {
		*cached = set
	}
	return set, nil
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	legacyRoot := flags.String("legacy", "", "v2 saga root, the reference encoding")
	connectorRoot := flags.String("connectors", "", "connector saga root")
	repo := flags.String("repo", "", "source checkout")
	granularityName := flags.String("granularity", "exact", "granularity to read connector shards at")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *legacyRoot == "" || *connectorRoot == "" || *repo == "" {
		return fmt.Errorf("--legacy, --connectors, and --repo are required")
	}
	granularity, err := granularityFlag(*granularityName)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var changes gitdiff.ChangeSet
	legacy, err := evaluateLegacy(ctx, *legacyRoot, *repo, &changes)
	if err != nil {
		return err
	}
	compact, err := evaluateConnectors(ctx, *connectorRoot, *repo, granularity, &changes)
	if err != nil {
		return err
	}

	fmt.Printf("legacy     load %-8s evaluate %-8s atoms %d covered %d overlaps %d orphans %d\n",
		legacy.load.Round(time.Millisecond), legacy.evaluate.Round(time.Millisecond),
		legacy.report.Summary.Total, legacy.report.Summary.Covered,
		legacy.report.Summary.Overlapping, legacy.report.Summary.Orphaned)
	fmt.Printf("connectors load %-8s evaluate %-8s atoms %d covered %d overlaps %d orphans %d (%d shards, %s)\n",
		compact.load.Round(time.Millisecond), compact.evaluate.Round(time.Millisecond),
		compact.report.Summary.Total, compact.report.Summary.Covered,
		compact.report.Summary.Overlapping, compact.report.Summary.Orphaned,
		compact.shards, *granularityName)

	for _, issue := range compact.report.SchemaIssues {
		fmt.Printf("connector saga issue: %s %s: %s\n", issue.Severity, issue.Path, issue.Message)
	}
	for _, issue := range legacy.report.SchemaIssues {
		fmt.Printf("legacy saga issue: %s %s: %s\n", issue.Severity, issue.Path, issue.Message)
	}

	differences := equiv.Compare(legacy.document, legacy.report, compact.document, compact.report)
	if len(differences) == 0 {
		fmt.Println("equivalent: every atom has the same owners, the same notes, and the same overlap and stale verdicts")
		return nil
	}
	fmt.Printf("NOT equivalent: %d differences\n", len(differences))
	for i, difference := range differences {
		if i == 20 {
			fmt.Printf("  ... and %d more\n", len(differences)-20)
			break
		}
		fmt.Printf("  %s\n", difference)
	}
	return fmt.Errorf("%d semantic differences", len(differences))
}
