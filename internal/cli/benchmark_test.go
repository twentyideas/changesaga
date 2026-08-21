package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/change-saga/change-saga/internal/testfixture"
)

const (
	largeSagaMinimumAtomBudget    = 4_000
	largeSagaValidateTimeBudgetNS = 200_000_000
	largeSagaValidateAllocBudget  = 24 << 20
	largeSagaStatusTimeBudgetNS   = 500_000_000
	largeSagaStatusAllocBudget    = 64 << 20
)

// Initial Apple M3 Pro / Go 1.26 measurements for the default fixture were
// 61-73 ms and 14.5 MB/op for validate, and 158-195 ms and 49.4 MB/op for
// status. The reported budgets leave headroom for CI and slower filesystems;
// they are metrics rather than assertions because benchmark hosts vary.

func BenchmarkValidateLargeSaga(b *testing.B) {
	fixture, size := largeSagaBenchmarkFixture(b)
	args := []string{"--json", fixture.Root}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := Validate(ctx, args, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(fixture.Atoms), "atoms/op")
	b.ReportMetric(float64(fixture.Mappings), "mappings/op")
	b.ReportMetric(float64(fixture.References), "references/op")
	b.ReportMetric(float64(fixture.DiffFiles), "diff-files/op")
	b.ReportMetric(float64(size), "saga-B/op")
	b.ReportMetric(largeSagaValidateTimeBudgetNS, "time-budget-ns/op")
	b.ReportMetric(largeSagaValidateAllocBudget, "alloc-budget-B/op")
}

func BenchmarkStatusLargeSaga(b *testing.B) {
	fixture, size := largeSagaBenchmarkFixture(b)
	args := []string{"--json", "--repo", fixture.Repository, fixture.Root}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := Status(ctx, args, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(fixture.Atoms), "atoms/op")
	b.ReportMetric(float64(fixture.Mappings), "mappings/op")
	b.ReportMetric(float64(fixture.References), "references/op")
	b.ReportMetric(float64(fixture.DiffFiles), "diff-files/op")
	b.ReportMetric(float64(size), "saga-B/op")
	b.ReportMetric(largeSagaStatusTimeBudgetNS, "time-budget-ns/op")
	b.ReportMetric(largeSagaStatusAllocBudget, "alloc-budget-B/op")
}

func largeSagaBenchmarkFixture(b *testing.B) (testfixture.LargeSaga, int64) {
	b.Helper()
	fixture, err := testfixture.GenerateLargeSaga(context.Background(), b.TempDir(), testfixture.DefaultLargeSagaOptions())
	if err != nil {
		b.Fatal(err)
	}
	if fixture.Atoms < largeSagaMinimumAtomBudget || fixture.Mappings != fixture.Atoms || fixture.References >= fixture.Mappings {
		b.Fatalf("fixture scale regressed: atoms=%d mappings=%d references=%d", fixture.Atoms, fixture.Mappings, fixture.References)
	}
	size, err := treeSize(fixture.Root)
	if err != nil {
		b.Fatal(err)
	}
	return fixture, size
}

func treeSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}
