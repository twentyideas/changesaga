// Package bench holds the portable half of the experiment's measurements: Go
// benchmarks over a generated fixture, so a reviewer without the 230 MB saga
// can still see the shape of the result. The whole-codebase numbers come from
// `compactctl bench` and are recorded in docs/findings.md.
package bench

import (
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/index"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/migrate"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/reader"
	"github.com/twentyideas/changesaga/internal/coverage"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/testfixture"
)

type built struct {
	legacyRoot    string
	connectorRoot string
	changes       gitdiff.ChangeSet
	result        migrate.Result
}

func build(tb testing.TB) built {
	tb.Helper()
	options := testfixture.DefaultLargeSagaOptions()
	generated, err := testfixture.GenerateLargeSaga(context.Background(), tb.TempDir(), options)
	if err != nil {
		tb.Fatalf("generate fixture: %v", err)
	}
	document, _, err := saga.Load(generated.Root)
	if err != nil {
		tb.Fatal(err)
	}
	source := document.Manifest.Source
	changes, err := gitdiff.Read(context.Background(), generated.Repository, source.Repository, source.Base, source.Head)
	if err != nil {
		tb.Fatal(err)
	}
	connectorRoot := filepath.Join(tb.TempDir(), "compact.saga")
	result, err := migrate.ToConnectors(generated.Root, connectorRoot, connector.Ranges, migrate.Connectors)
	if err != nil {
		tb.Fatalf("migrate: %v", err)
	}
	return built{legacyRoot: generated.Root, connectorRoot: connectorRoot, changes: changes, result: result}
}

// TestFixtureEncodingCost is a measurement, not a budget. It exists because
// the generated fixture is the encoding's worst realistic case: every fragment
// owns exactly one four-line range in each source file, so every shard holds a
// single record and the hoisted header has nothing to amortise against. The
// invariant worth asserting is therefore not "smaller" — on this shape it is
// not — but that the per-record cost, once the header is set aside, is an order
// of magnitude below a v2 reference. That is the property that turns into a
// 158x saving on a whole-codebase saga, where one target owns hundreds of lines
// of one file.
func TestFixtureEncodingCost(t *testing.T) {
	fixture := build(t)
	legacyDeflated := deflatedSize(t, fixture.legacyRoot, ".json")
	connectorDeflated := deflatedSize(t, fixture.connectorRoot, connector.Extension)

	perReference := float64(fixture.result.LegacyBytes) / float64(fixture.result.LegacyRefs)
	perRecord := float64(fixture.result.ConnectorBytes-fixture.result.HeaderBytes) / float64(fixture.result.Records)
	recordsPerShard := float64(fixture.result.Records) / float64(fixture.result.ConnectorFiles)

	t.Logf("v2 JSON    %4d files, %8d raw, %8d deflated, %d references (%.0f B each)",
		fixture.result.LegacyFiles, fixture.result.LegacyBytes, legacyDeflated, fixture.result.LegacyRefs, perReference)
	t.Logf("connectors %4d files, %8d raw, %8d deflated, %d records (%.0f B each after a %d B header budget)",
		fixture.result.ConnectorFiles, fixture.result.ConnectorBytes, connectorDeflated,
		fixture.result.Records, perRecord, fixture.result.HeaderBytes)
	t.Logf("raw ratio %.2fx, deflated ratio %.2fx, %.1f records per shard",
		float64(fixture.result.LegacyBytes)/float64(fixture.result.ConnectorBytes),
		float64(legacyDeflated)/float64(connectorDeflated), recordsPerShard)

	if perRecord*10 > perReference {
		t.Fatalf("a connector record costs %.0f B against a v2 reference's %.0f B; the encoding has lost its advantage",
			perRecord, perReference)
	}
	if fixture.result.Records > fixture.result.LegacyRefs {
		t.Fatalf("the encoding produced %d records from %d references; it should never grow the record count",
			fixture.result.Records, fixture.result.LegacyRefs)
	}
}

