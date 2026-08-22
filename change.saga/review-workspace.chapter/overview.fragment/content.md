# The local review workspace {#local-review-workspace}

This chapter follows one reviewer-sized journey through the local application added between the empty base and `v0.0.6`: read the authored change, traverse its evidence in either direction, annotate a precise point, and preserve the result as Git-native history.

The boundary is intentionally narrow. It owns the HTTP/UI workspace, review-record writer, atomic filesystem primitives, and Git attribution resolver under `internal/server/**`, `internal/reviewstore/**`, `internal/store/**`, and `internal/gitattribution/**`. Saga parsing, diff identity construction, authoring commands, release packaging, and unrelated chapters remain outside this review unit.

Review the journey map first, then use the interactive walkthrough. The annotation section concentrates mutable state and crash/concurrency behavior; the final section isolates identity and trust boundaries. No review decisions or comments are authored here.
