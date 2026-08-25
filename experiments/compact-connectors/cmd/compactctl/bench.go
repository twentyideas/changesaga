package main

import (
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/index"
	"github.com/twentyideas/changesaga/internal/gitdiff"
)

// treeStats measures what a saga actually costs a repository: the bytes on
// disk, and — because Git stores compressed objects — the bytes after
// deflate. Reporting only the raw size would flatter the compact encoding,
// since the v2 encoding's redundancy is exactly what a compressor eats.
type treeStats struct {
	Files      int
	Bytes      int64
	Compressed int64
}

func measureTree(root, extension, parent string) (treeStats, error) {
	var stats treeStats
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
			return nil
		}
		if parent != "" && filepath.Base(filepath.Dir(current)) != parent {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stats.Files++
		stats.Bytes += info.Size()

		// Compress each file on its own, which is how Git stores a blob.
		handle, err := os.Open(current)
		if err != nil {
			return err
		}
		defer handle.Close()
		counter := &byteCounter{}
		writer, err := gzip.NewWriterLevel(counter, gzip.BestCompression)
		if err != nil {
			return err
		}
		if _, err := io.Copy(writer, handle); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		stats.Compressed += counter.total
		return nil
	})
	return stats, err
}

type byteCounter struct{ total int64 }

func (c *byteCounter) Write(p []byte) (int, error) {
	c.total += int64(len(p))
	return len(p), nil
}

