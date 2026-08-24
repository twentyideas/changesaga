# Remove quadratic work from the review core {#remove-quadratic-work-from-the-review-core}

Ranged authoring makes the common saga smaller, but the reader must still behave linearly when it encounters fine-grained or externally authored evidence.

The diff model now assigns stable integer positions to atoms. Coverage evaluation stores primary ownership in a contiguous positional slice, additional owners in a sparse overlap map, and per-target results as positions rather than copied atom structs.[^positions]

Review construction builds a key from target, normalized evidence path, and diff index. That replaces the former inner scan over every selector owned by a target with a direct lookup.[^selector-index]

Aggregate queries request a summary-only session. Overview and children can report hierarchy and counts without constructing reverse ownership or fragment-detail state that their response schemas cannot expose.[^summary]

Scale tests preserve these as contracts: canonical range behavior, positional equivalence, selector growth, allocation budgets, and large-saga query latency all get focused regression coverage.[^budgets]

[^positions]: Coverage evaluation indexes parsed atoms by integer position and materializes detailed ownership only for callers that request it.
[^selector-index]: Review ownership links use a prebuilt selector index instead of scanning every selector for every assigned atom.
[^summary]: Aggregate query operations explicitly select summary construction and omit detail-only projections.
[^budgets]: Large generated fixtures and benchmarks assert result equivalence while rejecting superlinear growth and unbounded allocation.
