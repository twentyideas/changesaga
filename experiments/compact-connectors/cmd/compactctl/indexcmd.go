package main

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/index"
)

func runIndex(args []string) error {
	flags := flag.NewFlagSet("index", flag.ExitOnError)
	sagaRoot := flags.String("saga", "", "connector saga root")
	discard := flags.Bool("discard", false, "delete the index first, forcing a cold build")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *sagaRoot == "" {
		return fmt.Errorf("--saga is required")
	}
	ctx := context.Background()

	if *discard {
		existing, err := index.Open(ctx, *sagaRoot)
		if err == nil {
			if err := existing.Discard(); err != nil {
				return err
			}
		}
	}

	start := time.Now()
	handle, err := index.Open(ctx, *sagaRoot)
	if err != nil {
		return err
	}
	defer handle.Close()
	elapsed := time.Since(start)

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	size, err := handle.FileSize()
	if err != nil {
		return err
	}
	overview, err := handle.Overview(ctx)
	if err != nil {
		return err
	}
	path, err := index.Path(*sagaRoot)
	if err != nil {
		return err
	}

	fmt.Printf("index         %s\n", path)
	fmt.Printf("open          %s (%d shards rebuilt, %d reused, %d removed)\n",
		elapsed.Round(time.Millisecond), handle.Rebuilt, handle.Reused, handle.Removed)
	fmt.Printf("index size    %s\n", humanBytes(size))
	fmt.Printf("allocated     %.1f MB (cumulative)\n", float64(memory.TotalAlloc)/(1<<20))
	fmt.Printf("peak heap     %.1f MB\n", float64(memory.HeapSys)/(1<<20))
	fmt.Printf("shards        %d\n", overview.Shards)
	fmt.Printf("records       %d\n", overview.Records)
	fmt.Printf("line atoms    %d\n", overview.LineAtoms)
	fmt.Printf("event atoms   %d\n", overview.EventAtoms)
	fmt.Printf("owners        %d\n", overview.Owners)
	fmt.Printf("source files  %d\n", overview.SourceFiles)
	fmt.Printf("distinct notes %d\n", overview.Notes)
	fmt.Printf("comparisons   %d\n", overview.Comparisons)
	return nil
}

func runQuery(args []string) error {
	flags := flag.NewFlagSet("query", flag.ExitOnError)
	sagaRoot := flags.String("saga", "", "connector saga root")
	operation := flags.String("op", "overview", "overview, targets, or owners")
	sourcePath := flags.String("path", "", "source path, for owners")
	side := flags.String("side", "new", "old or new, for owners")
	line := flags.String("line", "1", "line number, for owners")
	repeat := flags.Int("repeat", 1, "run the operation this many times and report the median")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *sagaRoot == "" {
		return fmt.Errorf("--saga is required")
	}
	ctx := context.Background()

	openStart := time.Now()
	handle, err := index.Open(ctx, *sagaRoot)
	if err != nil {
		return err
	}
	defer handle.Close()
	fmt.Printf("open          %s (%d rebuilt, %d reused)\n",
		time.Since(openStart).Round(time.Microsecond), handle.Rebuilt, handle.Reused)

	number, err := strconv.Atoi(*line)
	if err != nil {
		return err
	}
	durations := make([]time.Duration, 0, *repeat)
	var render func()
	for run := 0; run < *repeat; run++ {
		start := time.Now()
		switch *operation {
		case "overview":
			overview, err := handle.Overview(ctx)
			if err != nil {
				return err
			}
			render = func() { fmt.Printf("%+v\n", overview) }
		case "targets":
			targets, err := handle.CoverageByTarget(ctx)
			if err != nil {
				return err
			}
			render = func() {
				for _, target := range targets {
					fmt.Printf("  %-72s %8d atoms  %4d files\n", target.Target, target.Atoms, target.Files)
				}
			}
		case "owners":
			if *sourcePath == "" {
				return fmt.Errorf("--path is required for owners")
			}
			owners, err := handle.OwnersOfLine(ctx, *sourcePath, *side, number)
			if err != nil {
				return err
			}
			render = func() {
				for _, owner := range owners {
					fmt.Printf("  %s\n    lines %d-%d via %s\n    note: %s\n", owner.Target, owner.Start, owner.End, owner.ShardPath, owner.Note)
				}
			}
		default:
			return fmt.Errorf("unknown operation %q", *operation)
		}
		durations = append(durations, time.Since(start))
	}
	fmt.Printf("%-13s %s (median of %d)\n", *operation, median(durations).Round(time.Microsecond), *repeat)
	render()
	return nil
}

func median(values []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}
