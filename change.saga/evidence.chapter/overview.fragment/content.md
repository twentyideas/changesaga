# Portable evidence and AI authoring {#portable-evidence-ai-authoring}

This chapter explains the machinery that turns a source comparison into a portable, inspectable review narrative. It owns the command surface and authoring contract, the versioned Saga model and schemas, Git-derived diff identities, coverage and maintenance analysis, and the deterministic read API. The browser server runtime belongs to the local-review chapter and is intentionally outside this boundary.

## Since inception to v0.0.6 {#since-inception}

The comparison begins at `14afd0526c471d9ccad5825fadda735094e599e4`, before any path owned by this chapter exists, and ends at the `v0.0.6` product snapshot. The evidence is therefore lifecycle-aware: each owned file has an `add` event as well as its new-side line atoms, so a reviewer sees the arrival of the whole authoring system rather than a misleading set of ordinary modifications.[^inception-additions]

[^inception-additions]: Git file-lifecycle evidence records every owned path as an addition at v0.0.6 against the inception base.

## Review path {#review-path}

Start with the pipeline map to see how commands, packages, Git evidence, coverage, and queries compose. Then use the interactive loop to inspect the happy path and failure/recovery states. Finish with the concise format and AI-interface contracts, where implementation claims link to exact source ranges.

## Boundaries and checks {#boundaries-checks}

The format is Git-native and repository-portable; it does not promise that a complete mapping proves correctness. Validation checks structure and authoring quality, status reports omission coverage, mapping scrutiny surfaces broad or weak records, and claims carry explicit verification history. Compatibility risk concentrates in stable IDs, absolute diff URIs, schema/runtime agreement, deterministic ordering, and read-only query bounds.
