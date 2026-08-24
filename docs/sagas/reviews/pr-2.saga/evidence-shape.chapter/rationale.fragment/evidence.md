# Why the format did not change {#why-the-format-did-not-change}

The original hypothesis was that a whole-codebase saga needed a compact connector encoding and a query index. The prototype implemented that hypothesis completely enough to measure it: parser and writer, migration both directions, semantic equivalence, disposable indexing, benchmarks, and real Git merge cases.[^prototype]

The result isolated the dominant defect. Changed-line authoring emitted one URI per changed line even though v2 already supports dense ranges. Coalescing only consecutive atoms reduced the pathological evidence from 230.36 MB to 2.33 MB while preserving the same 532,290 covered atoms.[^ranges]

That made the architectural choice straightforward: retain the experiment and findings, but ship canonical ranged v2 selectors. Manual input is canonicalized too, so equivalent ranges have one representation.[^canonical]

Coverage filenames now derive from selector identity rather than wall-clock time. Two branches that explain the same selectors differently therefore collide in Git and force the explanation conflict to be reconciled; unrelated selectors keep independent files.[^conflicts]

[^prototype]: The compact connector module is an isolated measurement harness, not a runtime dependency of the Change Saga CLI.
[^ranges]: Changed-line coverage is coalesced only across consecutive atoms with the same immutable comparison identity, path, and side.
[^canonical]: Manual line lists are parsed, sorted, deduplicated, and re-emitted as canonical dense ranges.
[^conflicts]: Coverage record names hash the canonical selector set and deliberately exclude the human explanation note.
