# Large-saga performance

Performance work uses deterministic fixtures rather than a checked-in generated
saga. The default disk fixture models a mega pull request with:

- 8 chapters, 48 sections, and 145 fragments across Markdown, SVG, and HTML;
- 4,096 changed-line atoms, 1,024 ranged references, and 4,096 exact ownership
  mappings in 144 diff files;
- 288 approval records, 48 threads with messages and events, and 32 file review
  records; and
- a real two-commit Git comparison with stable object IDs and 555,020 bytes of
  saga data.

`TestGenerateLargeSagaIsDeterministicAndValid` compares independent generated
trees byte for byte, loads the result through normal validation, and proves that
every atom is mapped with no overlaps or stale references. The in-memory server
fixtures use 4,096 atoms when isolating Code Diff, Coverage, rendering, and
linked-drawer construction.

## Budgets

These are review budgets, not cross-platform test assertions. Wall time varies
with Git, filesystem, CPU, and concurrent CI load; a repeatable regression or an
allocation increase should be investigated before adjusting a budget.

| Surface | Benchmark | Time budget | Allocation budget | Output budget |
| --- | --- | ---: | ---: | ---: |
| CLI validation | `BenchmarkValidateLargeSaga` | 200 ms | 24 MiB | — |
| CLI status | `BenchmarkStatusLargeSaga` | 500 ms | 64 MiB | — |
| HTTP first load | `BenchmarkLargeSagaHTTP/first_load` | 750 ms | 20 MiB | 1 MiB HTML |
| Chapter deep-link route | `BenchmarkLargeSagaHTTP/chapter_navigation` | 30 ms | 4 MiB | — |
| Code Diff model | `BenchmarkMakeCodeReviewViewLargeSaga` | 25 ms | 8 MiB | — |
| Coverage model | `BenchmarkLargeSagaCoverageView` | 30 ms | 20 MiB | — |
| Coverage HTML | `BenchmarkLargeSagaCoverageRender` | 250 ms | 64 MiB | — |
| Linked drawer model | `BenchmarkLargeSagaLinkedDrawerConstruction` | 60 ms | 32 MiB | — |
| Coverage matching | `BenchmarkEvaluateLargeMappedDiff` | 100 ms | 56 MiB | — |

The server still sends one page-like HTML payload. Chapter routes remain valid
deep links, chapter navigation stays collapsed until requested, and opening the
Code Diff, Coverage, or linked drawer does not require another saga load.

## Reference results

The following results were recorded on Apple M3 Pro, darwin/arm64, Go 1.26.
Times are representative medians; allocations are rounded per operation.

| Hot path | Before | After | Change |
| --- | ---: | ---: | ---: |
| Coverage matching, 10,000 atoms / 2,000 mappings | 493.8 ms, 45.22 MB | 37.1 ms, 45.23 MB | 13.3× faster; allocations stable |
| Git attribution, 100 committed records | 1.65 s, 17.30 MB | 175 ms, 6.31 MB | 9.4× faster; 64% fewer allocated bytes |
| HTTP first load after repository-query batching, before bounded history workers | 1.17 s, 13.04 MB | 379 ms, 13.06 MB | 3.1× faster; allocations stable |

Current post-change output for the remaining page construction surfaces:

| Surface | Time | Allocated | Allocations / output |
| --- | ---: | ---: | ---: |
| CLI validation | 60.6 ms | 14.52 MB | 109,501 allocs |
| CLI status | 151.4 ms | 49.48 MB | 414,447 allocs |
| Chapter deep-link route | 13.5 ms | 2.49 MB | 20,594 allocs |
| Code Diff model | 4.03 ms | 6.33 MB | 47,439 allocs |
| Coverage model | 10.4 ms | 13.56 MB | 118,247 allocs |
| Coverage HTML | 123.6 ms | 51.69 MB | 1,965,501 allocs |
| Linked drawer model | 25.1 ms | 19.66 MB | 315,372 allocs |
| HTTP first load | 379 ms | 13.06 MB | 105,716 allocs; 634,934-byte HTML |

Run the benchmark set with:

```sh
go test ./internal/coverage ./internal/cli ./internal/gitattribution ./internal/server \
  -run '^$' -bench 'LargeSaga|LargeMappedDiff' -benchmem -benchtime=3x -count=3
```

For before/after comparisons, keep the fixture and `-benchtime` identical and
use `benchstat` when it is available. Do not include fixture construction in the
timed region.

## Remaining bottlenecks

- Coverage HTML rendering allocates about 52 MB and nearly two million objects;
  the complete audit view is intentionally still present in the single payload.
- Git attribution retains one `git log --follow` per unique committed record to
  preserve rename-aware authorship. Four bounded workers reduce latency without
  weakening that identity rule, but history walking remains the largest part of
  first load.
- `gitdiff.Read` keeps separate display-context and canonical product-identity
  diffs because they require different Git options and exclude different paths.
- CLI status still spends most allocations parsing fully qualified diff URIs and
  constructing the complete coverage report.
