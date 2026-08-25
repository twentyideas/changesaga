package migrate_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/equiv"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/migrate"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/reader"
	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

type fixture struct {
	sagaRoot   string
	repository string
	changes    gitdiff.ChangeSet
	document   *saga.Saga
	report     coverage.Report
	bytes      int64
	files      int
}

// buildFixture generates a real Git comparison and a fully covered v2 saga, and
// evaluates it through the unmodified coverage engine. Everything the tests
// below assert is measured against that evaluation rather than against a
// hand-written expectation, so a change in coverage semantics cannot pass by
// agreeing with a stale fixture.
func buildFixture(t *testing.T) fixture {
	t.Helper()
	options := testfixture.DefaultLargeSagaOptions()
	options.Chapters = 2
	options.SectionsPerChapter = 2
	options.FragmentsPerSection = 2
	options.SourceFiles = 6
	options.ChangedLinesPerFile = 24
	options.Threads = 2
	options.DiffReviews = 2

	generated, err := testfixture.GenerateLargeSaga(context.Background(), t.TempDir(), options)
	if err != nil {
		t.Fatalf("generate fixture: %v", err)
	}
	document, validation, err := saga.Load(generated.Root)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("generated fixture is invalid: %+v", validation.Issues)
	}
	source := document.Manifest.Source
	changes, err := gitdiff.Read(context.Background(), generated.Repository, source.Repository, source.Base, source.Head)
	if err != nil {
		t.Fatalf("read comparison: %v", err)
	}
	report := coverage.Evaluate(document, validation, changes)
	if report.Summary.Total == 0 {
		t.Fatal("fixture comparison has no atoms")
	}
	size, count := treeSize(t, generated.Root, ".json", "___diffs")
	return fixture{
		sagaRoot: generated.Root, repository: generated.Repository, changes: changes,
		document: document, report: report, bytes: size, files: count,
	}
}

func treeSize(t *testing.T, root, extension, parent string) (int64, int) {
	t.Helper()
	var total int64
	var count int
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
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
		total += info.Size()
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total, count
}

