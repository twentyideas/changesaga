// compactctl drives the compact-connector experiment: it migrates a v2 saga to
// the connector encoding, migrates it back, verifies that the round trip
// preserves coverage semantics, builds and queries the disposable SQLite index,
// and prints the measurements behind docs/findings.md.
//
// It is an experiment harness, not a product surface. Nothing here is wired
// into `change-saga`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/migrate"
	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "stats":
		err = runStats(os.Args[2:])
	case "migrate":
		err = runMigrate(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "index":
		err = runIndex(os.Args[2:])
	case "query":
		err = runQuery(os.Args[2:])
	case "bench":
		err = runBench(os.Args[2:])
	case "packsize":
		err = runPackSize(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "compactctl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `compactctl <command> [flags]

  stats    --saga PATH [--repo PATH]   load a saga and report its size and coverage shape
  migrate  --saga PATH --out PATH [--granularity exact|ranges] [--dual] [--back]
  verify   --legacy PATH --connectors PATH --repo PATH [--granularity exact|ranges]
  index    --saga PATH [--discard]
  query    --saga PATH --op overview|targets|owners [--path P --side new|old --line N] [--repeat N]
  bench    --legacy PATH --connectors PATH [--repo PATH] [--repeat N]
  packsize --tree PATH [--extension .json|.connectors]
`)
}

func granularityFlag(value string) (connector.Granularity, error) {
	switch value {
	case "exact":
		return connector.Exact, nil
	case "ranges":
		return connector.Ranges, nil
	default:
		return 0, fmt.Errorf("granularity must be exact or ranges, not %q", value)
	}
}

func runStats(args []string) error {
	flags := flag.NewFlagSet("stats", flag.ExitOnError)
	sagaRoot := flags.String("saga", "", "saga root")
	repo := flags.String("repo", "", "source checkout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *sagaRoot == "" {
		return fmt.Errorf("--saga is required")
	}

	start := time.Now()
	document, validation, err := saga.Load(*sagaRoot)
	if err != nil {
		return err
	}
	loadDuration := time.Since(start)
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	owners := migrate.Owners(document)
	files, references, withEvidence := 0, 0, 0
	singleLine, ranged, events := 0, 0, 0
	sources := map[string]bool{}
	for _, owner := range owners {
		if len(owner.Diffs) > 0 {
			withEvidence++
		}
		for _, diffFile := range owner.Diffs {
			files++
			references += len(diffFile.Diffs)
			for _, reference := range diffFile.Diffs {
				record, err := connector.FromReference(reference)
				if err != nil {
					continue
				}
				sources[record.SourcePath()] = true
				switch {
				case record.Kind == "event":
					events++
				case record.Start == record.End:
					singleLine++
				default:
					ranged++
				}
			}
		}
	}

	fmt.Printf("saga            %s\n", document.Manifest.ID)
	fmt.Printf("valid           %t (%d issues)\n", validation.Valid, len(validation.Issues))
	fmt.Printf("load            %s\n", loadDuration.Round(time.Millisecond))
	fmt.Printf("allocated       %.1f MB (cumulative, not peak)\n", float64(memory.TotalAlloc)/(1<<20))
	fmt.Printf("live heap       %.1f MB\n", float64(memory.HeapAlloc)/(1<<20))
	fmt.Printf("peak heap       %.1f MB\n", float64(memory.HeapSys)/(1<<20))
	fmt.Printf("targets         %d (%d own evidence)\n", len(owners), withEvidence)
	fmt.Printf("evidence files  %d\n", files)
	fmt.Printf("references      %d\n", references)
	fmt.Printf("source paths    %d\n", len(sources))
	fmt.Printf("line refs       %d single-line, %d ranged, %d events\n", singleLine, ranged, events)

	if *repo != "" {
		start = time.Now()
		changes, err := gitdiff.Read(context.Background(), *repo,
			document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head)
		if err != nil {
			return err
		}
		diffDuration := time.Since(start)
		start = time.Now()
		report := coverage.Evaluate(document, validation, changes)
		evaluateDuration := time.Since(start)
		fmt.Printf("git diff        %s\n", diffDuration.Round(time.Millisecond))
		fmt.Printf("coverage        %s\n", evaluateDuration.Round(time.Millisecond))
		fmt.Printf("atoms           %d\n", report.Summary.Total)
		fmt.Printf("covered         %d\n", report.Summary.Covered)
		fmt.Printf("uncovered       %d\n", report.Summary.Uncovered)
		fmt.Printf("overlapping     %d\n", report.Summary.Overlapping)
		fmt.Printf("orphaned        %d\n", report.Summary.Orphaned)
	}
	return nil
}

func runMigrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ExitOnError)
	sagaRoot := flags.String("saga", "", "saga root")
	out := flags.String("out", "", "destination root")
	granularityName := flags.String("granularity", "ranges", "exact or ranges")
	back := flags.Bool("back", false, "migrate connectors back to v2 evidence")
	dual := flags.Bool("dual", false, "keep the v2 JSON evidence beside the connector shards")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *sagaRoot == "" || *out == "" {
		return fmt.Errorf("--saga and --out are required")
	}
	granularity, err := granularityFlag(*granularityName)
	if err != nil {
		return err
	}
	start := time.Now()
	var result migrate.Result
	if *back {
		result, err = migrate.ToLegacy(*sagaRoot, *out, granularity)
	} else {
		mode := migrate.Connectors
		if *dual {
			mode = migrate.Dual
		}
		result, err = migrate.ToConnectors(*sagaRoot, *out, granularity, mode)
	}
	if err != nil {
		return err
	}
	fmt.Printf("elapsed         %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("owners          %d\n", result.Owners)
	if *back {
		fmt.Printf("read            %d connector records\n", result.Records)
		fmt.Printf("wrote           %d v2 evidence files (%s, %d references)\n",
			result.LegacyFiles, humanBytes(result.LegacyBytes), result.LegacyRefs)
		return nil
	}
	fmt.Printf("read            %d v2 evidence files (%s, %d references)\n",
		result.LegacyFiles, humanBytes(result.LegacyBytes), result.LegacyRefs)
	fmt.Printf("wrote           %d connector shards (%s, %d records)\n",
		result.ConnectorFiles, humanBytes(result.ConnectorBytes), result.Records)
	if result.ConnectorBytes > 0 {
		fmt.Printf("  of which header %s (%.0f%%)\n", humanBytes(result.HeaderBytes),
			100*float64(result.HeaderBytes)/float64(result.ConnectorBytes))
		if result.LegacyBytes > 0 {
			fmt.Printf("ratio           %.1fx smaller raw\n", float64(result.LegacyBytes)/float64(result.ConnectorBytes))
		}
	}
	return nil
}

func humanBytes(value int64) string {
	switch {
	case value >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(value)/(1<<30))
	case value >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(value)/(1<<20))
	case value >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(value)/(1<<10))
	default:
		return fmt.Sprintf("%d B", value)
	}
}