func deflatedSize(t *testing.T, root, extension string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
			return nil
		}
		if filepath.Base(filepath.Dir(current)) != connector.EvidenceDirectory {
			return nil
		}
		contents, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		var buffer bytes.Buffer
		writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
		if err != nil {
			return err
		}
		if _, err := writer.Write(contents); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		total += int64(buffer.Len())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func BenchmarkLoadAndEvaluateLegacy(b *testing.B) {
	fixture := build(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		document, validation, err := saga.Load(fixture.legacyRoot)
		if err != nil {
			b.Fatal(err)
		}
		if report := coverage.Evaluate(document, validation, fixture.changes); report.Summary.Total == 0 {
			b.Fatal("no atoms")
		}
	}
}

func BenchmarkLoadAndEvaluateConnectors(b *testing.B) {
	fixture := build(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		document, validation, _, err := reader.Load(fixture.connectorRoot, connector.Exact)
		if err != nil {
			b.Fatal(err)
		}
		if report := coverage.Evaluate(document, validation, fixture.changes); report.Summary.Total == 0 {
			b.Fatal("no atoms")
		}
	}
}

func BenchmarkIndexColdBuild(b *testing.B) {
	b.Setenv("CHANGE_SAGA_INDEX_DIR", b.TempDir())
	fixture := build(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if handle, err := index.Open(ctx, fixture.connectorRoot); err == nil {
			if err := handle.Discard(); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		handle, err := index.Open(ctx, fixture.connectorRoot)
		if err != nil {
			b.Fatal(err)
		}
		if err := handle.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndexWarmOverview(b *testing.B) {
	b.Setenv("CHANGE_SAGA_INDEX_DIR", b.TempDir())
	fixture := build(b)
	ctx := context.Background()
	handle, err := index.Open(ctx, fixture.connectorRoot)
	if err != nil {
		b.Fatal(err)
	}
	defer handle.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := handle.Overview(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndexOwnersOfLine(b *testing.B) {
	b.Setenv("CHANGE_SAGA_INDEX_DIR", b.TempDir())
	fixture := build(b)
	ctx := context.Background()
	handle, err := index.Open(ctx, fixture.connectorRoot)
	if err != nil {
		b.Fatal(err)
	}
	defer handle.Close()

	var probe string
	for _, atom := range fixture.changes.Atoms {
		if atom.Kind == "line" {
			probe = atom.Path
			break
		}
	}
	if probe == "" {
		b.Fatal("fixture has no line atoms")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		owners, err := handle.OwnersOfLine(ctx, probe, "new", 1)
		if err != nil {
			b.Fatal(err)
		}
		if len(owners) == 0 {
			b.Fatal("probe line has no owner")
		}
	}
}

// BenchmarkIndexIncrementalRefresh is the number that decides whether the index
// is worth keeping between commands: the cost of noticing that one shard of
// many changed.
func BenchmarkIndexIncrementalRefresh(b *testing.B) {
	b.Setenv("CHANGE_SAGA_INDEX_DIR", b.TempDir())
	fixture := build(b)
	ctx := context.Background()
	shards, err := reader.Stat(fixture.connectorRoot)
	if err != nil {
		b.Fatal(err)
	}
	touched := filepath.Join(fixture.connectorRoot, filepath.FromSlash(shards[0].Path))
	contents, err := os.ReadFile(touched)
	if err != nil {
		b.Fatal(err)
	}
	if handle, err := index.Open(ctx, fixture.connectorRoot); err == nil {
		handle.Close()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		annotated := append(append([]byte(nil), contents...), []byte("\n# touch "+itoa(i)+"\n")...)
		if err := os.WriteFile(touched, annotated, 0o644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		handle, err := index.Open(ctx, fixture.connectorRoot)
		if err != nil {
			b.Fatal(err)
		}
		if handle.Rebuilt != 1 {
			b.Fatalf("refresh rebuilt %d shards, want 1", handle.Rebuilt)
		}
		if err := handle.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
