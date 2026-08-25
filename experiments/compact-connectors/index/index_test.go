package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/index"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/migrate"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/reader"
	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

// connectorFixture builds a small real comparison, covers it, and migrates the
// coverage to connector shards. The index is always tested against shards that
// came from the real coverage pipeline rather than from hand-written records.
func connectorFixture(t *testing.T) (root string, changes gitdiff.ChangeSet) {
	t.Helper()
	// Isolating the cache directory is what makes "cold" mean cold: the index
	// otherwise persists across test runs by design.
	t.Setenv("CHANGE_SAGA_INDEX_DIR", t.TempDir())

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
	document, _, err := saga.Load(generated.Root)
	if err != nil {
		t.Fatal(err)
	}
	source := document.Manifest.Source
	changes, err = gitdiff.Read(context.Background(), generated.Repository, source.Repository, source.Base, source.Head)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(t.TempDir(), "compact.saga")
	if _, err := migrate.ToConnectors(generated.Root, root, connector.Ranges, migrate.Connectors); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return root, changes
}

func TestIndexBuildsColdThenReusesEveryShard(t *testing.T) {
	root, _ := connectorFixture(t)
	ctx := context.Background()

	cold, err := index.Open(ctx, root)
	if err != nil {
		t.Fatalf("cold open: %v", err)
	}
	shards, err := reader.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if cold.Rebuilt != len(shards) || cold.Reused != 0 {
		t.Fatalf("cold build rebuilt %d of %d shards and reused %d; a cold build must read them all",
			cold.Rebuilt, len(shards), cold.Reused)
	}
	if err := cold.Close(); err != nil {
		t.Fatal(err)
	}

	warm, err := index.Open(ctx, root)
	if err != nil {
		t.Fatalf("warm open: %v", err)
	}
	defer warm.Close()
	if warm.Rebuilt != 0 || warm.Reused != len(shards) {
		t.Fatalf("warm open rebuilt %d shards and reused %d; an unchanged saga must re-read nothing",
			warm.Rebuilt, warm.Reused)
	}
}

// Invalidation is per shard, which is the property that makes the index worth
// keeping: one new mapping must not cost a full rebuild.
func TestIndexRebuildsOnlyTheShardsThatChanged(t *testing.T) {
	root, _ := connectorFixture(t)
	ctx := context.Background()

	first, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	total := first.Rebuilt
	if total < 3 {
		t.Fatalf("fixture produced only %d shards; the test needs several", total)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	shards, err := reader.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	touched := filepath.Join(root, filepath.FromSlash(shards[0].Path))
	contents, err := os.ReadFile(touched)
	if err != nil {
		t.Fatal(err)
	}
	// Append a comment: the bytes change, the meaning does not, and the index
	// must notice regardless.
	if err := os.WriteFile(touched, append(contents, []byte("\n# reviewed\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Rebuilt != 1 || changed.Reused != total-1 {
		t.Fatalf("editing one shard rebuilt %d shards and reused %d; want 1 and %d",
			changed.Rebuilt, changed.Reused, total-1)
	}
	if err := changed.Close(); err != nil {
		t.Fatal(err)
	}

	// A removed shard must leave the index, or a deleted mapping would keep
	// answering queries.
	removedPath := shards[1].Path
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(removedPath))); err != nil {
		t.Fatal(err)
	}
	after, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if after.Removed != 1 {
		t.Fatalf("deleting a shard removed %d index entries, want 1", after.Removed)
	}
	overview, err := after.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Shards != total-1 {
		t.Fatalf("index still holds %d shards after a deletion, want %d", overview.Shards, total-1)
	}
}

// The index is a cache. Deleting it must cost time, never correctness.
func TestDiscardedIndexRebuildsToTheSameAnswers(t *testing.T) {
	root, _ := connectorFixture(t)
	ctx := context.Background()

	first, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := first.CoverageByTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeOverview, err := first.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Discard(); err != nil {
		t.Fatal(err)
	}

	second, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after, err := second.CoverageByTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterOverview, err := second.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if beforeOverview != afterOverview {
		t.Fatalf("rebuilt index summarises differently:\n before %+v\n after  %+v", beforeOverview, afterOverview)
	}
	if len(before) != len(after) {
		t.Fatalf("rebuilt index has %d targets, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("target %d differs after rebuild:\n before %+v\n after  %+v", i, before[i], after[i])
		}
	}
}

// The index must agree with the coverage engine about who owns what. If it
// ever disagreed, a reviewer's fast answer and their slow answer would differ.
func TestIndexAgreesWithTheCoverageEngineAboutOwnership(t *testing.T) {
	root, changes := connectorFixture(t)
	ctx := context.Background()

	document, validation, _, err := reader.Load(root, connector.Exact)
	if err != nil {
		t.Fatal(err)
	}
	report := coverage.Evaluate(document, validation, changes)
	if report.Summary.Covered == 0 {
		t.Fatal("fixture reports no coverage")
	}

	handle, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	overview, err := handle.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if indexed := overview.LineAtoms + overview.EventAtoms; indexed != report.Summary.Total {
		t.Fatalf("index counts %d atoms, the coverage engine counts %d", indexed, report.Summary.Total)
	}

	checked := 0
	for _, atom := range changes.Atoms {
		if atom.Kind != "line" {
			continue
		}
		owners, err := handle.OwnersOfLine(ctx, atom.Path, atom.Side, atom.Line)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]bool{}
		for _, assignment := range report.Ownership[atom.Key] {
			expected[assignment.Target] = true
		}
		got := map[string]bool{}
		for _, owner := range owners {
			got[owner.Target] = true
		}
		if len(got) != len(expected) {
			t.Fatalf("index reports %d owners for %s, coverage reports %d", len(got), atom.Key, len(expected))
		}
		for target := range expected {
			if !got[target] {
				t.Fatalf("index missed owner %s for %s", target, atom.Key)
			}
		}
		checked++
		if checked >= 200 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("no line atoms were checked")
	}
}

// A cold answer has to be paid for once; a warm answer has to be nearly free.
// The assertion is a ratio rather than an absolute time, because absolute times
// on a shared machine say more about the machine than about the index.
func TestWarmAccessIsFarCheaperThanColdAccess(t *testing.T) {
	root, _ := connectorFixture(t)
	ctx := context.Background()

	start := time.Now()
	cold, err := index.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cold.CoverageByTarget(ctx); err != nil {
		t.Fatal(err)
	}
	coldDuration := time.Since(start)
	if err := cold.Close(); err != nil {
		t.Fatal(err)
	}

	best := time.Duration(1 << 62)
	for range 5 {
		start = time.Now()
		warm, err := index.Open(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := warm.CoverageByTarget(ctx); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed < best {
			best = elapsed
		}
		if err := warm.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("cold %s, best warm %s", coldDuration.Round(time.Microsecond), best.Round(time.Microsecond))
	if best >= coldDuration {
		t.Fatalf("warm access (%s) was not cheaper than cold access (%s); the cache is not being reused",
			best, coldDuration)
	}
}