func evaluateConnectors(t *testing.T, root string, base fixture, granularity connector.Granularity) (*saga.Saga, coverage.Report) {
	t.Helper()
	document, validation, shards, err := reader.Load(root, granularity)
	if err != nil {
		t.Fatalf("load connector saga: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("connector saga is invalid: %+v", validation.Issues)
	}
	if len(shards) == 0 {
		t.Fatal("connector saga contains no shards")
	}
	return document, coverage.Evaluate(document, validation, base.changes)
}

// This is the load-bearing test of the whole experiment: the compact encoding
// must say exactly what the v2 encoding says about every atom.
func TestConnectorRoundTripPreservesCoverageSemantics(t *testing.T) {
	base := buildFixture(t)

	for _, granularity := range []struct {
		name  string
		value connector.Granularity
	}{{"exact", connector.Exact}, {"ranges", connector.Ranges}} {
		t.Run(granularity.name, func(t *testing.T) {
			compactRoot := filepath.Join(t.TempDir(), "compact.saga")
			result, err := migrate.ToConnectors(base.sagaRoot, compactRoot, granularity.value, migrate.Connectors)
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			if result.ConnectorFiles == 0 {
				t.Fatal("migration produced no connector files")
			}
			if result.ConnectorBytes >= result.LegacyBytes {
				t.Fatalf("connector encoding is not smaller: %d >= %d", result.ConnectorBytes, result.LegacyBytes)
			}

			// Reading the compact encoding at exact granularity must reproduce
			// v2 selector-for-selector, which is what preserves stale-reference
			// reporting and not merely atom counts.
			document, report := evaluateConnectors(t, compactRoot, base, connector.Exact)
			if differences := equiv.Compare(base.document, base.report, document, report); len(differences) != 0 {
				t.Fatalf("connector encoding changed coverage meaning: %v", differences)
			}

			// The reverse migration has to hand a v2-only reader something it
			// can read, with the same meaning again.
			legacyRoot := filepath.Join(t.TempDir(), "legacy.saga")
			if _, err := migrate.ToLegacy(compactRoot, legacyRoot, connector.Exact); err != nil {
				t.Fatalf("migrate back: %v", err)
			}
			restored, restoredValidation, err := saga.Load(legacyRoot)
			if err != nil {
				t.Fatalf("load restored saga: %v", err)
			}
			if !restoredValidation.Valid {
				t.Fatalf("restored saga is invalid: %+v", restoredValidation.Issues)
			}
			restoredReport := coverage.Evaluate(restored, restoredValidation, base.changes)
			if differences := equiv.Compare(base.document, base.report, restored, restoredReport); len(differences) != 0 {
				t.Fatalf("v2 -> connectors -> v2 changed coverage meaning: %v", differences)
			}
		})
	}
}

// The transition state has to be safe under a reader that knows nothing about
// connectors. A v2 loader reads only `*.json` from `___diffs`, so the shards
// beside them must be invisible to it and change nothing it reports.
func TestDualEncodedSagaIsUnchangedForAnUnmodifiedV2Reader(t *testing.T) {
	base := buildFixture(t)
	dualRoot := filepath.Join(t.TempDir(), "dual.saga")
	if _, err := migrate.ToConnectors(base.sagaRoot, dualRoot, connector.Ranges, migrate.Dual); err != nil {
		t.Fatalf("migrate dual: %v", err)
	}

	shards, err := reader.Stat(dualRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) == 0 {
		t.Fatal("dual migration wrote no connector shards")
	}

	document, validation, err := saga.Load(dualRoot)
	if err != nil {
		t.Fatalf("an unmodified v2 loader could not read the dual-encoded saga: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("the dual-encoded saga is invalid to a v2 loader: %+v", validation.Issues)
	}
	report := coverage.Evaluate(document, validation, base.changes)
	if differences := equiv.Compare(base.document, base.report, document, report); len(differences) != 0 {
		t.Fatalf("adding connector shards changed what a v2 reader reports: %v", differences)
	}
}

// Dropping the v2 JSON is the step that a v2 reader cannot follow. It must fail
// loudly rather than report a fully documented change as entirely undocumented,
// so the connector-only state declares itself with a version a v2 reader
// rejects.
func TestConnectorOnlySagaMustAnnounceItselfWithAVersionV2Rejects(t *testing.T) {
	base := buildFixture(t)
	compactRoot := filepath.Join(t.TempDir(), "compact.saga")
	if _, err := migrate.ToConnectors(base.sagaRoot, compactRoot, connector.Ranges, migrate.Connectors); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Without a tripwire, a v2 reader loads the saga happily and reports every
	// atom as uncovered. That is the silent failure the version bump exists to
	// prevent, and this asserts it really is the behaviour being prevented.
	silent, silentValidation, err := saga.Load(compactRoot)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	silentReport := coverage.Evaluate(silent, silentValidation, base.changes)
	if silentReport.Summary.Covered != 0 {
		t.Fatalf("expected a v2 reader to see no coverage at all, saw %d", silentReport.Summary.Covered)
	}

	manifestPath := filepath.Join(compactRoot, "saga.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["version"] = saga.CurrentVersion + 1
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}

	_, validation, err := saga.Load(compactRoot)
	if err != nil {
		return // A hard load error is an even louder failure, and is fine.
	}
	if validation.Valid {
		t.Fatal("a v2 reader accepted a connector-only saga; it would report the change as undocumented")
	}
	var announced bool
	for _, issue := range validation.Issues {
		if issue.Severity == "error" && strings.Contains(issue.Message, "unsupported version") {
			announced = true
		}
	}
	if !announced {
		t.Fatalf("expected an unsupported-version error, got %+v", validation.Issues)
	}
}

func TestShardNameIsDeterministicAndCollisionFree(t *testing.T) {
	paths := []string{
		"libs/daylight/runtime-update/src/lib/contracts.ts",
		"libs/daylight/runtime-update/src/lib/contracts.spec.ts",
		"a.ts",
		"docs/design notes.md",
		strings.Repeat("very/deep/", 20) + "file.go",
	}
	seen := map[string]string{}
	for _, path := range paths {
		name := migrate.ShardName(path)
		if again := migrate.ShardName(path); again != name {
			t.Fatalf("shard name for %q is not deterministic: %q vs %q", path, name, again)
		}
		if !strings.HasSuffix(name, connector.Extension) {
			t.Fatalf("shard name %q has the wrong extension", name)
		}
		if previous, ok := seen[name]; ok {
			t.Fatalf("shard name %q collides for %q and %q", name, previous, path)
		}
		seen[name] = path
	}
}

// A dual-encoded saga states the same atoms twice. A connector-aware reader
// must take one side and drop the other, or every atom would acquire two
// owners and the saga would report an overlap on every line it documents.
func TestConnectorReaderIgnoresTheV2JsonBesideAShard(t *testing.T) {
	base := buildFixture(t)
	dualRoot := filepath.Join(t.TempDir(), "dual.saga")
	if _, err := migrate.ToConnectors(base.sagaRoot, dualRoot, connector.Ranges, migrate.Dual); err != nil {
		t.Fatalf("migrate dual: %v", err)
	}
	document, validation, _, err := reader.Load(dualRoot, connector.Exact)
	if err != nil {
		t.Fatalf("load dual: %v", err)
	}
	report := coverage.Evaluate(document, validation, base.changes)
	if report.Summary.Overlapping != base.report.Summary.Overlapping {
		t.Fatalf("reading a dual-encoded saga manufactured %d overlaps, want %d",
			report.Summary.Overlapping, base.report.Summary.Overlapping)
	}
	if differences := equiv.Compare(base.document, base.report, document, report); len(differences) != 0 {
		t.Fatalf("reading a dual-encoded saga changed coverage meaning: %v", differences)
	}
}

// A comparison that reports no differences is only reassuring if it can report
// one. This corrupts a single record and requires the comparison to notice —
// otherwise every equivalence result in this package would be vacuous.
func TestEquivalenceComparisonDetectsAChangedOwner(t *testing.T) {
	base := buildFixture(t)
	compactRoot := filepath.Join(t.TempDir(), "compact.saga")
	if _, err := migrate.ToConnectors(base.sagaRoot, compactRoot, connector.Ranges, migrate.Connectors); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	shards, err := reader.ReadShards(compactRoot)
	if err != nil {
		t.Fatal(err)
	}
	var victim reader.Shard
	for _, shard := range shards {
		for _, record := range shard.File.Records {
			if record.Kind == "lines" {
				victim = shard
			}
		}
		if victim.Path != "" {
			break
		}
	}
	if victim.Path == "" {
		t.Fatal("no line record to corrupt")
	}

	// Drop the shard entirely: the atoms it explained must become uncovered.
	if err := os.Remove(filepath.Join(compactRoot, filepath.FromSlash(victim.Path))); err != nil {
		t.Fatal(err)
	}
	document, validation, _, err := reader.Load(compactRoot, connector.Exact)
	if err != nil {
		t.Fatal(err)
	}
	report := coverage.Evaluate(document, validation, base.changes)
	differences := equiv.Compare(base.document, base.report, document, report)
	if len(differences) == 0 {
		t.Fatal("removing a shard changed nothing the comparison could see; the equivalence check is vacuous")
	}
	var sawOwnership bool
	for _, difference := range differences {
		if difference.Kind == "owners" || difference.Kind == "atom presence" {
			sawOwnership = true
		}
	}
	if !sawOwnership {
		t.Fatalf("removing a shard was noticed, but not as an ownership change: %v", differences)
	}
}