func runBench(args []string) error {
	flags := flag.NewFlagSet("bench", flag.ExitOnError)
	legacyRoot := flags.String("legacy", "", "v2 saga root")
	connectorRoot := flags.String("connectors", "", "connector saga root")
	repo := flags.String("repo", "", "source checkout")
	repeat := flags.Int("repeat", 5, "warm repetitions per measurement")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *legacyRoot == "" || *connectorRoot == "" {
		return fmt.Errorf("--legacy and --connectors are required")
	}
	ctx := context.Background()

	legacyStats, err := measureTree(*legacyRoot, ".json", connector.EvidenceDirectory)
	if err != nil {
		return err
	}
	connectorStats, err := measureTree(*connectorRoot, connector.Extension, connector.EvidenceDirectory)
	if err != nil {
		return err
	}

	fmt.Println("## Canonical committed evidence")
	fmt.Printf("v2 JSON        %6d files  %12s raw  %12s deflated\n",
		legacyStats.Files, humanBytes(legacyStats.Bytes), humanBytes(legacyStats.Compressed))
	fmt.Printf("connectors     %6d files  %12s raw  %12s deflated\n",
		connectorStats.Files, humanBytes(connectorStats.Bytes), humanBytes(connectorStats.Compressed))
	if connectorStats.Bytes > 0 {
		fmt.Printf("ratio          %38.1fx raw  %11.1fx deflated\n",
			float64(legacyStats.Bytes)/float64(connectorStats.Bytes),
			float64(legacyStats.Compressed)/float64(connectorStats.Compressed))
	}

	fmt.Println("\n## Derived SQLite index")
	coldStart := time.Now()
	cold, err := index.Open(ctx, *connectorRoot)
	if err != nil {
		return err
	}
	if err := cold.Discard(); err != nil {
		return err
	}
	cold, err = index.Open(ctx, *connectorRoot)
	if err != nil {
		return err
	}
	coldBuild := time.Since(coldStart)
	var afterBuild runtime.MemStats
	runtime.ReadMemStats(&afterBuild)
	indexSize, err := cold.FileSize()
	if err != nil {
		return err
	}
	overviewCold := time.Now()
	if _, err := cold.Overview(ctx); err != nil {
		return err
	}
	overviewColdDuration := time.Since(overviewCold)
	if err := cold.Close(); err != nil {
		return err
	}

	fmt.Printf("cold build     %s (%d shards) -> %s on disk, %.1f MB allocated\n",
		coldBuild.Round(time.Millisecond), cold.Rebuilt, humanBytes(indexSize), float64(afterBuild.TotalAlloc)/(1<<20))
	fmt.Printf("overview, cold %s\n", overviewColdDuration.Round(time.Microsecond))

	warmOpens := make([]time.Duration, 0, *repeat)
	warmOverviews := make([]time.Duration, 0, *repeat)
	warmTargets := make([]time.Duration, 0, *repeat)
	warmOwners := make([]time.Duration, 0, *repeat)
	for range *repeat {
		start := time.Now()
		handle, err := index.Open(ctx, *connectorRoot)
		if err != nil {
			return err
		}
		warmOpens = append(warmOpens, time.Since(start))

		start = time.Now()
		if _, err := handle.Overview(ctx); err != nil {
			return err
		}
		warmOverviews = append(warmOverviews, time.Since(start))

		start = time.Now()
		targets, err := handle.CoverageByTarget(ctx)
		if err != nil {
			return err
		}
		warmTargets = append(warmTargets, time.Since(start))
		_ = targets

		start = time.Now()
		if _, err := handle.OwnersOfLine(ctx, benchProbePath(*connectorRoot), "new", 1); err != nil {
			return err
		}
		warmOwners = append(warmOwners, time.Since(start))
		if err := handle.Close(); err != nil {
			return err
		}
	}
	fmt.Printf("warm open      %s\n", median(warmOpens).Round(time.Microsecond))
	fmt.Printf("overview, warm %s\n", median(warmOverviews).Round(time.Microsecond))
	fmt.Printf("target rollup  %s\n", median(warmTargets).Round(time.Microsecond))
	fmt.Printf("owners of line %s\n", median(warmOwners).Round(time.Microsecond))

	if *repo == "" {
		return nil
	}
	fmt.Println("\n## Whole-saga load and coverage evaluation")
	var changes gitdiff.ChangeSet
	legacy, err := evaluateLegacy(ctx, *legacyRoot, *repo, &changes)
	if err != nil {
		return err
	}
	var afterLegacy runtime.MemStats
	runtime.ReadMemStats(&afterLegacy)
	compact, err := evaluateConnectors(ctx, *connectorRoot, *repo, connector.Ranges, &changes)
	if err != nil {
		return err
	}
	exact, err := evaluateConnectors(ctx, *connectorRoot, *repo, connector.Exact, &changes)
	if err != nil {
		return err
	}
	var afterCompact runtime.MemStats
	runtime.ReadMemStats(&afterCompact)

	fmt.Printf("v2 JSON        load %-9s evaluate %-9s atoms %d\n",
		legacy.load.Round(time.Millisecond), legacy.evaluate.Round(time.Millisecond), legacy.report.Summary.Total)
	fmt.Printf("connectors     load %-9s evaluate %-9s atoms %d (ranged selectors)\n",
		compact.load.Round(time.Millisecond), compact.evaluate.Round(time.Millisecond), compact.report.Summary.Total)
	fmt.Printf("connectors     load %-9s evaluate %-9s atoms %d (expanded to v2 selector identity)\n",
		exact.load.Round(time.Millisecond), exact.evaluate.Round(time.Millisecond), exact.report.Summary.Total)
	fmt.Printf("allocated      v2 %.0f MB cumulative, connectors +%.0f MB for two further evaluations\n",
		float64(afterLegacy.TotalAlloc)/(1<<20), float64(afterCompact.TotalAlloc-afterLegacy.TotalAlloc)/(1<<20))
	return nil
}

// benchProbePath returns some source path the saga actually covers, so the
// point-query measurement is a hit rather than an empty scan.
func benchProbePath(root string) string {
	var probe string
	_ = filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil || probe != "" {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != connector.Extension {
			return nil
		}
		handle, err := os.Open(current)
		if err != nil {
			return err
		}
		defer handle.Close()
		file, err := connector.Parse(handle)
		if err != nil {
			return nil
		}
		probe = file.Source
		return nil
	})
	return probe
}
